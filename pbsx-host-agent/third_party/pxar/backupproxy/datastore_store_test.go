package backupproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
)

func TestDatastoreStoreDoesNotCountExistingRawChunkAsUploaded(t *testing.T) {
	root := t.TempDir()
	chunkStore, err := datastore.NewChunkStore(root)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("existing raw payload chunk")
	digest := sha256.Sum256(payload)
	blob, err := datastore.EncodeBlob(nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := chunkStore.InsertChunk(digest, blob); err != nil {
		t.Fatal(err)
	}

	// This API takes an explicit path; its fixture need not use PBS's POSIX-only
	// timestamp directory name. Keep the session checks runnable on Windows.
	snapshotDir := filepath.Join(root, "host", "target", "snapshot-fixture")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := buzhash.NewConfig(4 << 20)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewDatastoreStore(root, snapshotDir, cfg, DatastoreStoreOptions{UID: -1, GID: -1, SyncWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	var progress UploadProgress
	session, err := store.StartSession(context.Background(), BackupConfig{
		BackupType: datastore.BackupHost,
		BackupID:   "target",
		BackupTime: 1787932800,
		CryptMode:  datastore.CryptModeNone,
		OnUploadProgress: func(current UploadProgress) {
			progress = current
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	injections := make(chan InjectChunks)
	close(injections)
	suggestions := make(chan uint64)
	close(suggestions)
	if _, err := session.UploadPayloadInterleaved(context.Background(), "existing.ppxar.didx", bytes.NewReader(payload), injections, suggestions); err != nil {
		t.Fatal(err)
	}
	if progress.ProcessedChunks != 1 || progress.ProcessedBytes != uint64(len(payload)) || progress.UploadedChunks != 0 || progress.UploadedBytes != 0 {
		t.Fatalf("unexpected existing raw chunk progress: %+v", progress)
	}
}

func TestDatastoreStorePublishesManifestLastWithoutLoadingReusedChunks(t *testing.T) {
	t.Run("default", func(t *testing.T) { testDatastoreManifestPublication(t, false) })
	t.Run("durable", func(t *testing.T) { testDatastoreManifestPublication(t, true) })
}

func testDatastoreManifestPublication(t *testing.T, syncWrites bool) {
	t.Helper()
	root := t.TempDir()
	chunkStore, err := datastore.NewChunkStore(root)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("existing payload chunk")
	digest := sha256.Sum256(payload)
	blob, err := datastore.EncodeBlob(nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := chunkStore.InsertChunk(digest, blob); err != nil {
		t.Fatal(err)
	}

	snapshotDir := filepath.Join(root, "host", "target", "snapshot-fixture")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := buzhash.NewConfig(4 << 20)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewDatastoreStore(root, snapshotDir, cfg, DatastoreStoreOptions{UID: -1, GID: -1, SyncWrites: syncWrites})
	if err != nil {
		t.Fatal(err)
	}
	var progress UploadProgress
	session, err := store.StartSession(context.Background(), BackupConfig{
		BackupType: datastore.BackupHost,
		BackupID:   "target",
		BackupTime: 1787936400,
		CryptMode:  datastore.CryptModeNone,
		OnUploadProgress: func(current UploadProgress) {
			progress = current
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	loaded := false
	injections := make(chan InjectChunks, 1)
	injections <- InjectChunks{Chunks: []KnownChunkRef{{
		Digest: digest,
		Size:   uint64(len(payload)),
		LoadEncodedBlob: func() ([]byte, error) {
			loaded = true
			return nil, os.ErrInvalid
		},
	}}, Size: uint64(len(payload))}
	close(injections)
	if _, err := session.UploadPayloadInterleaved(context.Background(), "target.ppxar.didx", bytes.NewReader(nil), injections, nil); err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.Fatal("reused payload chunk was loaded")
	}
	if progress.ProcessedChunks != 1 || progress.ProcessedBytes != uint64(len(payload)) || progress.UploadedChunks != 0 || progress.UploadedBytes != 0 {
		t.Fatalf("unexpected reused chunk progress: %+v", progress)
	}
	_, err = session.UploadArchive(context.Background(), "target.mpxar.didx", bytes.NewReader([]byte("metadata")))
	if runtime.GOOS == "windows" && syncWrites && errors.Is(err, os.ErrPermission) && strings.Contains(err.Error(), "sync chunk dir") {
		// Windows can reject directory fsync. Required durability must fail
		// before publishing a completed manifest, rather than claim success.
		if _, statErr := os.Stat(filepath.Join(snapshotDir, "index.json.blob")); !os.IsNotExist(statErr) {
			t.Fatal("durability failure published a completed manifest")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, "index.json.blob")); !os.IsNotExist(err) {
		t.Fatal("manifest became visible before session finish")
	}
	manifest, err := session.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("manifest files = %d, want 2", len(manifest.Files))
	}

	manifestBlob, err := os.ReadFile(filepath.Join(snapshotDir, "index.json.blob"))
	if err != nil {
		t.Fatal(err)
	}
	manifestJSON, err := datastore.DecodeBlob(nil, manifestBlob)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := datastore.UnmarshalManifest(manifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.BackupID != "target" || parsed.BackupType != "host" || parsed.CryptMode != "" {
		t.Fatalf("unexpected manifest identity: %+v", parsed)
	}

	idx, err := datastore.OpenDynamicIndex(filepath.Join(snapshotDir, "target.ppxar.didx"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idx.Close() }()
	entry := idx.Entry(0)
	if idx.Count() != 1 || entry.Digest != digest || entry.EndOffset != uint64(len(payload)) {
		t.Fatalf("unexpected reused index entry: %+v", entry)
	}
}

func TestDatastoreStoreRejectsSnapshotOutsideDatastore(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".chunks"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := buzhash.NewConfig(4 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDatastoreStore(root, t.TempDir(), cfg, DatastoreStoreOptions{}); err == nil {
		t.Fatal("expected outside snapshot path to be rejected")
	}
}

func TestDatastoreStoreRejectsMissingReusedChunk(t *testing.T) {
	root := t.TempDir()
	if _, err := datastore.NewChunkStore(root); err != nil {
		t.Fatal(err)
	}
	snapshotDir := filepath.Join(root, "host", "target", "snapshot-fixture")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := buzhash.NewConfig(4 << 20)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewDatastoreStore(root, snapshotDir, cfg, DatastoreStoreOptions{UID: -1, GID: -1})
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.StartSession(context.Background(), BackupConfig{
		BackupType: datastore.BackupHost,
		BackupID:   "target",
		BackupTime: 1787936400,
		CryptMode:  datastore.CryptModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing [32]byte
	copy(missing[:], []byte("chunk that was never stored........"))
	injections := make(chan InjectChunks, 1)
	injections <- InjectChunks{Chunks: []KnownChunkRef{{Digest: missing, Size: 4}}}
	close(injections)
	if _, err := session.UploadPayloadInterleaved(context.Background(), "target.ppxar.didx", bytes.NewReader(nil), injections, nil); err == nil {
		t.Fatal("expected missing reused chunk to be rejected")
	}
}
