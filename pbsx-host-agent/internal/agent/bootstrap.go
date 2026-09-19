package agent

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs/fswire"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
	"github.com/pbs-x/pbsx-host-agent/internal/conf"
	"github.com/pbs-x/pbsx-host-agent/internal/host"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
	"github.com/pbs-x/pbsx-host-agent/internal/mtls"
)

type BootstrapRequest struct {
	HostID            string         `json:"host-id"`
	Token             string         `json:"token"`
	ClientCertificate string         `json:"client-certificate"`
	Heartbeat         AgentHeartbeat `json:"heartbeat"`
}

type AgentHeartbeat struct {
	Hostname        string                `json:"hostname"`
	OperatingSystem string                `json:"operating-system"`
	OSVersion       string                `json:"os-version,omitempty"`
	Architecture    string                `json:"architecture"`
	AgentVersion    string                `json:"agent-version"`
	Capabilities    []string              `json:"capabilities,omitempty"`
	Disks           []AgentDiskInfo       `json:"disks,omitempty"`
	Filesystems     []AgentFilesystemInfo `json:"filesystems,omitempty"`
}

type AgentDiskInfo struct {
	Name       string `json:"name"`
	Size       uint64 `json:"size,omitempty"`
	Model      string `json:"model,omitempty"`
	Serial     string `json:"serial,omitempty"`
	Rotational *bool  `json:"rotational,omitempty"`
}

type AgentFilesystemInfo struct {
	Mountpoint string `json:"mountpoint"`
	FileSystem string `json:"file-system"`
	Source     string `json:"source,omitempty"`
	Total      uint64 `json:"total,omitempty"`
	Available  uint64 `json:"available,omitempty"`
}

type BootstrapResponse struct {
	BackupAuthID string `json:"backup-auth-id"`
	BackupSecret string `json:"backup-secret"`
}

type apiEnvelope[T any] struct {
	Data T `json:"data"`
}

func Bootstrap() error {
	token, err := registry.GetEntry(registry.CONFIG, "BootstrapToken", false)
	if err != nil || token == nil || strings.TrimSpace(token.Value) == "" {
		return fmt.Errorf("Bootstrap: token not found: %w", err)
	}

	serverURL, err := registry.GetEntry(registry.CONFIG, "ServerURL", false)
	if err != nil || serverURL == nil {
		return fmt.Errorf("Bootstrap: server url not found: %w", err)
	}

	hostID, err := AgentHostID()
	if err != nil {
		return fmt.Errorf("Bootstrap: failed to get host id: %w", err)
	}
	hostname, err := host.AgentHostname()
	if err != nil {
		return fmt.Errorf("Bootstrap: failed to get hostname: %w", err)
	}

	// PBS-X deliberately does not send a private key to the server. The agent creates a
	// long-lived self-signed control-plane identity and enrollment binds its fingerprint.
	certPEM, privateKeyPEM, err := mtls.GenerateSelfSignedClientCertificate(hostID, 3072, 3650)
	if err != nil {
		return fmt.Errorf("Bootstrap: generating client identity failed: %w", err)
	}

	heartbeat, err := CurrentHeartbeat()
	if err != nil {
		return fmt.Errorf("Bootstrap: failed to build heartbeat: %w", err)
	}
	heartbeat.Hostname = hostname

	reqBody, err := json.Marshal(&BootstrapRequest{
		HostID:            hostID,
		Token:             strings.TrimSpace(token.Value),
		ClientCertificate: string(certPEM),
		Heartbeat:         heartbeat,
	})
	if err != nil {
		return fmt.Errorf("Bootstrap: failed to marshal request: %w", err)
	}

	endpoint, serverName, err := pbsAPIEndpoint(serverURL.Value, "/api2/json/pbsx/agent/bootstrap")
	if err != nil {
		return fmt.Errorf("Bootstrap: server url is invalid: %w", err)
	}
	tlsConfig, err := bootstrapTLSConfig(serverName)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("Bootstrap: error creating http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Bootstrap: request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("Bootstrap: failed to read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Bootstrap: server returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}

	var envelope apiEnvelope[BootstrapResponse]
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return fmt.Errorf("Bootstrap: invalid PBS response: %w", err)
	}
	if envelope.Data.BackupAuthID == "" || envelope.Data.BackupSecret == "" {
		return fmt.Errorf("Bootstrap: server returned incomplete upload credentials")
	}

	entries := []*registry.RegistryEntry{
		{Key: "Cert", Value: string(certPEM), Path: registry.AUTH, IsSecret: true},
		{Key: "Priv", Value: string(privateKeyPEM), Path: registry.AUTH, IsSecret: true},
		{Key: "BackupAuthID", Value: envelope.Data.BackupAuthID, Path: registry.AUTH, IsSecret: false},
		{Key: "BackupSecret", Value: envelope.Data.BackupSecret, Path: registry.AUTH, IsSecret: true},
	}
	for _, entry := range entries {
		if err := registry.UpsertEntry(entry); err != nil {
			return fmt.Errorf("Bootstrap: failed to persist %s: %w", entry.Key, err)
		}
	}

	// The server token is single-use. Remove the local copy as well after a complete enrollment.
	if err := registry.DeleteEntry(registry.CONFIG, "BootstrapToken"); err != nil {
		log.Error(err, "bootstrap: failed to remove consumed enrollment token")
	}
	InvalidateTLSConfigCache()
	return nil
}

func CurrentHeartbeat() (AgentHeartbeat, error) {
	hostname, err := host.AgentHostname()
	if err != nil {
		return AgentHeartbeat{}, err
	}
	drives, err := GetLocalDrives()
	if err != nil {
		return AgentHeartbeat{}, err
	}
	return AgentHeartbeat{
		Hostname:        hostname,
		OperatingSystem: hostOSFamily(),
		Architecture:    runtime.GOARCH,
		AgentVersion:    conf.Version,
		Capabilities: []string{
			"native-pbs-upload",
			"file-backup",
			"multi-source-pxar",
		},
		Filesystems: filesystemsFromDrives(drives),
	}, nil
}

func hostOSFamily() string {
	switch runtime.GOOS {
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	default:
		return "other"
	}
}

func filesystemsFromDrives(drives []fswire.DriveInfo) []AgentFilesystemInfo {
	result := make([]AgentFilesystemInfo, 0, len(drives))
	for _, drive := range drives {
		mountpoint := drive.Letter
		if runtime.GOOS == "windows" && len(mountpoint) == 1 {
			mountpoint += `:\`
		}
		result = append(result, AgentFilesystemInfo{
			Mountpoint: mountpoint,
			FileSystem: drive.FileSystem,
			Source:     drive.VolumeName,
			Total:      drive.TotalBytes,
			Available:  drive.FreeBytes,
		})
	}
	return result
}

func pbsAPIEndpoint(rawServerURL, path string) (string, string, error) {
	rawServerURL = strings.TrimSpace(rawServerURL)
	if rawServerURL == "" {
		return "", "", fmt.Errorf("empty server URL")
	}
	if !strings.Contains(rawServerURL, "://") {
		rawServerURL = "https://" + rawServerURL
	}
	u, err := url.Parse(rawServerURL)
	if err != nil {
		return "", "", err
	}
	if u.Hostname() == "" {
		return "", "", fmt.Errorf("server URL has no hostname")
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if u.Scheme != "https" {
		return "", "", fmt.Errorf("PBS-X requires HTTPS")
	}
	if u.Port() == "" {
		u.Host = u.Hostname() + ":8007"
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), u.Hostname(), nil
}

func bootstrapTLSConfig(serverName string) (*tls.Config, error) {
	pin, err := registry.GetEntry(registry.CONFIG, "ServerCAFingerprint", false)
	if err == nil && pin != nil && strings.TrimSpace(pin.Value) != "" {
		expectedFingerprint := normalizeFingerprint(pin.Value)
		if len(expectedFingerprint) != 64 {
			return nil, fmt.Errorf("bootstrap: invalid ServerCAFingerprint")
		}
		return &tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         serverName,
			InsecureSkipVerify: true, // verification is performed against the pinned leaf fingerprint below
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return fmt.Errorf("bootstrap: server supplied no certificate")
				}
				fingerprint := sha256.Sum256(cs.PeerCertificates[0].Raw)
				if hex.EncodeToString(fingerprint[:]) != expectedFingerprint {
					return fmt.Errorf("bootstrap: server certificate fingerprint does not match ServerCAFingerprint")
				}
				return nil
			},
		}, nil
	}

	// No pin means ordinary PKI validation, never an implicit insecure connection. A stock PBS
	// self-signed certificate therefore requires its fingerprint during enrollment.
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}, nil
}

func normalizeFingerprint(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, ":", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}
