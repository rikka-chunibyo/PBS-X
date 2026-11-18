package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
)

// StorageCredentials overrides only PBS datastore traffic, never host management.
type StorageCredentials struct {
	AuthID string `json:"auth_id"`
	Secret string `json:"secret"`
}

func (c StorageCredentials) Validate() error {
	if c.AuthID == "" && c.Secret == "" {
		return nil
	}
	parts := strings.Split(c.AuthID, "!")
	userRealm := strings.Split(parts[0], "@")
	if len(parts) != 2 || len(userRealm) != 2 || userRealm[0] == "" || userRealm[1] == "" || parts[1] == "" ||
		strings.ContainsAny(c.AuthID, ":\r\n\t ") || strings.TrimSpace(c.Secret) == "" || strings.ContainsAny(c.Secret, "\r\n") {
		return fmt.Errorf("storage credentials require an API token ID (user@realm!token) and secret")
	}
	return nil
}

// Store as a single encrypted entry so readers never see a partially changed pair.
func SetStorageCredentials(c StorageCredentials) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := registry.UpsertEntry(&registry.RegistryEntry{Path: registry.AUTH, Key: "StorageCredentials", Value: string(data), IsSecret: true}); err != nil {
		return err
	}
	mode := "auto"
	if c.AuthID != "" {
		mode = "custom"
	}
	return registry.UpsertEntry(&registry.RegistryEntry{Path: registry.CONFIG, Key: "StorageCredentialMode", Value: mode})
}

func storageAuthToken() (string, error) {
	return storageAuthTokenFrom(registry.GetEntry)
}

func storageAuthTokenFrom(get func(string, string, bool) (*registry.RegistryEntry, error)) (string, error) {
	mode, err := get(registry.CONFIG, "StorageCredentialMode", false)
	if err != nil && !errors.Is(err, registry.ErrNotFound) {
		return "", fmt.Errorf("read storage credential mode: %w", err)
	}
	if mode != nil && mode.Value != "auto" && mode.Value != "" {
		if mode.Value != "custom" {
			return "", fmt.Errorf("unknown storage credential mode")
		}
		entry, err := get(registry.AUTH, "StorageCredentials", true)
		if err != nil || entry == nil || entry.Value == "" {
			return "", fmt.Errorf("custom storage credentials are unavailable")
		}
		var c StorageCredentials
		if json.Unmarshal([]byte(entry.Value), &c) != nil {
			return "", fmt.Errorf("stored storage credentials are invalid")
		}
		if err := c.Validate(); err != nil {
			return "", err
		}
		if c.AuthID == "" {
			return "", fmt.Errorf("custom storage token ID is missing")
		}
		return c.AuthID + ":" + c.Secret, nil
	}
	// No override is the backwards-compatible automatic per-host key.
	authID, err := get(registry.AUTH, "BackupAuthID", false)
	if err != nil || authID == nil || authID.Value == "" {
		return "", fmt.Errorf("PBS API auth ID is not configured")
	}
	secret, err := get(registry.AUTH, "BackupSecret", true)
	if err != nil || secret == nil || secret.Value == "" {
		return "", fmt.Errorf("PBS API secret is not configured")
	}
	return strings.TrimSpace(authID.Value) + ":" + strings.TrimSpace(secret.Value), nil
}
