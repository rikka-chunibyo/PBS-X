package enrollment

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const Prefix = "pbsx1."

type Values struct {
	Version             int    `json:"v"`
	HostID              string `json:"host_id"`
	ServerURL           string `json:"server_url"`
	BootstrapToken      string `json:"bootstrap_token"`
	ServerCAFingerprint string `json:"server_ca_fingerprint"`
}

// Decode accepts the opaque, URL-safe enrollment code emitted by PBS-X.
func Decode(code string) (Values, error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, Prefix) {
		return Values{}, fmt.Errorf("enrollment code must start with %q", Prefix)
	}
	encoded := strings.TrimPrefix(code, Prefix)
	if encoded == "" || len(encoded) > 16*1024 {
		return Values{}, fmt.Errorf("enrollment code has an invalid length")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Values{}, fmt.Errorf("decode enrollment code: %w", err)
	}
	var values Values
	if err := json.Unmarshal(raw, &values); err != nil {
		return Values{}, fmt.Errorf("parse enrollment code: %w", err)
	}
	if values.Version != 1 {
		return Values{}, fmt.Errorf("unsupported enrollment code version %d", values.Version)
	}
	if values.HostID == "" || values.ServerURL == "" || values.BootstrapToken == "" || values.ServerCAFingerprint == "" {
		return Values{}, fmt.Errorf("enrollment code is missing required values")
	}
	serverURL, err := url.ParseRequestURI(values.ServerURL)
	if err != nil || serverURL.Scheme != "https" || serverURL.Host == "" {
		return Values{}, fmt.Errorf("enrollment server must be a valid HTTPS URL")
	}
	return values, nil
}
