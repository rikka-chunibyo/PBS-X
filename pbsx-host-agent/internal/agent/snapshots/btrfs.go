package snapshots

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type BtrfsSnapshotHandler struct{}

func (b *BtrfsSnapshotHandler) CreateSnapshot(jobID string, sourcePath string) (Snapshot, error) {
	if !b.IsSupported(sourcePath) {
		return Snapshot{}, fmt.Errorf("source path %q is not on Btrfs", sourcePath)
	}
	mount, err := findMount(sourcePath)
	if err != nil {
		return Snapshot{}, err
	}
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return Snapshot{}, err
	}
	rel, err := filepath.Rel(mount.Target, abs)
	if err != nil {
		return Snapshot{}, err
	}

	base := filepath.Join(mount.Target, ".pbsx-snapshots")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return Snapshot{}, fmt.Errorf("create Btrfs snapshot directory: %w", err)
	}
	name := fmt.Sprintf("%s-%d", safeSnapshotComponent(jobID), time.Now().UTC().UnixNano())
	backend := filepath.Join(base, name)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// Snapshot the mounted subvolume, then expose the requested subdirectory
	// inside that read-only snapshot. This works for jobs targeting /home/foo
	// instead of requiring every configured source to itself be a subvolume.
	if output, err := exec.CommandContext(ctx, "btrfs", "subvolume", "snapshot", "-r", mount.Target, backend).CombinedOutput(); err != nil {
		return Snapshot{}, fmt.Errorf("create Btrfs snapshot: %s: %w", strings.TrimSpace(string(output)), err)
	}
	root := backend
	if rel != "." && rel != "" {
		root = filepath.Join(backend, rel)
	}
	snapshot := Snapshot{Path: root, BackendPath: backend, TimeStarted: time.Now(), SourcePath: sourcePath, Handler: b}
	if rel == "." || rel == "" {
		snapshot.Excludes = append(snapshot.Excludes, ".pbsx-snapshots")
	}
	return snapshot, nil
}

func (b *BtrfsSnapshotHandler) DeleteSnapshot(snapshot Snapshot) error {
	backend := snapshot.BackendPath
	if backend == "" {
		backend = snapshot.Path
	}
	if output, err := exec.Command("btrfs", "subvolume", "delete", backend).CombinedOutput(); err != nil {
		return fmt.Errorf("delete Btrfs snapshot %q: %s: %w", backend, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (b *BtrfsSnapshotHandler) IsSupported(sourcePath string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	mount, err := findMount(sourcePath)
	return err == nil && mount.FSType == "btrfs"
}
