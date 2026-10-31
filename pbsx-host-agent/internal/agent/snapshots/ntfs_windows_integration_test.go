//go:build windows

package snapshots

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNTFSSnapshotLive exercises the real Windows VSS provider. It is opt-in
// because it requires an elevated Windows test host and creates a volume shadow
// copy while it runs.
func TestNTFSSnapshotLive(t *testing.T) {
	if os.Getenv("PBSX_WINDOWS_VSS_INTEGRATION") != "1" {
		t.Skip("set PBSX_WINDOWS_VSS_INTEGRATION=1 on an elevated Windows test host")
	}

	source := t.TempDir()
	livePath := filepath.Join(source, "vss-probe.txt")
	const before = "content captured before the VSS snapshot\n"
	const after = "content written after the VSS snapshot\n"
	if err := os.WriteFile(livePath, []byte(before), 0o600); err != nil {
		t.Fatalf("create VSS probe: %v", err)
	}

	handler := &NtfsSnapshotHandler{}
	snapshot, err := handler.CreateSnapshot("windows-vss-integration", source)
	if err != nil {
		t.Fatalf("create live VSS snapshot: %v", err)
	}
	removed := false
	t.Cleanup(func() {
		if !removed {
			_ = handler.DeleteSnapshot(snapshot)
		}
	})

	if snapshot.Direct {
		t.Fatal("live VSS snapshot unexpectedly uses direct-read mode")
	}
	if snapshot.BackendPath == "" {
		t.Fatal("live VSS snapshot has no backend path for cleanup")
	}

	snapshotPath := filepath.Join(snapshot.Path, filepath.Base(livePath))
	got, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read probe through VSS snapshot: %v", err)
	}
	if string(got) != before {
		t.Fatalf("initial snapshot content = %q, want %q", got, before)
	}

	if err := os.WriteFile(livePath, []byte(after), 0o600); err != nil {
		t.Fatalf("mutate live probe after snapshot: %v", err)
	}
	got, err = os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("reread probe through VSS snapshot: %v", err)
	}
	if string(got) != before {
		t.Fatalf("snapshot changed with live file: got %q, want %q", got, before)
	}

	if err := handler.DeleteSnapshot(snapshot); err != nil {
		t.Fatalf("delete live VSS snapshot: %v", err)
	}
	removed = true
}
