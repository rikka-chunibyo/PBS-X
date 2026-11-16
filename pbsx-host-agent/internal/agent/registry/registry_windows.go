//go:build windows

package registry

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/billgraziano/dpapi"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type RegistryEntry struct {
	Path     string
	Key      string
	Value    string
	IsSecret bool
}

var (
	legacySecretFilePath string
	legacyKeyFilePath    string

	initOnce sync.Once
	initErr  error
)

func init() {
	_ = ensureInitialized()
}

func ensureInitialized() error {
	initOnce.Do(func() {
		serviceDataDir := filepath.Join("C:\\", "PBS-X Agent")
		legacySecretFilePath = filepath.Join(serviceDataDir, "secrets.json")
		legacyKeyFilePath = filepath.Join(serviceDataDir, "master.key")

		// Machine-local DPAPI can be decrypted by other local accounts if they
		// can read the blob. Restrict current and retained legacy vault keys
		// before migrating or writing any values. Public desktop status uses a
		// separate key and never imports this privileged registry package.
		for _, path := range []string{CONFIG, AUTH, legacyConfig, legacyAuth} {
			if err := protectCredentialKey(path, path == CONFIG || path == AUTH); err != nil {
				initErr = fmt.Errorf("protect credential registry: %w", err)
				return
			}
		}

		if err := migrateLegacyRegistryPath(legacyConfig, CONFIG); err != nil {
			initErr = err
			return
		}
		if err := migrateLegacyRegistryPath(legacyAuth, AUTH); err != nil {
			initErr = err
			return
		}
		_ = migrateFromFileSecretsToRegistry()
	})
	return initErr
}

func protectCredentialKey(path string, create bool) error {
	access := uint32(registry.READ | registry.SET_VALUE | registry.WOW64_64KEY | windows.WRITE_DAC)
	var key registry.Key
	var err error
	if create {
		key, _, err = registry.CreateKey(registry.LOCAL_MACHINE, path, access)
	} else {
		key, err = registry.OpenKey(registry.LOCAL_MACHINE, path, access)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
	}
	if err != nil {
		return err
	}
	defer key.Close()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;CI;KA;;;SY)(A;CI;KA;;;BA)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return fmt.Errorf("credential security descriptor has no restricted DACL")
	}
	return windows.SetSecurityInfo(windows.Handle(key), windows.SE_REGISTRY_KEY,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func migrateLegacyRegistryPath(oldPath, newPath string) error {
	oldKey, err := registry.OpenKey(registry.LOCAL_MACHINE, oldPath, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer oldKey.Close()

	names, err := oldKey.ReadValueNames(0)
	if err != nil {
		return fmt.Errorf("read legacy registry values %s: %w", oldPath, err)
	}
	if len(names) == 0 {
		return nil
	}

	newKey, _, err := registry.CreateKey(registry.LOCAL_MACHINE, newPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create PBS-X registry path %s: %w", newPath, err)
	}
	defer newKey.Close()

	for _, name := range names {
		if _, _, err := newKey.GetStringValue(name); err == nil {
			continue
		}
		value, _, err := oldKey.GetStringValue(name)
		if err != nil {
			continue
		}
		if err := newKey.SetStringValue(name, value); err != nil {
			return fmt.Errorf("migrate registry value %s/%s: %w", oldPath, name, err)
		}
	}
	return nil
}

func migrateFromFileSecretsToRegistry() error {
	data, err := os.ReadFile(legacySecretFilePath)
	if err != nil {
		return nil
	}
	var legacySecrets map[string]string
	if err := json.Unmarshal(data, &legacySecrets); err != nil {
		return nil
	}

	failed := 0
	for b64k, val := range legacySecrets {
		raw, err := base64.StdEncoding.DecodeString(b64k)
		if err != nil {
			failed++
			continue
		}
		parts := strings.SplitN(string(raw), "|", 2)
		if len(parts) != 2 {
			failed++
			continue
		}

		normalized, err := normalizeStoredSecret(val, parts[1])
		if err != nil {
			failed++
			continue
		}
		if isPEMData(normalized) {
			normalized = normalizePEMData(normalized)
		}

		if err := writeSecretDPAPIToRegistry(parts[0], parts[1], normalized); err != nil {
			failed++
			continue
		}
	}

	if failed == 0 {
		_ = os.Remove(legacySecretFilePath)
		_ = os.Remove(legacyKeyFilePath)
	} else {
		log.Error(fmt.Errorf("migration: %d entries failed to migrate", failed), "legacy secrets not removed")
	}
	return nil
}

func writeSecretDPAPIToRegistry(path, key, plaintext string) error {
	// Legacy file entries must land in the protected current vault, never in
	// arbitrary HKLM locations supplied by the old file's encoded path.
	switch {
	case strings.EqualFold(path, AUTH), strings.EqualFold(path, legacyAuth):
		path = AUTH
	case strings.EqualFold(path, CONFIG), strings.EqualFold(path, legacyConfig):
		path = CONFIG
	default:
		return fmt.Errorf("unsupported credential registry path")
	}
	if err := protectCredentialKey(path, true); err != nil {
		return err
	}
	enc, err := dpapi.EncryptMachineLocal(plaintext)
	if err != nil {
		return fmt.Errorf("dpapi encrypt failed: %w", err)
	}

	baseKey, _, err := registry.CreateKey(registry.LOCAL_MACHINE, path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed opening registry key: %w", err)
	}
	defer baseKey.Close()

	return baseKey.SetStringValue(key, enc)
}

func GetEntry(path string, key string, isSecret bool) (*RegistryEntry, error) {
	if err := ensureInitialized(); err != nil {
		return nil, err
	}

	baseKey, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, fmt.Errorf("GetEntry error: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("GetEntry error: %w", err)
	}
	defer baseKey.Close()

	val, _, err := baseKey.GetStringValue(key)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, fmt.Errorf("GetEntry error: key not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("GetEntry error reading key: %w", err)
	}

	if isSecret {
		plain, err := dpapi.Decrypt(val)
		if err != nil {
			return nil, fmt.Errorf("GetEntry decryption error: %w", err)
		}
		val = plain

		unwrapped, err := normalizeStoredSecret(val, key)
		if err != nil {
			log.Error(err, "GetEntry: unwrapBase64Layers", "path", path, "key", key)
		} else {
			val = unwrapped
		}

		log.Debug("GetEntry: decrypted protected entry", "path", path, "key", key)
	}

	if isSecret && isPEMData(val) {
		val = normalizePEMData(val)
	}

	return &RegistryEntry{
		Path:     path,
		Key:      key,
		Value:    val,
		IsSecret: isSecret,
	}, nil
}

func CreateEntry(entry *RegistryEntry) error {
	if err := ensureInitialized(); err != nil {
		return err
	}

	if _, err := GetEntry(entry.Path, entry.Key, entry.IsSecret); err == nil {
		return fmt.Errorf("CreateEntry error: key already exists")
	}

	value := preprocessValue(entry.Value, entry.IsSecret)

	if entry.IsSecret {
		return writeSecretDPAPIToRegistry(entry.Path, entry.Key, value)
	}

	baseKey, _, err := registry.CreateKey(registry.LOCAL_MACHINE, entry.Path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("CreateEntry error: %w", err)
	}
	defer baseKey.Close()

	return baseKey.SetStringValue(entry.Key, value)
}

func CreateEntryIfNotExists(entry *RegistryEntry) error {
	if err := ensureInitialized(); err != nil {
		return err
	}

	baseKey, _, err := registry.CreateKey(
		registry.LOCAL_MACHINE,
		entry.Path,
		registry.QUERY_VALUE|registry.SET_VALUE,
	)
	if err != nil {
		return fmt.Errorf("CreateEntryIfNotExists error: %w", err)
	}
	defer baseKey.Close()

	if _, _, err := baseKey.GetStringValue(entry.Key); err == nil {
		return nil
	} else if err != registry.ErrNotExist {
		return fmt.Errorf("CreateEntryIfNotExists error checking key: %w", err)
	}

	value := preprocessValue(entry.Value, entry.IsSecret)
	if entry.IsSecret {
		value, err = dpapi.EncryptMachineLocal(value)
		if err != nil {
			return fmt.Errorf("CreateEntryIfNotExists error encrypting: %w", err)
		}
	}

	if err := baseKey.SetStringValue(entry.Key, value); err != nil {
		return fmt.Errorf("CreateEntryIfNotExists error writing key: %w", err)
	}
	return nil
}

func UpsertEntry(entry *RegistryEntry) error {
	if err := ensureInitialized(); err != nil {
		return err
	}

	value := preprocessValue(entry.Value, entry.IsSecret)

	if entry.IsSecret {
		return writeSecretDPAPIToRegistry(entry.Path, entry.Key, value)
	}

	baseKey, _, err := registry.CreateKey(registry.LOCAL_MACHINE, entry.Path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("UpsertEntry error: %w", err)
	}
	defer baseKey.Close()

	return baseKey.SetStringValue(entry.Key, value)
}

func DeleteEntry(path string, key string) error {
	if err := ensureInitialized(); err != nil {
		return err
	}

	baseKey, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer baseKey.Close()

	return baseKey.DeleteValue(key)
}
