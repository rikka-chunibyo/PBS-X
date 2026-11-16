//go:build unix

package agentstatus

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const statusDirectory = "/var/lib/pbsx-agent-status"

func readStatus() ([]byte, error) {
	f, err := os.Open(filepath.Join(statusDirectory, "status.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, (16<<10)+1))
}

func writeStatus(data []byte) error {
	if err := os.MkdirAll(statusDirectory, 0755); err != nil {
		return err
	}
	info, err := os.Lstat(statusDirectory)
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != 0 || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("unsafe local status directory")
	}
	// The service deliberately uses a restrictive umask. This directory contains
	// only the public status, so ordinary desktop/CLI users must traverse it.
	if err := os.Chmod(statusDirectory, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(statusDirectory, ".status-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0644); err == nil {
		_, err = f.Write(data)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filepath.Join(statusDirectory, "status.json"))
}
