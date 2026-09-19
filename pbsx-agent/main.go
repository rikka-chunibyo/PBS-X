package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const agentVersion = "0.1.0-dev"

type config struct {
	server      string
	hostID      string
	tokenID     string
	tokenSecret string
	fingerprint string
	interval    time.Duration
	once        bool
}

type agent struct {
	cfg    config
	client *http.Client
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.server, "server", env("PBSX_SERVER", ""), "PBS-X server base URL, for example https://pbs.example:8007")
	flag.StringVar(&cfg.hostID, "host-id", env("PBSX_HOST_ID", ""), "host ID configured on the PBS-X server")
	flag.StringVar(&cfg.tokenID, "token-id", env("PBSX_TOKEN_ID", ""), "PBS API token auth ID, for example agent@pbs!desktop")
	flag.StringVar(&cfg.tokenSecret, "token-secret", env("PBSX_TOKEN_SECRET", ""), "PBS API token secret (prefer PBSX_TOKEN_SECRET over command-line use)")
	flag.StringVar(&cfg.fingerprint, "fingerprint", env("PBSX_FINGERPRINT", ""), "optional SHA-256 TLS certificate fingerprint")
	flag.DurationVar(&cfg.interval, "interval", 60*time.Second, "heartbeat interval")
	flag.BoolVar(&cfg.once, "once", false, "send one heartbeat and exit")
	flag.Parse()

	if err := cfg.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "pbsx-agent:", err)
		os.Exit(2)
	}

	client, err := newHTTPClient(cfg.fingerprint)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pbsx-agent:", err)
		os.Exit(2)
	}

	a := &agent{cfg: cfg, client: client}
	if cfg.once {
		if err := a.heartbeat(); err != nil {
			fmt.Fprintln(os.Stderr, "pbsx-agent:", err)
			os.Exit(1)
		}
		return
	}

	for {
		if err := a.heartbeat(); err != nil {
			fmt.Fprintln(os.Stderr, "pbsx-agent: heartbeat failed:", err)
		}
		time.Sleep(cfg.interval)
	}
}

func (cfg config) validate() error {
	if cfg.server == "" {
		return errors.New("missing --server or PBSX_SERVER")
	}
	if cfg.hostID == "" {
		return errors.New("missing --host-id or PBSX_HOST_ID")
	}
	if cfg.tokenID == "" {
		return errors.New("missing --token-id or PBSX_TOKEN_ID")
	}
	if cfg.tokenSecret == "" {
		return errors.New("missing --token-secret or PBSX_TOKEN_SECRET")
	}
	parsed, err := url.Parse(cfg.server)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("server must be an https URL with a host")
	}
	if cfg.interval < 10*time.Second {
		return errors.New("heartbeat interval must be at least 10 seconds")
	}
	return nil
}

func newHTTPClient(fingerprint string) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if fingerprint != "" {
		wanted, err := parseFingerprint(fingerprint)
		if err != nil {
			return nil, err
		}

		// PBS commonly uses a self-signed certificate. When a fingerprint is supplied we pin
		// the leaf certificate ourselves instead of asking the platform CA store to trust it.
		tlsConfig.InsecureSkipVerify = true // verified by VerifyPeerCertificate below
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("server did not present a TLS certificate")
			}
			got := sha256.Sum256(rawCerts[0])
			if got != wanted {
				return fmt.Errorf("server TLS fingerprint mismatch: got %s", formatFingerprint(got))
			}
			return nil
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

func parseFingerprint(value string) ([32]byte, error) {
	var result [32]byte
	cleaned := strings.ReplaceAll(strings.TrimSpace(value), ":", "")
	if len(cleaned) != 64 {
		return result, errors.New("TLS fingerprint must contain 32 SHA-256 bytes")
	}
	decoded, err := hex.DecodeString(cleaned)
	if err != nil {
		return result, fmt.Errorf("invalid TLS fingerprint: %w", err)
	}
	copy(result[:], decoded)
	return result, nil
}

func formatFingerprint(value [32]byte) string {
	parts := make([]string, len(value))
	for i, b := range value {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

func (a *agent) heartbeat() error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("get hostname: %w", err)
	}

	inv := collectInventory()
	payload := struct {
		Hostname        string           `json:"hostname"`
		OperatingSystem string           `json:"operating-system"`
		OSVersion       string           `json:"os-version,omitempty"`
		Architecture    string           `json:"architecture"`
		AgentVersion    string           `json:"agent-version"`
		Capabilities    []string         `json:"capabilities,omitempty"`
		Disks           []diskInfo       `json:"disks,omitempty"`
		Filesystems     []filesystemInfo `json:"filesystems,omitempty"`
	}{
		Hostname:        hostname,
		OperatingSystem: operatingSystem(),
		OSVersion:       operatingSystemVersion(),
		Architecture:    runtime.GOARCH,
		AgentVersion:    agentVersion,
		Capabilities:    []string{"heartbeat-v1", "inventory-v1"},
		Disks:           inv.Disks,
		Filesystems:     inv.Filesystems,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode heartbeat: %w", err)
	}

	endpoint := strings.TrimRight(a.cfg.server, "/") + "/api2/json/admin/host/" + url.PathEscape(a.cfg.hostID) + "/heartbeat"
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "PBSAPIToken "+a.cfg.tokenID+":"+percentEncode(a.cfg.tokenSecret))
	req.Header.Set("User-Agent", "pbsx-agent/"+agentVersion)

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}

	return nil
}

func operatingSystem() string {
	switch runtime.GOOS {
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	default:
		return "other"
	}
}

func operatingSystemVersion() string {
	switch runtime.GOOS {
	case "linux":
		if value := linuxPrettyName(); value != "" {
			return value
		}
	case "windows":
		if output, err := exec.Command("cmd", "/c", "ver").Output(); err == nil {
			return strings.TrimSpace(string(output))
		}
	case "darwin":
		if output, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			return "macOS " + strings.TrimSpace(string(output))
		}
	}
	return runtime.GOOS
}

func linuxPrettyName() string {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		value := strings.TrimPrefix(line, "PRETTY_NAME=")
		value = strings.Trim(value, "\"")
		return value
	}
	return ""
}

// percentEncode matches the RFC 3986-style encoding used for PBS API token secrets.
func percentEncode(value string) string {
	const hexChars = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~' {
			out.WriteByte(c)
		} else {
			out.WriteByte('%')
			out.WriteByte(hexChars[c>>4])
			out.WriteByte(hexChars[c&0x0f])
		}
	}
	return out.String()
}
