package agent

import (
	"fmt"
	"testing"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
)

func TestStorageCredentialSelection(t *testing.T) {
	for _, test := range []struct {
		name, mode, override, want string
		fail                       bool
	}{
		{name: "legacy automatic", want: "device@pbs!auto:auto-secret"},
		{name: "explicit automatic", mode: "auto", want: "device@pbs!auto:auto-secret"},
		{name: "custom", mode: "custom", override: `{"auth_id":"restore@pbs!shared","secret":"custom-secret"}`, want: "restore@pbs!shared:custom-secret"},
		{name: "unreadable custom never falls back", mode: "custom", fail: true},
		{name: "corrupt custom never falls back", mode: "custom", override: `broken`, fail: true},
		{name: "empty custom never falls back", mode: "custom", override: `{}`, fail: true},
		{name: "unknown mode", mode: "future", fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			get := func(path, key string, secret bool) (*registry.RegistryEntry, error) {
				values := map[string]string{"StorageCredentialMode": test.mode, "StorageCredentials": test.override, "BackupAuthID": "device@pbs!auto", "BackupSecret": "auto-secret"}
				value := values[key]
				if value == "" {
					return nil, fmt.Errorf("missing")
				}
				if key == "StorageCredentials" && !secret {
					t.Fatal("custom credential read without decryption")
				}
				return &registry.RegistryEntry{Value: value}, nil
			}
			got, err := storageAuthTokenFrom(get)
			if (err != nil) != test.fail || got != test.want {
				t.Fatalf("selection got token-match=%t error=%v", got == test.want, err)
			}
		})
	}
}

func TestStorageCredentialValidation(t *testing.T) {
	for _, c := range []StorageCredentials{{}, {AuthID: "user@pbs!token", Secret: "secret"}} {
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []StorageCredentials{{AuthID: " "}, {AuthID: "@pbs!token", Secret: "secret"}, {AuthID: "root@pam", Secret: "secret"}, {AuthID: "u@pbs!token"}, {Secret: "secret"}, {AuthID: "u@pbs!token\n", Secret: "secret"}} {
		if c.Validate() == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
}
