package pxar

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	pxarlib "github.com/pbs-plus/pxar"
)

func TestCrossFilesystemHardlinkFailurePreservesDestination(t *testing.T) {
	shared, err := os.MkdirTemp("/dev/shm", "pbsx-hardlink-test-")
	if err != nil {
		t.Skipf("shared-memory temporary directory unavailable: %v", err)
	}
	defer os.RemoveAll(shared)
	source := filepath.Join(shared, "source")
	dest := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(source, []byte("ARCHIVED"), 0600); err != nil {
		t.Fatal(err)
	}
	// Establish EXDEV before invoking the restore; some hosts put /tmp in shm.
	probe := filepath.Join(filepath.Dir(dest), "probe")
	if err := os.Link(source, probe); !errors.Is(err, syscall.EXDEV) {
		_ = os.Remove(probe)
		t.Skipf("fixture paths are not on different filesystems: %v", err)
	}
	if err := os.WriteFile(dest, []byte("LOCAL-ORIGINAL"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := openRestoreFS("/")
	if err != nil {
		t.Fatal(err)
	}
	defer root.root.Close()
	links := newHardlinkIndex()
	links.register("source", source)
	state := &restoreState{target: root, hl: links}
	job := restoreJob{dest: dest, srcPath: "alias", info: pxarlib.FileInfo{FileType: pxarlib.FileTypeHardlink, LinkTarget: "source"}}
	if err := processJob(t.Context(), state, job); err == nil {
		t.Fatal("expected cross-filesystem hardlink failure")
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "LOCAL-ORIGINAL" {
		t.Fatalf("failed restore lost destination: %q %v", got, err)
	}
}
