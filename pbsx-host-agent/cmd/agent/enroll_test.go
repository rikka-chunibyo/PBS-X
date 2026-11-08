package main

import (
	"strings"
	"testing"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/enrollment"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
)

func TestEnrollmentCodeFromArgs(t *testing.T) {
	code := enrollment.Prefix + "abc"
	for _, args := range [][]string{{"enroll", code}, {code}} {
		got, requested, err := enrollmentCodeFromArgs(args)
		if err != nil || !requested || got != code {
			t.Fatalf("args %#v returned %q, %t, %v", args, got, requested, err)
		}
	}
	if _, requested, err := enrollmentCodeFromArgs([]string{"enroll"}); err == nil || !requested {
		t.Fatal("missing enrollment code was not rejected")
	}
	if _, requested, err := enrollmentCodeFromArgs([]string{"version"}); err != nil || requested {
		t.Fatal("ordinary command was treated as enrollment")
	}
}

func TestInitialBootstrapEnvironmentDoesNotReplayConsumedCodes(t *testing.T) {
	for _, tc := range []struct {
		name              string
		stored            *string
		cert              bool
		environment, want string
	}{
		{"fresh", nil, false, "first-code", "first-code"},
		{"unconfigured-placeholder", stringPointer(""), false, "first-code", "first-code"},
		{"existing-marker", stringPointer(""), true, "consumed-code", ""},
		{"older-enrolled-profile", nil, true, "consumed-code", ""},
		{"explicit-new-code", stringPointer("new-code"), true, "old-environment-code", "new-code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.stored
			lookup := func(path, key string, _ bool) (*registry.RegistryEntry, error) {
				if path == registry.CONFIG && value != nil {
					return &registry.RegistryEntry{Value: *value}, nil
				}
				if path == registry.AUTH && tc.cert {
					return &registry.RegistryEntry{Value: "existing-certificate"}, nil
				}
				return nil, registry.ErrNotFound
			}
			write := func(entry *registry.RegistryEntry) error { value = stringPointer(entry.Value); return nil }
			if err := initializeBootstrapToken(tc.environment, lookup, write); err != nil {
				t.Fatal(err)
			}
			if value == nil || *value != tc.want {
				t.Fatalf("stored token differs for %s", tc.name)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func TestEnrollmentEnvironmentReplacesExistingValues(t *testing.T) {
	values := enrollment.Values{
		HostID:              "new-host",
		ServerURL:           "https://pbs.example:8007",
		BootstrapToken:      "new-token",
		ServerCAFingerprint: "aa:bb",
	}
	environment := enrollmentEnvironment([]string{
		"PATH=/bin",
		"PBSX_HOST_ID=old-host",
	}, values)
	joined := strings.Join(environment, "\n")
	for _, expected := range []string{
		"PATH=/bin",
		"PBSX_HOST_ID=new-host",
		"PBSX_INIT_SERVER_URL=https://pbs.example:8007",
		"PBSX_INIT_BOOTSTRAP_TOKEN=new-token",
		"PBSX_INIT_SERVER_CA_FINGERPRINT=aa:bb",
		"PBSX_STDOUT_ONLY=true",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("environment is missing %q: %q", expected, joined)
		}
	}
	if strings.Contains(joined, "old-host") {
		t.Fatalf("stale environment value survived: %q", joined)
	}
}
