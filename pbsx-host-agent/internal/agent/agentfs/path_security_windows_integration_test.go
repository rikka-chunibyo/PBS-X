//go:build windows

package agentfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
)

func TestResolvedParentWithinVSSSnapshot(t *testing.T) {
	if os.Getenv("PBSX_WINDOWS_VSS_INTEGRATION") != "1" {
		t.Skip("set PBSX_WINDOWS_VSS_INTEGRATION=1 on an elevated Windows test host")
	}

	source := t.TempDir()
	nested := filepath.Join(source, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "probe.txt"), []byte("vss path probe"), 0o600); err != nil {
		t.Fatalf("create VSS path probe: %v", err)
	}

	snapshot, err := snapshots.Manager.CreateSnapshot("agentfs-vss-path", source)
	if err != nil {
		t.Fatalf("create VSS snapshot: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Handler.DeleteSnapshot(snapshot) })

	server := NewServer("agentfs-vss-path", "standard", snapshot)
	path, err := server.safeAbs(`nested\probe.txt`)
	if err != nil {
		t.Fatalf("resolve file inside VSS snapshot: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file inside VSS snapshot: %v", err)
	}
	if string(data) != "vss path probe" {
		t.Fatalf("VSS path probe = %q", data)
	}
}
