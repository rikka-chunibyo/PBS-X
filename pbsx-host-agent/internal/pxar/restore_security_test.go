package pxar

import (
	"bytes"
	"context"
	"fmt"
	pxar "github.com/pbs-plus/pxar"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Real restore entry point, with a deterministic in-memory archive transport.
type restoreFixture struct {
	payload  []byte
	entries  []pxar.FileInfo
	short    bool
	rootName string
}

func (f *restoreFixture) LookupByPath(context.Context, string) (*pxar.FileInfo, error) {
	name := f.rootName
	if name == "" {
		name = "folder"
	}
	return &pxar.FileInfo{FileName: []byte(name), FileType: pxar.FileTypeDirectory, RawMode: 0755, RawUID: uint32(os.Getuid()), RawGID: uint32(os.Getgid()), MtimeSecs: 1}, nil
}
func TestRestoreArchiveRootMarker(t *testing.T) {
	fixture := fileFixture("RIGHT")
	fixture.rootName = string(filepath.Separator)
	dest := t.TempDir()
	if err := runFixtureRestore(t.Context(), fixture, dest, RestoreModeNoAttr); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "file"))
	if err != nil || string(data) != "RIGHT" {
		t.Fatalf("root restore %q: %v", data, err)
	}
}
func (f *restoreFixture) ReadDir(context.Context, uint64) ([]pxar.FileInfo, error) {
	return f.entries, nil
}
func (f *restoreFixture) ReadLink(context.Context, uint64, uint64) ([]byte, error) {
	return []byte("target"), nil
}
func (f *restoreFixture) ListXAttrs(context.Context, uint64, uint64) (map[string][]byte, error) {
	return map[string][]byte{}, nil
}
func (f *restoreFixture) ReadFileContentReader(context.Context, uint64, uint64) (io.ReadCloser, error) {
	data := f.payload
	if f.short && len(data) > 0 {
		data = data[:len(data)-1]
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (f *restoreFixture) Close() error { return nil }
func fileFixture(data string) *restoreFixture {
	return &restoreFixture{payload: []byte(data), entries: []pxar.FileInfo{{FileName: []byte("file"), FileType: pxar.FileTypeFile, RawMode: 0644, RawUID: uint32(os.Getuid()), RawGID: uint32(os.Getgid()), RawSize: uint64(len(data)), ContentRange: []uint64{0, uint64(len(data))}, MtimeSecs: 1}}}
}
func runFixtureRestore(ctx context.Context, f *restoreFixture, dest string, mode RestoreMode) error {
	client, errs := NewLocalClient(f, "fixture")
	defer client.Close()
	err := RestoreWithOptions(ctx, client, []string{"/"}, RestoreOptions{Mode: mode, DestDir: dest})
	if err != nil {
		return err
	}
	select {
	case err = <-errs:
		return err
	default:
		return nil
	}
}
func TestRestoreReplacesDifferentSameSizeContents(t *testing.T) {
	for _, mode := range []RestoreMode{RestoreModeNoAttr, RestoreModeNormal} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			dest := t.TempDir()
			p := filepath.Join(dest, "folder", "file")
			os.Mkdir(filepath.Dir(p), 0755)
			os.WriteFile(p, []byte("WRONG"), 0644)
			os.Chtimes(p, time.Unix(1, 0), time.Unix(1, 0))
			if err := runFixtureRestore(t.Context(), fileFixture("RIGHT"), dest, mode); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(p)
			if err != nil || string(data) != "RIGHT" {
				t.Fatalf("restore left %q: %v", data, err)
			}
		})
	}
}
func TestRestorePreservesUnrelatedTempNamedFiles(t *testing.T) {
	dest := t.TempDir()
	for _, name := range []string{".pxar-restore-user-notes", ".pxar-restore-active-other-run"} {
		os.WriteFile(filepath.Join(dest, name), []byte("keep"), 0600)
	}
	if err := runFixtureRestore(t.Context(), fileFixture("new"), dest, RestoreModeNoAttr); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".pxar-restore-user-notes", ".pxar-restore-active-other-run"} {
		data, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(data) != "keep" {
			t.Fatalf("unrelated file lost: %s %v", name, err)
		}
	}
}
func TestRestoreRejectsDirectorySymlinkEscape(t *testing.T) {
	dest, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "folder")); err != nil {
		t.Skipf("symlink privilege required: %v", err)
	}
	if err := runFixtureRestore(t.Context(), fileFixture("new"), dest, RestoreModeNoAttr); err == nil {
		t.Fatal("outside symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(err) {
		t.Fatal("outside destination modified", err)
	}
}
func TestRestoreRejectsRacingDirectorySymlink(t *testing.T) {
	dest, outside := t.TempDir(), t.TempDir()
	fs, err := openRestoreFS(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.root.Close()
	link := filepath.Join(dest, "racing")
	probe := filepath.Join(dest, "probe")
	if err = os.Symlink(outside, probe); err != nil {
		t.Skipf("symlink privilege required: %v", err)
	}
	os.Remove(probe)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			os.Symlink(outside, link)
			os.Remove(link)
		}
	}()
	for i := 0; i < 200; i++ {
		file, err := fs.open(filepath.Join(link, "file"), os.O_CREATE|os.O_WRONLY, 0600)
		if err == nil {
			file.Write([]byte("bad"))
			file.Close()
		}
	}
	close(stop)
	wg.Wait()
	if _, err = os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(err) {
		t.Fatal("racing symlink escaped", err)
	}
}
func TestRestoreRejectsShortPayloadWithoutReplacingFile(t *testing.T) {
	dest := t.TempDir()
	p := filepath.Join(dest, "folder", "file")
	os.Mkdir(filepath.Dir(p), 0755)
	os.WriteFile(p, []byte("keep original"), 0644)
	fixture := fileFixture("RIGHT")
	fixture.short = true
	if err := runFixtureRestore(t.Context(), fixture, dest, RestoreModeNoAttr); err == nil {
		t.Fatal("short payload accepted")
	}
	data, _ := os.ReadFile(p)
	if string(data) != "keep original" {
		t.Fatal("existing file damaged")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatal("failed run leaked temporary file")
	}
}

func TestRestoreReplacesFileSymlinkWithoutTouchingTarget(t *testing.T) {
	dest, outside := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(dest, "folder"), 0755)
	external := filepath.Join(outside, "file")
	os.WriteFile(external, []byte("outside"), 0644)
	if err := os.Symlink(external, filepath.Join(dest, "folder", "file")); err != nil {
		t.Skipf("symlink privilege required: %v", err)
	}
	if err := runFixtureRestore(t.Context(), fileFixture("RIGHT"), dest, RestoreModeNoAttr); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(external)
	if string(data) != "outside" {
		t.Fatal("symlink target modified")
	}
	data, _ = os.ReadFile(filepath.Join(dest, "folder", "file"))
	if string(data) != "RIGHT" {
		t.Fatal("restored content missing")
	}
}
