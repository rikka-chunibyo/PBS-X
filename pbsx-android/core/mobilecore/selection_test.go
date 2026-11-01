package mobilecore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-plus/pxar/transfer"
	"github.com/pbs-plus/pxar/vfs"
)

func archiveFixture(t *testing.T, files map[string]string) *vfs.LocalFS {
	t.Helper()
	source := &fsDocuments{root: t.TempDir()}
	for name, data := range files {
		writeFixture(t, source, name, data)
	}
	if err := os.MkdirAll(source.full("Empty"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg, _ := buzhash.NewConfig(4096)
	dir := t.TempDir()
	store, err := backupproxy.NewLocalStore(dir, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = backupproxy.NewServer(backupproxy.NewLocalClient(&documentFS{ctx: context.Background(), docs: source}), store).RunBackupWithMode(context.Background(), ".", backupproxy.BackupConfig{BackupType: datastore.BackupHost, BackupID: "selection", BackupTime: 1700000000, ChunkConfig: cfg, DetectionMode: backupproxy.DetectionData, Compress: true})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := os.ReadFile(filepath.Join(dir, "root.mpxar.didx"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(dir, "root.ppxar.didx"))
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := datastore.NewChunkStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := transfer.NewSplitReader(meta, payload, datastore.NewChunkStoreSource(chunks))
	if err != nil {
		t.Fatal(err)
	}
	fs := vfs.NewLocalFS(reader).SetMaxCache(8)
	t.Cleanup(func() { fs.Close() })
	return fs
}
func writeFixture(t *testing.T, d *fsDocuments, name, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(d.full(name)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.full(name), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func assertContent(t *testing.T, d *fsDocuments, name, want string) {
	t.Helper()
	got, err := os.ReadFile(d.full(name))
	if err != nil || string(got) != want {
		t.Fatalf("%s: got %q (%v), want %q", name, got, err, want)
	}
}

func TestSelectedRestoreConflictModes(t *testing.T) {
	files := map[string]string{"Photos/a.txt": "backup-A", "Photos/same.txt": "same", "Photos/missing.txt": "new", "Private/unselected.txt": "not selected"}
	for _, mode := range []string{"skip", "replace", "rename"} {
		t.Run(mode, func(t *testing.T) {
			fs := archiveFixture(t, files)
			target := &fsDocuments{root: t.TempDir()}
			writeFixture(t, target, "Photos/a.txt", "old-data")
			writeFixture(t, target, "Photos/same.txt", "same")
			writeFixture(t, target, "Photos/local.txt", "keep local")
			writeFixture(t, target, "Photos/a (restored 1).txt", "older copy")
			old := time.Unix(1234567890, 0)
			if err := os.Chtimes(target.full("Photos/same.txt"), old, old); err != nil {
				t.Fatal(err)
			}
			stats, err := restoreSelected(context.Background(), fs, []string{"Photos"}, mode, target, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertContent(t, target, "Photos/missing.txt", "new")
			assertContent(t, target, "Photos/local.txt", "keep local")
			assertContent(t, target, "Photos/a (restored 1).txt", "older copy")
			info, err := os.Stat(target.full("Photos/same.txt"))
			if err != nil || !info.ModTime().Equal(old) {
				t.Fatal("identical/preexisting file was rewritten")
			}
			if _, err := os.Stat(target.full("Private")); !os.IsNotExist(err) {
				t.Fatal("unselected directory was restored")
			}
			switch mode {
			case "skip":
				assertContent(t, target, "Photos/a.txt", "old-data")
				if stats.Files != 1 || stats.Skipped != 2 {
					t.Fatalf("stats %+v", stats)
				}
			case "replace":
				assertContent(t, target, "Photos/a.txt", "backup-A")
				if stats.Files != 2 || stats.Skipped != 1 {
					t.Fatalf("stats %+v", stats)
				}
			case "rename":
				assertContent(t, target, "Photos/a.txt", "old-data")
				assertContent(t, target, "Photos/a (restored 2).txt", "backup-A")
				assertContent(t, target, "Photos/same (restored 1).txt", "same")
				if stats.Files != 3 || stats.Renamed != 2 {
					t.Fatalf("stats %+v", stats)
				}
			}
		})
	}
}
func TestSingleFileAndOverlappingSelections(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"Photos/猫.jpg": "image", "Photos/other.jpg": "other", "Docs/a.txt": "document"})
	target := &fsDocuments{root: t.TempDir()}
	paths, err := selections(`["Photos/猫.jpg","Photos/猫.jpg"]`)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := restoreSelected(context.Background(), fs, paths, "fail", target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 1 {
		t.Fatal(stats)
	}
	assertContent(t, target, "Photos/猫.jpg", "image")
	if _, err := os.Stat(target.full("Photos/other.jpg")); !os.IsNotExist(err) {
		t.Fatal("unselected sibling restored")
	}
	paths, err = selections(`["Photos/猫.jpg","Photos","Docs/a.txt","Photos/other.jpg"]`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"Docs/a.txt", "Photos"}) {
		t.Fatal(paths)
	}
	for _, bad := range []string{`[]`, `["../escape"]`, `["/absolute"]`, `["Photos/../Docs"]`, `null`} {
		if _, err := selections(bad); err == nil {
			t.Fatal("accepted unsafe/empty selection", bad)
		}
	}
}
func TestTypeConflictsNeverDeleteExistingTrees(t *testing.T) {
	for _, mode := range []string{"skip", "replace", "rename"} {
		t.Run(mode, func(t *testing.T) {
			fs := archiveFixture(t, map[string]string{"Photos/a.txt": "incoming"})
			target := &fsDocuments{root: t.TempDir()}
			writeFixture(t, target, "Photos/a.txt/local.txt", "keep")
			_, err := restoreSelected(context.Background(), fs, []string{"Photos/a.txt"}, mode, target, nil)
			if mode == "replace" && err == nil {
				t.Fatal("replace deleted a folder")
			}
			if mode != "replace" && err != nil {
				t.Fatal(err)
			}
			assertContent(t, target, "Photos/a.txt/local.txt", "keep")
			if mode == "rename" {
				assertContent(t, target, "Photos/a (restored 1).txt", "incoming")
			}
		})
	}
	fs := archiveFixture(t, map[string]string{"Photos/a.txt": "incoming"})
	target := &fsDocuments{root: t.TempDir()}
	writeFixture(t, target, "Photos", "existing file")
	if _, err := restoreSelected(context.Background(), fs, []string{"Photos/a.txt"}, "rename", target, nil); err != nil {
		t.Fatal(err)
	}
	assertContent(t, target, "Photos", "existing file")
	assertContent(t, target, "Photos (restored 1)/a.txt", "incoming")
}

type invalidInspect struct {
	Documents
	value string
}

func (d invalidInspect) InspectJSON(string) (string, error) { return d.value, nil }
func TestDestinationMetadataFailureCannotLookMissing(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"file.txt": "data"})
	for _, raw := range []string{`null`, `{}`, `{"exists":null}`, `{"exists":true}`} {
		target := &fsDocuments{root: t.TempDir()}
		if _, err := restoreSelected(context.Background(), fs, []string{"file.txt"}, "replace", invalidInspect{target, raw}, nil); err == nil {
			t.Fatal("accepted incomplete destination metadata", raw)
		}
		items, _ := os.ReadDir(target.root)
		if len(items) != 0 {
			t.Fatal("destination mutated before valid inspection")
		}
	}
}
func TestLargeDirectoryWorksWithSmallContentCache(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 40; i++ {
		files[fmt.Sprintf("Many/%02d.txt", i)] = fmt.Sprintf("content %d", i)
	}
	fs := archiveFixture(t, files)
	target := &fsDocuments{root: t.TempDir()}
	stats, err := restoreSelected(context.Background(), fs, []string{"Many"}, "fail", target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 40 {
		t.Fatal(stats)
	}
	for name, data := range files {
		assertContent(t, target, name, data)
	}
}
func TestArchiveBrowserPagesAndEmptyDirectories(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"a.txt": "A", "b.txt": "B", "Folder/猫.jpg": "image"})
	raw, err := listArchivePage(fs, ".", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Total   int           `json:"total"`
		Entries []archiveItem `json:"entries"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || len(page.Entries) != 2 || !page.Entries[0].Directory || !page.Entries[1].Directory {
		t.Fatal(raw)
	}
	raw, err = listArchivePage(fs, "Empty", 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(raw), []byte(`"entries":[]`)) {
		t.Fatal(raw)
	}
	target := &fsDocuments{root: t.TempDir()}
	if _, err := restoreSelected(context.Background(), fs, []string{"Empty"}, "skip", target, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target.full("Empty"))
	if err != nil || !info.IsDir() {
		t.Fatal("empty directory was not restored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := restoreSelected(ctx, fs, []string{"a.txt"}, "replace", target, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type unsupportedEntryFS struct{ vfs.FileSystem }

func (f unsupportedEntryFS) Lookup(p string) (*pxar.FileInfo, error) {
	if p == "/link" {
		return pxar.NewFileInfo("link", 0, os.ModeSymlink|0777, time.Now(), 0, 0), nil
	}
	return f.FileSystem.Lookup(p)
}
func TestUnsupportedSelectionFailsBeforeWriting(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"valid.txt": "value"})
	target := &fsDocuments{root: t.TempDir()}
	_, err := restoreSelected(context.Background(), unsupportedEntryFS{fs}, []string{"valid.txt", "link"}, "replace", target, nil)
	if err == nil {
		t.Fatal("special file accepted")
	}
	items, _ := os.ReadDir(target.root)
	if len(items) != 0 {
		t.Fatal("restore wrote files before validating every selection")
	}
}
