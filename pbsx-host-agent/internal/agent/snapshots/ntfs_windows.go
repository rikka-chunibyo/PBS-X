//go:build windows

package snapshots

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mxk/go-vss"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
)

type NtfsSnapshotHandler struct{}

func (w *NtfsSnapshotHandler) CreateSnapshot(jobID string, sourcePath string) (Snapshot, error) {
	if sourcePath == "" {
		return Snapshot{}, errors.New("empty source path")
	}

	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return Snapshot{}, err
	}
	source, err := planWindowsSnapshotSource(absSource)
	if err != nil {
		return Snapshot{}, fmt.Errorf("source path %q has no Windows volume: %w", sourcePath, err)
	}
	volName := source.Volume
	rel := source.Relative

	vssFolder, err := getVSSFolder()
	if err != nil {
		return Snapshot{}, fmt.Errorf("error getting VSS folder: %w", err)
	}

	digest := sha256.Sum256([]byte(strings.ToLower(absSource)))
	sourceKey := hex.EncodeToString(digest[:6])
	snapshotName := fmt.Sprintf("%s-%s-%s", safeSnapshotComponent(jobID), safeSnapshotComponent(volName), sourceKey)
	snapshotPath := filepath.Join(vssFolder, snapshotName)
	timeStarted := time.Now()

	cleanupExistingSnapshot(snapshotPath)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := createSnapshotWithRetry(ctx, snapshotPath, volName); err != nil {
		cleanupExistingSnapshot(snapshotPath)
		return Snapshot{}, fmt.Errorf("snapshot creation failed: %w", err)
	}

	_, err = vss.Get(snapshotPath)
	if err != nil {
		cleanupExistingSnapshot(snapshotPath)
		return Snapshot{}, fmt.Errorf("snapshot validation failed: %w", err)
	}

	root := snapshotPath
	if rel != "" {
		root = filepath.Join(snapshotPath, rel)
	}
	return Snapshot{
		Path:        root,
		BackendPath: snapshotPath,
		TimeStarted: timeStarted,
		SourcePath:  sourcePath,
		Handler:     w,
	}, nil
}

func (w *NtfsSnapshotHandler) DeleteSnapshot(snapshot Snapshot) error {
	backend := snapshot.BackendPath
	if backend == "" {
		backend = snapshot.Path
	}
	if err := vss.Remove(backend); err != nil {
		return fmt.Errorf("failed to delete VSS snapshot: %w", err)
	}

	if vssFolder, err := getVSSFolder(); err == nil {
		if strings.HasPrefix(backend, vssFolder) {
			_ = os.Remove(backend)
		}
	}

	return nil
}

func (w *NtfsSnapshotHandler) IsSupported(sourcePath string) bool {
	// Assume VSS is supported on Windows
	return true
}

func getVSSFolder() (string, error) {
	tmpDir := os.TempDir()
	configBasePath := filepath.Join(tmpDir, "pbsx-vss")
	if err := os.MkdirAll(configBasePath, 0750); err != nil {
		return "", fmt.Errorf("failed to create VSS directory %q: %w", configBasePath, err)
	}
	return configBasePath, nil
}

func reregisterVSSWriters() error {
	services := []string{
		"Winmgmt", // Windows Management Instrumentation
		"VSS",
		"swprv",
	}

	for _, svc := range services {
		if err := exec.Command("net", "stop", svc).Run(); err != nil {
			return fmt.Errorf("failed to stop service %s: %w", svc, err)
		}
	}

	for _, service := range slices.Backward(services) {
		if err := exec.Command("net", "start", service).Run(); err != nil {
			return fmt.Errorf("failed to start service %s: %w", service, err)
		}
	}

	return nil
}

func createSnapshotWithRetry(ctx context.Context, snapshotPath, volName string) error {
	const retryInterval = time.Second
	var lastError error

	for attempts := range 2 {
		for {
			if err := vss.CreateLink(snapshotPath, volName); err == nil {
				return nil
			} else if !strings.Contains(err.Error(), "shadow copy operation is already in progress") {
				lastError = err
				if attempts == 0 && (strings.Contains(err.Error(), "VSS") ||
					strings.Contains(err.Error(), "shadow copy")) {
					log.Error(err, "vss error detected, attempting to re-register")
					if reregErr := reregisterVSSWriters(); reregErr != nil {
						log.Error(reregErr, "failed to re-register VSS writers")
					}
					break
				}
				return fmt.Errorf("%w: %w", ErrSnapshotCreation, err)
			}

			select {
			case <-ctx.Done():
				return ErrSnapshotTimeout
			case <-time.After(retryInterval):
				continue
			}
		}
	}

	return fmt.Errorf("%w: %w", ErrSnapshotCreation, lastError)
}

func cleanupExistingSnapshot(path string) {
	if sc, err := vss.Get(path); err == nil {
		_ = vss.Remove(sc.ID)
	}

	if vssFolder, err := getVSSFolder(); err == nil {
		if strings.HasPrefix(path, vssFolder) {
			_ = os.Remove(path)
		}
	}
}
