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

type LVMSnapshotHandler struct{}

type lvmVolume struct {
	VG      string
	LV      string
	Path    string
	SegType string
	Mount   mountInfo
}

func lvmForSource(sourcePath string) (lvmVolume, error) {
	if runtime.GOOS != "linux" {
		return lvmVolume{}, fmt.Errorf("LVM is only available on Linux")
	}
	mount, err := findMount(sourcePath)
	if err != nil {
		return lvmVolume{}, err
	}
	out, err := exec.Command("lvs", "--noheadings", "--separator", "|", "-o", "vg_name,lv_name,lv_path,segtype", mount.Source).Output()
	if err != nil {
		return lvmVolume{}, fmt.Errorf("resolve LVM volume for %q (%s): %w", sourcePath, mount.Source, err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) < 4 {
		return lvmVolume{}, fmt.Errorf("unexpected lvs output: %q", strings.TrimSpace(string(out)))
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return lvmVolume{}, fmt.Errorf("incomplete LVM metadata for %q", mount.Source)
	}
	return lvmVolume{VG: parts[0], LV: parts[1], Path: parts[2], SegType: parts[3], Mount: mount}, nil
}

func (l *LVMSnapshotHandler) CreateSnapshot(jobID string, sourcePath string) (Snapshot, error) {
	volume, err := lvmForSource(sourcePath)
	if err != nil {
		return Snapshot{}, err
	}
	name := fmt.Sprintf("pbsx_%s_%d", safeSnapshotComponent(jobID), time.Now().UTC().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	args := []string{"--snapshot", "--name", name}
	// Thin LVs allocate snapshot blocks from their thin pool and must not be
	// given a classic COW size. Classic LVs use a percentage of origin size
	// rather than the old hard-coded 1 GiB snapshot.
	if !strings.Contains(strings.ToLower(volume.SegType), "thin") {
		cowSize := strings.TrimSpace(os.Getenv("PBSX_LVM_SNAPSHOT_EXTENTS"))
		if cowSize == "" {
			cowSize = "20%ORIGIN"
		}
		args = append(args, "--extents", cowSize)
	}
	args = append(args, volume.Path)
	if output, err := exec.CommandContext(ctx, "lvcreate", args...).CombinedOutput(); err != nil {
		return Snapshot{}, fmt.Errorf("create LVM snapshot: %s: %w", strings.TrimSpace(string(output)), err)
	}

	device := filepath.Join("/dev", volume.VG, name)
	mountBase := filepath.Join("/run/pbsx-agent/snapshots", safeSnapshotComponent(jobID))
	mountPath := filepath.Join(mountBase, fmt.Sprintf("%d", time.Now().UTC().UnixNano()))
	if err := os.MkdirAll(mountPath, 0o700); err != nil {
		_ = exec.Command("lvremove", "-f", device).Run()
		return Snapshot{}, fmt.Errorf("create LVM snapshot mountpoint: %w", err)
	}

	mountOpts := "ro"
	if strings.EqualFold(volume.Mount.FSType, "xfs") {
		mountOpts = "ro,nouuid"
	}
	if output, err := exec.Command("mount", "-o", mountOpts, "-t", volume.Mount.FSType, device, mountPath).CombinedOutput(); err != nil {
		_ = os.RemoveAll(mountPath)
		_ = exec.Command("lvremove", "-f", device).Run()
		return Snapshot{}, fmt.Errorf("mount LVM snapshot %q: %s: %w", device, strings.TrimSpace(string(output)), err)
	}

	abs, _ := filepath.Abs(sourcePath)
	rel, err := filepath.Rel(volume.Mount.Target, abs)
	if err != nil {
		_ = exec.Command("umount", mountPath).Run()
		_ = os.RemoveAll(mountPath)
		_ = exec.Command("lvremove", "-f", device).Run()
		return Snapshot{}, err
	}
	root := mountPath
	if rel != "." && rel != "" {
		root = filepath.Join(mountPath, rel)
	}
	return Snapshot{Path: root, BackendPath: device, TimeStarted: time.Now(), SourcePath: sourcePath, Handler: l}, nil
}

func (l *LVMSnapshotHandler) DeleteSnapshot(snapshot Snapshot) error {
	var errs []string
	// Path may point at a subdirectory inside the mount; find the actual mount.
	if mount, err := findMount(snapshot.Path); err == nil && strings.HasPrefix(filepath.Clean(mount.Target), "/run/pbsx-agent/snapshots/") {
		if output, err := exec.Command("umount", mount.Target).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("unmount %q: %s: %v", mount.Target, strings.TrimSpace(string(output)), err))
		} else {
			_ = os.RemoveAll(mount.Target)
		}
	}
	backend := snapshot.BackendPath
	if backend != "" {
		if output, err := exec.Command("lvremove", "-f", backend).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("remove %q: %s: %v", backend, strings.TrimSpace(string(output)), err))
		}
	}
	if len(errs) != 0 {
		return fmt.Errorf("delete LVM snapshot: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (l *LVMSnapshotHandler) IsSupported(sourcePath string) bool {
	_, err := lvmForSource(sourcePath)
	return err == nil
}
