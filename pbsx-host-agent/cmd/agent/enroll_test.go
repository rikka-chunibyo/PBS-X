package main

import (
	"strings"
	"testing"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/enrollment"
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
		"GODEBUG=old-value",
	}, values)
	joined := strings.Join(environment, "\n")
	for _, expected := range []string{
		"PATH=/bin",
		"PBSX_HOST_ID=new-host",
		"PBSX_INIT_SERVER_URL=https://pbs.example:8007",
		"PBSX_INIT_BOOTSTRAP_TOKEN=new-token",
		"PBSX_INIT_SERVER_CA_FINGERPRINT=aa:bb",
		"GODEBUG=fips140=on",
		"PBSX_STDOUT_ONLY=true",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("environment is missing %q: %q", expected, joined)
		}
	}
	if strings.Contains(joined, "old-host") || strings.Contains(joined, "old-value") {
		t.Fatalf("stale environment value survived: %q", joined)
	}
}
