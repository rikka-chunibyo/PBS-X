//go:build agent && unix

package migration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pbs-x/pbsx-host-agent/internal/conf"
)

// OldStatePath is the legacy path used before v0.79.0.
var OldStatePath = "/var/lib/pbs-plus"

// TryMigrate attempts to migrate data from legacy paths to new paths.
// This is a best-effort operation - if it fails (e.g., due to permissions),
// the agent continues to use the legacy paths automatically.
// Returns true if migration was successful, false otherwise.
func TryMigrate() bool {
	oldInfo, err := os.Stat(OldStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return true // Consider this success - nothing to migrate
		}
		fmt.Printf("[pbs-x] migration check failed: %v\n", err)
		return false
	}

	if !oldInfo.IsDir() {
		return true
	}

	newPath := "/var/lib/pbsx-agent"
	newInfo, err := os.Stat(newPath)
	if err == nil && newInfo.IsDir() {
		entries, err := os.ReadDir(newPath)
		if err == nil && len(entries) > 0 {
			// On the first migration run conf may deliberately keep using the legacy
			// directory until restart. Once the process has selected the PBS-X path,
			// the old directory can safely be moved aside.
			if conf.StatePrefix != OldStatePath {
				backupPath := OldStatePath + ".backup.legacy"
				if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
					_ = os.Rename(OldStatePath, backupPath)
				}
			}
			fmt.Printf("[pbs-x] migration: new path already exists with content, using it on next/current start\n")
			return true
		}
	}

	if err := os.MkdirAll(newPath, 0700); err != nil {
		fmt.Printf("[pbs-x] migration: cannot create new directory %s (will use legacy): %v\n",
			newPath, err)
		return false
	}

	// Test write permission in new directory
	testFile := filepath.Join(newPath, ".migration_test")
	if f, err := os.Create(testFile); err != nil {
		fmt.Printf("[pbs-x] migration: cannot write to new directory %s (will use legacy): %v\n",
			newPath, err)
		return false
	} else {
		_ = f.Close()
		_ = os.Remove(testFile)
	}

	backupPath := OldStatePath + ".backup.legacy"
	if _, err := os.Stat(backupPath); err == nil {
		fmt.Printf("[pbs-x] migration: backup already exists, migration may have been partially completed\n")
		return true
	}

	fmt.Printf("[pbs-x] migration: migrating data from %s to %s\n", OldStatePath, newPath)

	if err := migrateDirectory(OldStatePath, newPath); err != nil {
		fmt.Printf("[pbs-x] migration: failed during migration (will use legacy): %v\n", err)
		return false
	}

	if conf.StatePrefix == OldStatePath {
		// conf selected the legacy path during package initialization. Keep it in
		// place for this process so we do not recreate a fresh legacy tree after
		// migrating. On the next start, the populated PBS-X path wins and the old
		// tree is moved aside.
		fmt.Printf("[pbs-x] migration: copied state; restart will switch to %s\n", newPath)
		return true
	}

	if err := os.Rename(OldStatePath, backupPath); err != nil {
		fmt.Printf("[pbs-x] migration: data migrated but failed to backup old directory: %v\n", err)
	} else {
		fmt.Printf("[pbs-x] migration: complete, backed up old directory to %s\n", backupPath)
	}

	return true
}

func migrateDirectory(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		info, err := entry.Info()
		if err != nil {
			fmt.Printf("[pbs-x] migration warning: skipping file %s: %v\n", srcPath, err)
			continue
		}

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, info.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dstPath, err)
			}
			if err := migrateDirectory(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to copy file %s to %s: %w", srcPath, dstPath, err)
			}
		}

		// Preserve permissions
		if err := os.Chmod(dstPath, info.Mode()); err != nil {
			fmt.Printf("[pbs-x] migration warning: failed to set permissions on %s: %v\n", dstPath, err)
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return destFile.Sync()
}
