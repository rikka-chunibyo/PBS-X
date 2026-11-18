package backupproxy

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-plus/pxar/format"
)

func TestMetadataReuseUsesArchivePathsAndSkipsUnchangedReads(t *testing.T) {
	for _, root := range []string{".", "/root", "C:/Users/fixture"} {
		t.Run(root, func(t *testing.T) {
			mtime := format.StatxTimestamp{Secs: 1700000000}
			unchanged := bytes.Repeat([]byte("unchanged-payload"), 10000)
			initial := newMemFS()
			initial.addDirWithMtime(root, "", 0755, mtime)
			initial.addFileWithMtime(root+"/a.txt", root, unchanged, 0644, mtime)
			initial.addFileWithMtime(root+"/b.txt", root, []byte("old"), 0644, mtime)
			initial.addDirWithMtime(root+"/sub", root, 0755, mtime)
			initial.addFileWithMtime(root+"/sub/nested", root+"/sub", []byte("unchanged nested"), 0644, mtime)
			initial.addFileWithMtime(root+"/deleted", root, []byte("deleted"), 0644, mtime)
			previousServer, previous := newTestServer(t, initial)
			_, err := previousServer.RunSplitBackup(t.Context(), root, BackupConfig{BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000000})
			if err != nil {
				t.Fatal(err)
			}
			currentFS := newMemFS()
			currentFS.addDirWithMtime(root, "", 0755, mtime)
			currentFS.addFileWithMtime(root+"/a.txt", root, unchanged, 0644, mtime)
			currentFS.addFileWithMtime(root+"/b.txt", root, []byte("changed"), 0644, format.StatxTimestamp{Secs: mtime.Secs + 1})
			currentFS.addDirWithMtime(root+"/sub", root, 0755, mtime)
			currentFS.addFileWithMtime(root+"/sub/nested", root+"/sub", []byte("unchanged nested"), 0644, mtime)
			currentFS.addFileWithMtime(root+"/new", root, []byte("new"), 0644, mtime)
			server, current := newTestServer(t, currentFS)
			client := currentFS.provider()
			originalOpen := client.openFileFn
			reads := map[string]int{}
			client.openFileFn = func(ctx context.Context, path string) (io.ReadCloser, uint64, error) {
				reads[path]++
				if path == root+"/a.txt" || path == root+"/sub/nested" {
					t.Errorf("unchanged file was reopened: %s", path)
				}
				return originalOpen(ctx, path)
			}
			server.client = client
			_, err = server.RunMetadataBackup(t.Context(), root, BackupConfig{
				BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000001,
				PreviousBackup: &PreviousBackupRef{BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000000, Dir: previous},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reads, map[string]int{root + "/b.txt": 1, root + "/new": 1}) {
				t.Fatalf("source reads = %v", reads)
			}
			archive, _ := restoreSplitArchive(t, current)
			verifyFileContent(t, archive, "a.txt", string(unchanged))
			verifyFileContent(t, archive, "b.txt", "changed")
			verifyFileContent(t, archive, "sub/nested", "unchanged nested")
			verifyFileContent(t, archive, "new", "new")
			if entry, err := archive.Lookup("deleted"); err == nil && entry != nil {
				t.Fatal("deleted file was merged into the new snapshot")
			}
		})
	}
}

func TestMetadataReuseRejectsInvalidPreviousSnapshot(t *testing.T) {
	for _, damage := range []string{"missing manifest", "wrong identity", "corrupt index", "swapped index", "corrupt metadata chunk", "corrupt payload chunk"} {
		t.Run(damage, func(t *testing.T) {
			fs := newMemFS()
			mtime := format.StatxTimestamp{Secs: 1700000000}
			fs.addDirWithMtime(".", "", 0755, mtime)
			fs.addFileWithMtime("./a.txt", ".", []byte("alpha"), 0644, mtime)
			oldServer, previous := newTestServer(t, fs)
			result, err := oldServer.RunSplitBackup(t.Context(), ".", BackupConfig{BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000000})
			if err != nil {
				t.Fatal(err)
			}
			manifestFile := filepath.Join(previous, "index.json")
			switch damage {
			case "missing manifest":
				if err := os.Remove(manifestFile); err != nil {
					t.Fatal(err)
				}
			case "wrong identity":
				result.Manifest.BackupID = "other-host"
				data, err := result.Manifest.Marshal()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(manifestFile, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "corrupt index", "swapped index":
				name := filepath.Join(previous, "root.ppxar.didx")
				data, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				if damage == "corrupt index" {
					data[len(data)-1] ^= 1
				} else {
					index, err := datastore.ParseDynamicIndex(data)
					if err != nil {
						t.Fatal(err)
					}
					writer := datastore.NewDynamicIndexWriter(1700000000)
					for i := 0; i < index.Count(); i++ {
						entry := index.Entry(i)
						entry.Digest[0] ^= 1
						writer.Add(entry.EndOffset, entry.Digest)
					}
					index.Close()
					data, err = writer.Finish() // Valid index/header; differs from manifest.
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "corrupt metadata chunk", "corrupt payload chunk":
				name := "root.mpxar.didx"
				if damage == "corrupt payload chunk" {
					name = "root.ppxar.didx"
				}
				data, err := os.ReadFile(filepath.Join(previous, name))
				if err != nil {
					t.Fatal(err)
				}
				index, err := datastore.ParseDynamicIndex(data)
				if err != nil {
					t.Fatal(err)
				}
				digest := index.Entry(0).Digest
				index.Close()
				chunks, err := datastore.NewChunkStore(previous)
				if err != nil {
					t.Fatal(err)
				}
				blob, err := chunks.LoadChunk(digest)
				if err != nil {
					t.Fatal(err)
				}
				plain, err := datastore.DecodeBlob(nil, blob)
				if err != nil {
					t.Fatal(err)
				}
				plain[len(plain)-1] ^= 1
				blob, err = datastore.EncodeBlob(nil, plain) // Valid CRC, wrong SHA-256.
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(chunks.ChunkPath(digest), blob, 0600); err != nil {
					t.Fatal(err)
				}
			}
			server, current := newTestServer(t, fs)
			client := fs.provider()
			client.openFileFn = func(context.Context, string) (io.ReadCloser, uint64, error) {
				t.Error("source file opened during invalid previous-snapshot reuse")
				return nil, 0, os.ErrPermission
			}
			server.client = client
			_, err = server.RunMetadataBackup(t.Context(), ".", BackupConfig{
				BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000001,
				PreviousBackup: &PreviousBackupRef{BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000000, Dir: previous},
			})
			if err == nil {
				t.Fatal("invalid previous snapshot reused")
			}
			if strings.HasPrefix(damage, "corrupt metadata") || strings.HasPrefix(damage, "corrupt payload") {
				if !strings.Contains(err.Error(), "digest mismatch") {
					t.Fatalf("missing chunk verification error: %v", err)
				}
			}
			if _, err := os.Stat(filepath.Join(current, "index.json")); !os.IsNotExist(err) {
				t.Fatalf("failed reuse committed a snapshot: %v", err)
			}
		})
	}
}

func TestMetadataReuseDetectsSubsecondEdit(t *testing.T) {
	makeFS := func(data string, nanos uint32) memFS {
		fs := newMemFS()
		mtime := format.StatxTimestamp{Secs: 1700000000, Nanos: nanos}
		fs.addDirWithMtime(".", "", 0755, mtime)
		fs.addFileWithMtime("./file", ".", []byte(data), 0644, mtime)
		return fs
	}
	initial, previous := newTestServer(t, makeFS("AAAA", 100000000))
	if _, err := initial.RunSplitBackup(t.Context(), ".", BackupConfig{BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000010}); err != nil {
		t.Fatal(err)
	}
	currentFS := makeFS("BBBB", 900000000)
	server, current := newTestServer(t, currentFS)
	provider := currentFS.provider()
	open := provider.openFileFn
	reads := 0
	provider.openFileFn = func(ctx context.Context, path string) (io.ReadCloser, uint64, error) { reads++; return open(ctx, path) }
	server.client = provider
	if _, err := server.RunMetadataBackup(t.Context(), ".", BackupConfig{
		BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000011,
		PreviousBackup: &PreviousBackupRef{BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1700000010, Dir: previous},
	}); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("subsecond edit reused stale payload; source reads=%d", reads)
	}
	archive, _ := restoreSplitArchive(t, current)
	verifyFileContent(t, archive, "file", "BBBB")
}
