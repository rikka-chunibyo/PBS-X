package agent

import (
	"strings"
	"testing"
)

func TestBackupIDIsIndependentOfDeviceID(t *testing.T) {
	for _, id := range []string{"a", "_photos", "photos-2026.11", strings.Repeat("a", 100)} {
		if err := ValidateBackupID(id); err != nil {
			t.Fatal(id, err)
		}
	}
	for _, id := range []string{"", ".", "..", "../escape", "a/b", "a\\b", "has space", "a\nheader", "é"} {
		if ValidateBackupID(id) == nil {
			t.Fatalf("unsafe ID accepted: %q", id)
		}
	}
	if ValidateHostID("a") == nil || ValidateHostID("_photos") == nil {
		t.Fatal("device validation was broadened")
	}
}
