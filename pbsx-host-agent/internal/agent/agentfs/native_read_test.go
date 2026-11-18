package agentfs

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs/fswire"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
)

func TestCheckedNativeReadRejectsSourceChanges(t *testing.T) {
	for _, change := range []string{"rewrite", "grow", "shrink", "after-read"} {
		t.Run(change, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "source")
			data := bytes.Repeat([]byte("A"), 8192)
			if err := os.WriteFile(name, data, 0600); err != nil {
				t.Fatal(err)
			}
			old := time.Unix(1700000000, 100000000)
			if err := os.Chtimes(name, old, old); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			reader, err := newCheckedNativeFile(file)
			if err != nil {
				file.Close()
				t.Fatal(err)
			}
			defer reader.Close()
			if _, optimized := any(reader).(io.WriterTo); optimized {
				t.Fatal("optimized copy can bypass read validation")
			}
			if change == "after-read" {
				if _, err := io.ReadAll(reader); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := io.ReadFull(reader, make([]byte, 100)); err != nil {
					t.Fatal(err)
				}
			}
			updated := bytes.Repeat([]byte("B"), len(data))
			if change == "grow" {
				updated = append(updated, 'B')
			}
			if change == "shrink" {
				updated = updated[:101]
			}
			if err := os.WriteFile(name, updated, 0600); err != nil {
				t.Fatal(err)
			}
			now := old.Add(time.Second)
			if err := os.Chtimes(name, now, now); err != nil {
				t.Fatal(err)
			}
			if change != "after-read" {
				if _, err := io.ReadAll(reader); err == nil {
					t.Fatal("changed source read accepted")
				}
			}
			if err := reader.Close(); err == nil || !strings.Contains(err.Error(), "source file") {
				t.Fatalf("changed source close accepted: %v", err)
			}
		})
	}
}

func TestCheckedNativeReadAcceptsStableAndEmptyFiles(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("stable data")} {
		name := filepath.Join(t.TempDir(), "source")
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := newCheckedNativeFile(file)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		got, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("stable source changed: %q %v", got, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckedNativeReadRejectsWriteWithRestoredMtime(t *testing.T) {
	name := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(name, []byte("AAAA"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 123456700)
	if err := os.Chtimes(name, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := newCheckedNativeFile(file)
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := io.ReadFull(reader, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("BBBB"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("restoring mtime hid the source change")
	}
}

func TestNativeFormatStatPrefersPreciseTimestampHint(t *testing.T) {
	first := time.Unix(1700000000, 100000000)
	second := time.Unix(1700000000, 900000000)
	legacy := fswire.AgentFileInfo{LastWriteTime: 1700000000}
	info := fswire.AgentFileInfo{Mode: 0644, ModTime: 1700000000, NativeModTime: &first}
	before := nativeFormatStat("unused", info, legacy)
	info.NativeModTime = &second
	after := nativeFormatStat("unused", info, legacy)
	if before.Mtime == after.Mtime || !after.Mtime.Time().Equal(second) {
		t.Fatalf("lost native precision: %+v %+v", before.Mtime, after.Mtime)
	}
}

func TestNativePrecisionHintKeepsLegacyWireEncoding(t *testing.T) {
	precise := time.Unix(1700000000, 900000000)
	info := fswire.AgentFileInfo{ModTime: 1700000000, NativeModTime: &precise}
	got, err := cbor.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	info.NativeModTime = nil
	want, err := cbor.Marshal(info)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("precision hint changed the wire payload: %x != %x: %v", got, want, err)
	}
}

type changingReadAccessor struct {
	*NativeAccessor
	name    string
	changed bool
}
type changingReadStream struct {
	io.ReadCloser
	owner *changingReadAccessor
}

func (r *changingReadStream) Read(p []byte) (int, error) {
	if !r.owner.changed {
		p = p[:min(len(p), 1024)]
	}
	n, err := r.ReadCloser.Read(p)
	if n > 0 && !r.owner.changed {
		if err := os.WriteFile(r.owner.name, bytes.Repeat([]byte("B"), 8192), 0600); err != nil {
			return 0, err
		}
		now := time.Unix(1700000100, 0)
		if err := os.Chtimes(r.owner.name, now, now); err != nil {
			return 0, err
		}
		r.owner.changed = true
	}
	return n, err
}
func (a *changingReadAccessor) OpenFile(path string) (io.ReadCloser, uint64, error) {
	reader, size, err := a.NativeAccessor.OpenFile(path)
	if err != nil {
		return nil, 0, err
	}
	return &changingReadStream{ReadCloser: reader, owner: a}, size, nil
}

func TestNativeBackupDoesNotCommitMixedFileVersions(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "file")
	if err := os.WriteFile(name, bytes.Repeat([]byte("A"), 8192), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1700000000, 0)
	if err := os.Chtimes(name, old, old); err != nil {
		t.Fatal(err)
	}
	native := NewNativeAccessor("changing-source", "standard", snapshots.Snapshot{Path: root, SourcePath: root, Direct: true})
	defer native.Close()
	fs := &changingReadAccessor{NativeAccessor: native, name: name}
	output := t.TempDir()
	config, err := buzhash.NewConfig(4096)
	if err != nil {
		t.Fatal(err)
	}
	store, err := backupproxy.NewLocalStore(output, config, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := backupproxy.NewServer(backupproxy.NewLocalClient(fs), store).RunSplitBackup(t.Context(), ".", backupproxy.BackupConfig{BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000000, StrictMetadata: true})
	if err == nil || result != nil || !fs.changed {
		t.Fatalf("mixed file committed: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(output, "index.json")); !os.IsNotExist(err) {
		t.Fatalf("failed source produced a manifest: %v", err)
	}
}

func TestNativeWindowsTimestampSupportsFullDateRange(t *testing.T) {
	for _, year := range []int{1601, 1970, 2026, 2500} {
		want := time.Date(year, 1, 1, 0, 0, 0, 123456700, time.UTC)
		ticks := (want.Unix()+11_644_473_600)*10_000_000 + int64(want.Nanosecond()/100)
		if got := nativeWindowsModTime(ticks); !got.Equal(want) {
			t.Fatalf("Windows timestamp = %s, want %s", got, want)
		}
	}
}
