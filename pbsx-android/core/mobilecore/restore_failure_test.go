package mobilecore

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/pbs-plus/pxar/vfs"
)

type failedRestorePayload struct{ vfs.FileSystem }
type brokenRestoreReader struct{ sent bool }

func (r *brokenRestoreReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.ErrUnexpectedEOF
	}
	r.sent = true
	return copy(p, "partial"), nil
}
func (f failedRestorePayload) ReadContentReader(uint64, uint64) (io.ReadCloser, error) {
	return io.NopCloser(&brokenRestoreReader{}), nil
}

func assertNoRestoreStage(t *testing.T, target *fsDocuments) {
	t.Helper()
	entries, err := os.ReadDir(target.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pbsx-stage-") {
			t.Fatal("partial restore file was not cleaned up")
		}
	}
}

func TestFailedRestorePreservesOriginalAndRetryRestoresMissingFiles(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"file.txt": "complete backup bytes"})
	for _, mode := range []string{"replace", "skip", "rename", "fail"} {
		t.Run(mode, func(t *testing.T) {
			target := &fsDocuments{root: t.TempDir()}
			if mode == "replace" || mode == "rename" {
				writeFixture(t, target, "file.txt", "original")
			}
			_, err := restoreSelected(context.Background(), failedRestorePayload{fs}, []string{"file.txt"}, mode, target, nil)
			if err == nil {
				t.Fatal("injected read failure did not fail restore")
			}
			if mode == "replace" || mode == "rename" {
				assertContent(t, target, "file.txt", "original")
			} else if _, err := os.Stat(target.full("file.txt")); !os.IsNotExist(err) {
				t.Fatal("failed restore left a partial file at its final name")
			}
			assertNoRestoreStage(t, target)
			if _, err := restoreSelected(context.Background(), fs, []string{"file.txt"}, mode, target, nil); err != nil {
				t.Fatal(err)
			}
			if mode == "rename" {
				assertContent(t, target, "file.txt", "original")
				assertContent(t, target, "file (restored 1).txt", "complete backup bytes")
			} else {
				assertContent(t, target, "file.txt", "complete backup bytes")
			}
		})
	}
}

type cancelRestoreDocuments struct {
	*fsDocuments
	cancel context.CancelFunc
}

func (d cancelRestoreDocuments) OpenWrite(p string) (int64, error) {
	fd, err := d.fsDocuments.OpenWrite(p)
	d.cancel()
	return fd, err
}

func TestCancellationAfterStagingCannotTruncateOriginal(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"file.txt": "complete backup bytes"})
	target := &fsDocuments{root: t.TempDir()}
	writeFixture(t, target, "file.txt", "original")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := restoreSelected(ctx, fs, []string{"file.txt"}, "replace", cancelRestoreDocuments{target, cancel}, nil)
	if !errors.Is(err, context.Canceled) && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	assertContent(t, target, "file.txt", "original")
	assertNoRestoreStage(t, target)
}

type refusedRestoreCommit struct{ *fsDocuments }

func (d refusedRestoreCommit) CommitFile(string, string, bool) error {
	return errors.New("provider refused promotion")
}

func TestFailedCommitPreservesOriginalAndRemovesStage(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"file.txt": "complete backup bytes"})
	target := &fsDocuments{root: t.TempDir()}
	writeFixture(t, target, "file.txt", "original")
	_, err := restoreSelected(context.Background(), fs, []string{"file.txt"}, "replace", refusedRestoreCommit{target}, nil)
	if err == nil || !strings.Contains(err.Error(), "refused promotion") {
		t.Fatal(err)
	}
	assertContent(t, target, "file.txt", "original")
	assertNoRestoreStage(t, target)
}

type shortRestorePayload struct{ vfs.FileSystem }

func (f shortRestorePayload) ReadContentReader(start, end uint64) (io.ReadCloser, error) {
	reader, err := f.FileSystem.ReadContentReader(start, end)
	if err != nil {
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(reader, 1), reader}, nil
}

func TestShortPayloadNeverPromotesIncompleteRestore(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"file.txt": "complete backup bytes"})
	target := &fsDocuments{root: t.TempDir()}
	writeFixture(t, target, "file.txt", "original")
	_, err := restoreSelected(context.Background(), shortRestorePayload{fs}, []string{"file.txt"}, "replace", target, nil)
	if err == nil || !strings.Contains(err.Error(), "size does not match") {
		t.Fatal(err)
	}
	assertContent(t, target, "file.txt", "original")
	assertNoRestoreStage(t, target)
}

type fullRestoreDisk struct{ *fsDocuments }

func (d fullRestoreDisk) OpenWrite(p string) (int64, error) {
	fd, err := d.fsDocuments.OpenWrite(p)
	if err != nil {
		return fd, err
	}
	syscall.Close(int(fd))
	full, err := syscall.Open("/dev/full", syscall.O_WRONLY, 0)
	return int64(full), err
}

func TestFullDestinationDiskCannotDamageExistingFile(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"file.txt": "complete backup bytes"})
	target := &fsDocuments{root: t.TempDir()}
	writeFixture(t, target, "file.txt", "original")
	_, err := restoreSelected(context.Background(), fs, []string{"file.txt"}, "replace", fullRestoreDisk{target}, nil)
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
	assertContent(t, target, "file.txt", "original")
	assertNoRestoreStage(t, target)
}
