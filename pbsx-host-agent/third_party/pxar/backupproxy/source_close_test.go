package backupproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
)

type failedSourceClose struct {
	io.Reader
	err    error
	closes int
}

type closeTrackingStore struct {
	RemoteStore
	closes int
}
type closeTrackingSession struct {
	BackupSession
	owner *closeTrackingStore
}

func (s *closeTrackingStore) StartSession(ctx context.Context, cfg BackupConfig) (BackupSession, error) {
	session, err := s.RemoteStore.StartSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &closeTrackingSession{BackupSession: session, owner: s}, nil
}
func (s *closeTrackingSession) Close() error { s.owner.closes++; return s.BackupSession.Close() }

func (r *failedSourceClose) Close() error { r.closes++; return r.err }

func TestBackupPropagatesSourceCloseValidationError(t *testing.T) {
	for _, split := range []bool{false, true} {
		fs := newMemFS()
		fs.addDir(".", "", 0755)
		fs.addFile("./file", ".", []byte("data"), 0644)
		server, output := newTestServer(t, fs)
		want := errors.New("source changed at close")
		reader := &failedSourceClose{Reader: bytes.NewReader([]byte("data")), err: want}
		client := fs.provider()
		client.openFileFn = func(context.Context, string) (io.ReadCloser, uint64, error) { return reader, 4, nil }
		server.client = client
		tracked := &closeTrackingStore{RemoteStore: server.store}
		server.store = tracked
		config := BackupConfig{BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000000}
		var err error
		if split {
			_, err = server.RunSplitBackup(t.Context(), ".", config)
		} else {
			_, err = server.RunBackup(t.Context(), ".", config)
		}
		if !errors.Is(err, want) || reader.closes != 1 {
			t.Fatalf("close result lost: %v, closes=%d", err, reader.closes)
		}
		if tracked.closes != 1 {
			t.Fatalf("failed backup left its session open: closes=%d", tracked.closes)
		}
		if _, err := os.Stat(filepath.Join(output, "index.json")); !os.IsNotExist(err) {
			t.Fatalf("invalid source committed: %v", err)
		}
	}
}
