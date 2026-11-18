//go:build linux

package pxar

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-plus/pxar/format"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
	"github.com/pbs-x/pbsx-host-agent/internal/testutil/pbsfixture"
)

type changingSparseSource struct {
	*agentfs.NativeAccessor
	source  string
	changed bool
}

func (s *changingSparseSource) GetXAttrs(p string) ([]format.XAttr, error) {
	attrs, err := s.NativeAccessor.GetXAttrs(p)
	if err != nil {
		return nil, err
	}
	for _, attr := range attrs {
		if string(attr.Name()) == XAttrSparseManifest && !s.changed {
			// Deterministic scheduling of a legitimate concurrent application
			// write after the hole scan and before the content is read.
			file, err := os.OpenFile(s.source, os.O_RDWR, 0)
			if err != nil {
				return nil, err
			}
			_, err = file.WriteAt([]byte("RECENT-DATA"), 4096)
			closeErr := file.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
			s.changed = true
		}
	}
	return attrs, nil
}

func TestSparseRestorePreservesPayloadAfterSourceHoleChanges(t *testing.T) {
	source := t.TempDir()
	fileName := filepath.Join(source, "file.txt")
	file, err := os.Create(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(1 << 20); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	native := agentfs.NewNativeAccessor("review", "standard", snapshots.Snapshot{Path: source, SourcePath: source, Direct: true})
	defer native.Close()
	fs := &changingSparseSource{NativeAccessor: native, source: fileName}
	storeDir := t.TempDir()
	chunkConfig, err := buzhash.NewConfig(4096)
	if err != nil {
		t.Fatal(err)
	}
	store, err := backupproxy.NewLocalStore(storeDir, chunkConfig, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := backupproxy.NewServer(backupproxy.NewLocalClient(fs), store).RunSplitBackup(t.Context(), ".", backupproxy.BackupConfig{
		BackupType: datastore.BackupHost, BackupID: pbsfixture.HostID, BackupTime: pbsfixture.Stamp, DetectionMode: backupproxy.DetectionData, StrictMetadata: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fs.changed {
		t.Skip("fixture filesystem does not report sparse holes")
	}
	current, err := os.ReadFile(fileName)
	if err != nil || string(current[4096:4107]) != "RECENT-DATA" {
		t.Fatalf("source write missing: %v", err)
	}
	snapshot := pbsfixture.New(t, true)
	snapshot.Manifest = *result.Manifest
	chunkStore, err := datastore.NewChunkStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"root.mpxar.didx", "root.ppxar.didx"} {
		raw, err := os.ReadFile(filepath.Join(storeDir, name))
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Files[name] = raw
		index, err := datastore.ParseDynamicIndex(raw)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < index.Count(); i++ {
			digest := index.Entry(i).Digest
			blob, err := chunkStore.LoadChunk(digest)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.Chunks[digest] = blob
		}
		index.Close()
	}
	snapshot.WriteManifest(t)
	cfg := snapshot.Start(t)
	for _, mode := range []RestoreMode{RestoreModeNoAttr, RestoreModeNormal} {
		reader, err := NewPBSArchiveReader(t.Context(), cfg, pbsfixture.HostID, pbsfixture.Stamp)
		if err != nil {
			t.Fatal(err)
		}
		client, reported := NewLocalClient(reader, fmt.Sprint(mode))
		dest := t.TempDir()
		if err := RestoreWithOptions(t.Context(), client, []string{"/"}, RestoreOptions{Mode: mode, DestDir: dest}); err != nil {
			client.Close()
			t.Fatal(err)
		}
		select {
		case err := <-reported:
			client.Close()
			t.Fatalf("restore reported failure: %v", err)
		default:
		}
		client.Close()
		got, err := os.ReadFile(filepath.Join(dest, "file.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, current) {
			t.Fatalf("restore mode %v changed verified payload: %q", mode, got[4096:4107])
		}
	}
}
