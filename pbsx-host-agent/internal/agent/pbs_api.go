package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
)

// PBSConnectionInfo returns the pinned endpoint and selected storage credential.
// PBSAPIRequest independently retains the automatic host-management credential.
func PBSConnectionInfo() (string, string, *tls.Config, error) {
	server, err := registry.GetEntry(registry.CONFIG, "ServerURL", false)
	if err != nil || server == nil || strings.TrimSpace(server.Value) == "" {
		return "", "", nil, fmt.Errorf("PBS API server URL is not configured")
	}
	token, err := storageAuthToken()
	if err != nil {
		return "", "", nil, err
	}
	endpoint, _, err := pbsAPIEndpoint(server.Value, "/api2/json")
	if err != nil {
		return "", "", nil, err
	}
	tlsConfig, err := GetTLSConfig()
	if err != nil {
		return "", "", nil, err
	}
	return strings.TrimSuffix(endpoint, "/"), token, tlsConfig, nil
}

type pbsAPIEnvelope[T any] struct {
	Data T `json:"data"`
}

// PBSAPIRequest talks to PBS-X through the normal PBS proxy on port 8007 using
// the host-scoped API token issued during enrollment.
func PBSAPIRequest(ctx context.Context, method, path string, body any, out any) error {
	server, err := registry.GetEntry(registry.CONFIG, "ServerURL", false)
	if err != nil || server == nil {
		return fmt.Errorf("PBS API server URL is not configured")
	}
	authID, err := registry.GetEntry(registry.AUTH, "BackupAuthID", false)
	if err != nil || authID == nil {
		return fmt.Errorf("PBS API auth ID is not configured")
	}
	secret, err := registry.GetEntry(registry.AUTH, "BackupSecret", true)
	if err != nil || secret == nil {
		return fmt.Errorf("PBS API secret is not configured")
	}

	endpoint, _, err := pbsAPIEndpoint(server.Value, "/api2/json"+ensureLeadingSlash(path))
	if err != nil {
		return err
	}
	// pbsAPIEndpoint resets RawQuery, so preserve a query supplied with path.
	if parsedPath, err := url.Parse(path); err == nil && parsedPath.RawQuery != "" {
		u, _ := url.Parse(endpoint)
		u.RawQuery = parsedPath.RawQuery
		u.Path = "/api2/json" + ensureLeadingSlash(parsedPath.Path)
		endpoint = u.String()
	}

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "PBSAPIToken "+strings.TrimSpace(authID.Value)+":"+strings.TrimSpace(secret.Value))

	tlsConfig, err := GetTLSConfig()
	if err != nil {
		return err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig.Clone()
	client := &http.Client{Transport: transport, Timeout: 45 * time.Second}
	defer transport.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PBS API %s %s returned HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var envelope json.RawMessage
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return fmt.Errorf("decode PBS API envelope: %w", err)
	}
	envelope = wrapper.Data
	if len(envelope) == 0 || string(envelope) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope, out); err != nil {
		return fmt.Errorf("decode PBS API response: %w", err)
	}
	return nil
}

func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}
