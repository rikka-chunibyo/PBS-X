package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/enrollment"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
)

func persistEnrollmentValue(key, value string) error {
	if key == "BootstrapToken" {
		return initializeBootstrapToken(value, registry.GetEntry, registry.UpsertEntry)
	}
	entry := &registry.RegistryEntry{Path: registry.CONFIG, Key: key, Value: value}
	if value != "" {
		return registry.UpsertEntry(entry)
	}
	return registry.CreateEntryIfNotExists(entry)
}

// INIT environment values seed an unenrolled host. A consumed code in an old
// service environment must not override an existing identity or a new code
// explicitly saved by the enroll command.
func initializeBootstrapToken(value string,
	lookup func(string, string, bool) (*registry.RegistryEntry, error),
	write func(*registry.RegistryEntry) error,
) error {
	stored, err := lookup(registry.CONFIG, "BootstrapToken", false)
	if err != nil && !errors.Is(err, registry.ErrNotFound) {
		return err
	}
	if stored != nil && strings.TrimSpace(stored.Value) != "" {
		return nil
	}
	cert, err := lookup(registry.AUTH, "Cert", true)
	if err != nil && !errors.Is(err, registry.ErrNotFound) {
		return err
	}
	if cert != nil && cert.Value != "" {
		if stored != nil {
			return nil
		}
		value = ""
	} else if stored != nil && value == "" {
		return nil
	}
	return write(&registry.RegistryEntry{Path: registry.CONFIG, Key: "BootstrapToken", Value: value})
}

func enrollmentCodeFromArgs(args []string) (string, bool, error) {
	if len(args) == 0 {
		return "", false, nil
	}
	if args[0] == "enroll" {
		if len(args) != 2 {
			return "", true, fmt.Errorf("usage: pbsx-host-agent enroll %s<code>", enrollment.Prefix)
		}
		return args[1], true, nil
	}
	if strings.HasPrefix(args[0], enrollment.Prefix) {
		if len(args) != 1 {
			return "", true, fmt.Errorf("unexpected arguments after enrollment code")
		}
		return args[0], true, nil
	}
	return "", false, nil
}

func runEnrollmentCode(code string) error {
	values, err := enrollment.Decode(code)
	if err != nil {
		return err
	}
	if err := agent.ValidateHostID(values.HostID); err != nil {
		return err
	}
	if err := agent.SetStorageCredentials(agent.StorageCredentials{AuthID: values.StorageAuthID, Secret: values.StorageSecret}); err != nil {
		return fmt.Errorf("save storage credentials: %w", err)
	}
	for key, value := range map[string]string{
		"HostID":              values.HostID,
		"ServerURL":           values.ServerURL,
		"BootstrapToken":      values.BootstrapToken,
		"ServerCAFingerprint": values.ServerCAFingerprint,
	} {
		if err := registry.UpsertEntry(&registry.RegistryEntry{Path: registry.CONFIG, Key: key, Value: value}); err != nil {
			return fmt.Errorf("save %s: %w", key, err)
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve agent executable: %w", err)
	}
	cmd := exec.Command(executable)
	cmd.Env = enrollmentEnvironment(os.Environ(), values)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Fprintln(os.Stderr, "Enrollment code accepted; starting PBS-X Agent...")
	return cmd.Run()
}

func enrollmentEnvironment(base []string, values enrollment.Values) []string {
	updates := map[string]string{
		"PBSX_HOST_ID":                    values.HostID,
		"PBSX_INIT_SERVER_URL":            values.ServerURL,
		"PBSX_INIT_BOOTSTRAP_TOKEN":       values.BootstrapToken,
		"PBSX_INIT_SERVER_CA_FINGERPRINT": values.ServerCAFingerprint,
		"PBSX_STDOUT_ONLY":                "true",
	}
	result := make([]string, 0, len(base)+len(updates))
	for _, item := range base {
		separator := strings.IndexByte(item, '=')
		if separator > 0 {
			if _, replaced := updates[strings.ToUpper(item[:separator])]; replaced {
				continue
			}
		}
		result = append(result, item)
	}
	for key, value := range updates {
		result = append(result, key+"="+value)
	}
	return result
}
