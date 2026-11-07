package agent

import (
	"fmt"
	"regexp"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
	"github.com/pbs-x/pbsx-host-agent/internal/conf"
	"github.com/pbs-x/pbsx-host-agent/internal/host"
)

var hostIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var backupIDPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)

// ValidateHostID mirrors the PBS PROXMOX_SAFE_ID constraints used by PBS-X.
func ValidateHostID(id string) error {
	if len(id) < 2 || len(id) > 64 {
		return fmt.Errorf("host id must be between 2 and 64 characters")
	}
	if !hostIDPattern.MatchString(id) {
		return fmt.Errorf("host id %q contains unsupported characters", id)
	}
	return nil
}

// ValidateBackupID validates PBS host backup groups, not enrolled device IDs.
// A group may have a one-character ID; device identities still require two.
func ValidateBackupID(id string) error {
	if !backupIDPattern.MatchString(id) {
		return fmt.Errorf("backup id must contain only PBS safe ID characters")
	}
	return nil
}

// InitializeHostID persists a stable PBS-X host identity. PBSX_HOST_ID wins on
// first enrollment. Otherwise the current hostname becomes the initial ID.
func InitializeHostID() (string, error) {
	// A supplied bootstrap token marks an explicit enrollment attempt. In that
	// case, honor the accompanying host ID instead of retaining an earlier one.
	if conf.Env.InitBootstrapToken != "" && conf.Env.HostID != "" {
		if err := ValidateHostID(conf.Env.HostID); err != nil {
			return "", err
		}
		entry := &registry.RegistryEntry{Path: registry.CONFIG, Key: "HostID", Value: conf.Env.HostID}
		if err := registry.UpsertEntry(entry); err != nil {
			return "", fmt.Errorf("persist HostID: %w", err)
		}
		return conf.Env.HostID, nil
	}

	if existing, err := registry.GetEntry(registry.CONFIG, "HostID", false); err == nil && existing != nil && existing.Value != "" {
		if err := ValidateHostID(existing.Value); err != nil {
			return "", fmt.Errorf("stored HostID is invalid: %w", err)
		}
		return existing.Value, nil
	}

	id := conf.Env.HostID
	if id == "" {
		var err error
		id, err = host.AgentHostname()
		if err != nil {
			return "", fmt.Errorf("resolve default host id: %w", err)
		}
	}
	if err := ValidateHostID(id); err != nil {
		return "", err
	}
	entry := &registry.RegistryEntry{Path: registry.CONFIG, Key: "HostID", Value: id}
	if err := registry.UpsertEntry(entry); err != nil {
		return "", fmt.Errorf("persist HostID: %w", err)
	}
	return id, nil
}

// AgentHostID returns the stable PBS-X backup-group identity of this machine.
func AgentHostID() (string, error) {
	entry, err := registry.GetEntry(registry.CONFIG, "HostID", false)
	if err == nil && entry != nil && entry.Value != "" {
		if err := ValidateHostID(entry.Value); err != nil {
			return "", err
		}
		return entry.Value, nil
	}
	return InitializeHostID()
}
