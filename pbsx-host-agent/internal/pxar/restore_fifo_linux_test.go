//go:build linux

package pxar

import (
	"context"
	pxar "github.com/pbs-plus/pxar"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreFIFOWithoutWriter(t *testing.T) {
	for _, mode := range []RestoreMode{RestoreModeNormal, RestoreModeNoAttr} {
		fixture := &restoreFixture{entries: []pxar.FileInfo{{FileName: []byte("pipe"), FileType: pxar.FileTypeFifo, RawMode: 0600, RawUID: uint32(os.Getuid()), RawGID: uint32(os.Getgid()), MtimeSecs: 1}}}
		dest := t.TempDir()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		done := make(chan error, 1)
		go func() { done <- runFixtureRestore(ctx, fixture, dest, mode) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("FIFO restore blocked without a writer")
		}
		cancel()
		info, err := os.Lstat(filepath.Join(dest, "folder", "pipe"))
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatal("FIFO missing", err)
		}
	}
}

func TestRestoreSocketMetadataWithoutOpeningSocket(t *testing.T) {
	fixture := &restoreFixture{entries: []pxar.FileInfo{{FileName: []byte("socket"), FileType: pxar.FileTypeSocket, RawMode: 0600, RawUID: uint32(os.Getuid()), RawGID: uint32(os.Getgid()), MtimeSecs: 1}}}
	dest := t.TempDir()
	if err := runFixtureRestore(t.Context(), fixture, dest, RestoreModeNormal); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dest, "folder", "socket"))
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatal("socket missing", err)
	}
	if info.ModTime().Unix() != 1 || info.Mode().Perm() != 0600 {
		t.Fatal("socket metadata lost", info)
	}
}
