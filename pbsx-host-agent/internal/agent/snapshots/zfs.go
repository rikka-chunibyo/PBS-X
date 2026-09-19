package snapshots

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type ZFSSnapshotHandler struct{}

type zfsDataset struct {
	Name       string
	Mountpoint string
}

func zfsDatasetForPath(sourcePath string) (zfsDataset, string, error) {
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return zfsDataset{}, "", err
	}
	out, err := exec.Command("zfs", "list", "-H", "-t", "filesystem", "-o", "name,mountpoint").Output()
	if err != nil {
		return zfsDataset{}, "", fmt.Errorf("list ZFS filesystems: %w", err)
	}
	var best zfsDataset
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(fields) != 2 {
			fields = strings.Fields(strings.TrimSpace(line))
			if len(fields) != 2 {
				continue
			}
		}
		mp := filepath.Clean(fields[1])
		if mp == "none" || mp == "legacy" || !filepath.IsAbs(mp) {
			continue
		}
		rel, err := filepath.Rel(mp, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if len(mp) > len(best.Mountpoint) {
			best = zfsDataset{Name: fields[0], Mountpoint: mp}
		}
	}
	if best.Name == "" {
		return zfsDataset{}, "", fmt.Errorf("%q is not inside a mounted ZFS dataset", sourcePath)
	}
	rel, _ := filepath.Rel(best.Mountpoint, abs)
	return best, rel, nil
}

func (z *ZFSSnapshotHandler) CreateSnapshot(jobID string, sourcePath string) (Snapshot, error) {
	if runtime.GOOS == "windows" {
		return Snapshot{}, fmt.Errorf("ZFS snapshots are unavailable on Windows")
	}
	dataset, rel, err := zfsDatasetForPath(sourcePath)
	if err != nil {
		return Snapshot{}, err
	}

	shortName := fmt.Sprintf("pbsx-%s-%d", safeSnapshotComponent(jobID), time.Now().UTC().UnixNano())
	backend := dataset.Name + "@" + shortName
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "zfs", "snapshot", backend).CombinedOutput(); err != nil {
		return Snapshot{}, fmt.Errorf("create ZFS snapshot %q: %s: %w", backend, strings.TrimSpace(string(output)), err)
	}

	root := filepath.Join(dataset.Mountpoint, ".zfs", "snapshot", shortName)
	if rel != "." && rel != "" {
		root = filepath.Join(root, rel)
	}
	snapshot := Snapshot{Path: root, BackendPath: backend, TimeStarted: time.Now(), SourcePath: sourcePath, Handler: z}
	if rel == "." || rel == "" {
		snapshot.Excludes = append(snapshot.Excludes, ".zfs")
	}
	return snapshot, nil
}

func (z *ZFSSnapshotHandler) DeleteSnapshot(snapshot Snapshot) error {
	backend := snapshot.BackendPath
	if backend == "" {
		backend = snapshot.Path
	}
	if output, err := exec.Command("zfs", "destroy", backend).CombinedOutput(); err != nil {
		return fmt.Errorf("destroy ZFS snapshot %q: %s: %w", backend, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (z *ZFSSnapshotHandler) IsSupported(sourcePath string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	_, _, err := zfsDatasetForPath(sourcePath)
	return err == nil
}
