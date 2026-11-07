//go:build windows

package pxar

import (
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRestoreWindowsRootDirectoryMetadata(t *testing.T) {
	dest := t.TempDir()
	fs, err := openRestoreFS(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.root.Close()
	file, err := openWindowsRestoreFile(&restoreState{target: fs}, dest, "", windows.FILE_WRITE_ATTRIBUTES, false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		t.Fatalf("root metadata handle: %v %v", info, err)
	}
}

func TestRestoreRejectsJunctionEscape(t *testing.T) {
	dest, outside := t.TempDir(), t.TempDir()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(dest, "folder"), outside).CombinedOutput(); err != nil {
		t.Fatalf("create test junction: %v %s", err, out)
	}
	if err := runFixtureRestore(t.Context(), fileFixture("bad"), dest, RestoreModeNormal); err == nil {
		t.Fatal("junction escape accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(err) {
		t.Fatal("junction target modified", err)
	}
}
func TestRestoreMetadataUsesCapturedWindowsRoot(t *testing.T) {
	outer := t.TempDir()
	dest := filepath.Join(outer, "destination")
	moved := filepath.Join(outer, "moved")
	os.Mkdir(dest, 0755)
	p := filepath.Join(dest, "file")
	os.WriteFile(p, []byte("original"), 0644)
	fs, err := openRestoreFS(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.root.Close()
	if err = os.Rename(dest, moved); err != nil {
		// Windows may pin the Root against rename entirely. That is also
		// confinement: verify metadata still reaches the selected root.
		file, openErr := openWindowsRestoreFile(&restoreState{target: fs}, p, "", windows.GENERIC_WRITE, false, false)
		if openErr != nil {
			t.Fatal(openErr)
		}
		if openErr = file.Truncate(0); openErr == nil {
			_, openErr = file.Write([]byte("updated"))
		}
		file.Close()
		if openErr != nil {
			t.Fatal(openErr)
		}
		data, _ := os.ReadFile(p)
		if string(data) != "updated" {
			t.Fatal("pinned-root metadata failed")
		}
		t.Log("Windows Root prevents replacement while its handle is open")
		return
	}
	os.Mkdir(dest, 0755)
	os.WriteFile(p, []byte("replacement must survive"), 0644)
	file, err := openWindowsRestoreFile(&restoreState{target: fs}, p, "", windows.GENERIC_WRITE, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(0); err != nil {
		file.Close()
		t.Fatal(err)
	}
	_, err = file.Write([]byte("updated"))
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "replacement must survive" {
		t.Fatal("metadata opened a replacement root")
	}
	data, _ = os.ReadFile(filepath.Join(moved, "file"))
	if string(data) != "updated" {
		t.Fatal("captured root was not used")
	}
}

func TestRestoreWindowsMetadataAndADSRejectJunction(t *testing.T) {
	dest, outside := t.TempDir(), t.TempDir()
	outsideFile := filepath.Join(outside, "file")
	os.WriteFile(outsideFile, []byte("outside"), 0644)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(dest, "folder"), outside).CombinedOutput(); err != nil {
		t.Fatalf("create test junction: %v %s", err, out)
	}
	fs, err := openRestoreFS(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.root.Close()
	st := &restoreState{target: fs}
	p := filepath.Join(dest, "folder", "file")
	file, err := openWindowsRestoreFile(st, p, "", windows.FILE_WRITE_ATTRIBUTES, false, false)
	if err == nil {
		file.Close()
		t.Fatal("metadata escaped through junction")
	}
	if err = writeRestoreADS(st, p, "audit", []byte("bad")); err == nil {
		t.Fatal("ADS escaped through junction")
	}
	if _, err = os.Stat(outsideFile + ":audit"); !os.IsNotExist(err) {
		t.Fatal("outside ADS created", err)
	}
}
