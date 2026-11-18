//go:build unix

package backupproxy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An owner can create and rename files in a write/search-only directory, but
// cannot open that directory for fsync. Use real permission enforcement to
// verify that a failed durability barrier cannot become a successful result.
func TestSnapshotDirectorySyncFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires a non-root user for directory read-permission enforcement")
	}
	for _, name := range []string{"root.mpxar.didx", "index.json.blob"} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chmod(directory, 0300); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
			session := &localSession{baseDir: directory, uid: -1, gid: -1, syncWrites: true}
			err := session.writeSnapshotFile(name, []byte("complete fixture data"))
			if !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), "sync snapshot directory") {
				t.Fatalf("directory flush failure did not fail publication: %v", err)
			}
			// The rename itself succeeded; do not mistake a failure earlier in
			// the operation for exercising the missing directory flush.
			if data, err := os.ReadFile(filepath.Join(directory, name)); err != nil || string(data) != "complete fixture data" {
				t.Fatalf("expected the completed file before directory-flush failure: %q %v", data, err)
			}
		})
	}
}

func TestSnapshotPublicationWithoutRequiredSyncAllowsWriteOnlyDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
	session := &localSession{baseDir: directory, uid: -1, gid: -1}
	if err := session.writeSnapshotFile("index.json.blob", []byte("fixture")); err != nil {
		t.Fatal(err)
	}
}

func TestFinishCannotAcknowledgeSnapshotDirectorySyncFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires a non-root user for directory read-permission enforcement")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
	session := &localSession{baseDir: directory, uid: -1, gid: -1, syncWrites: true, manifestBlob: true}
	manifest, err := session.Finish(context.Background())
	if manifest != nil || !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), "sync snapshot directory") {
		t.Fatalf("Finish acknowledged an unflushed manifest: manifest=%v error=%v", manifest, err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".index.json.blob.tmp-") {
			t.Fatal("failed manifest publication leaked its temporary file")
		}
	}
}
