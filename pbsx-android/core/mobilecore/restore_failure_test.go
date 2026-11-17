package mobilecore

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
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
			if mode == "replace" {
				writeFixture(t, target, "file.txt", "original")
			}
			_, err := restoreSelected(context.Background(), failedRestorePayload{fs}, []string{"file.txt"}, mode, target, nil)
			if err == nil {
				t.Fatal("injected read failure did not fail restore")
			}
			if mode == "replace" {
				assertContent(t, target, "file.txt", "original")
			} else if _, err := os.Stat(target.full("file.txt")); !os.IsNotExist(err) {
				t.Fatal("failed restore left a partial file at its final name")
			}
			assertNoRestoreStage(t, target)
			if _, err := restoreSelected(context.Background(), fs, []string{"file.txt"}, mode, target, nil); err != nil {
				t.Fatal(err)
			}
			assertContent(t, target, "file.txt", "complete backup bytes")
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
