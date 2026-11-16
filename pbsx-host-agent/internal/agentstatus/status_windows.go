//go:build windows

package agentstatus

import "golang.org/x/sys/windows/registry"

const statusRegistry = `SOFTWARE\PBS-X\PublicStatus`

func readStatus() ([]byte, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, statusRegistry, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return nil, err
	}
	defer key.Close()
	value, _, err := key.GetStringValue("Snapshot")
	return []byte(value), err
}

func writeStatus(data []byte) error {
	// HKLM's inherited ACL permits ordinary users to read, not write. A separate
	// public key avoids weakening the encrypted credential registry's ACL.
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, statusRegistry, registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue("Snapshot", string(data))
}
