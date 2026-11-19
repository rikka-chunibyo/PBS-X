//go:build linux || android

package mobilecore

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLocalStage(t *testing.T, docs *LocalRestore, name, content string) {
	t.Helper()
	fd, err := docs.OpenWrite(name)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), name)
	if _, err = file.WriteString(content); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalRestoreCommitPreservesConflictsAndDoesNotTruncateOriginal(t *testing.T) {
	root := t.TempDir()
	docs, err := NewLocalRestore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer docs.Close()
	if err = os.WriteFile(filepath.Join(root, "target"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	writeLocalStage(t, docs, ".pbsx-stage-test", "incoming")
	if _, err = docs.OpenWrite("target"); err == nil {
		t.Fatal("allowed direct writes to final destination")
	}
	if _, err = docs.OpenWrite(".pbsx-stage-test"); err == nil {
		t.Fatal("truncated an existing stage")
	}
	if err = docs.CommitFile(".pbsx-stage-test", "target", false); err == nil {
		t.Fatal("overwrote a no-replace conflict")
	}
	old, _ := os.ReadFile(filepath.Join(root, "target"))
	if string(old) != "original" {
		t.Fatal("original file changed before successful commit")
	}
	if err = docs.CommitFile(".pbsx-stage-test", "target", true); err != nil {
		t.Fatal(err)
	}
	value, _ := os.ReadFile(filepath.Join(root, "target"))
	if string(value) != "incoming" {
		t.Fatal("replacement was incomplete")
	}
	if err = docs.DeleteFile("target"); err == nil {
		t.Fatal("allowed deleting a final user file")
	}
	writeLocalStage(t, docs, ".pbsx-stage-new", "new")
	if err = docs.CommitFile(".pbsx-stage-new", "new", false); err != nil {
		t.Fatal(err)
	}
}

func TestLocalRestoreRejectsTraversalAndSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "keep"), filepath.Join(root, "target")); err != nil {
		t.Fatal(err)
	}
	docs, err := NewLocalRestore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer docs.Close()
	for _, p := range []string{"../.pbsx-stage-outside", "link/.pbsx-stage-outside", "/tmp/.pbsx-stage-outside"} {
		if _, err := docs.OpenWrite(p); err == nil {
			t.Fatal("unsafe staging path accepted: " + p)
		}
	}
	if _, err := docs.InspectJSON("link/keep"); err == nil {
		t.Fatal("followed an intermediate symlink")
	}
	if _, err := docs.InspectJSON("target"); err == nil {
		t.Fatal("followed a final symlink")
	}
	writeLocalStage(t, docs, ".pbsx-stage-test", "new")
	if err = docs.CommitFile(".pbsx-stage-test", "target", true); err == nil {
		t.Fatal("replaced a symlink target")
	}
	value, _ := os.ReadFile(filepath.Join(outside, "keep"))
	if string(value) != "untouched" {
		t.Fatal("modified a file outside the restore root")
	}
	if _, err := NewLocalRestore(filepath.Join(root, "link")); err == nil {
		t.Fatal("allowed a symlink restore root")
	}
}

func TestLocalRestoreUsesExistingSelectiveRestoreModes(t *testing.T) {
	for _, mode := range []string{"skip", "replace", "rename"} {
		t.Run(mode, func(t *testing.T) {
			fs := archiveFixture(t, map[string]string{"Photos/a.txt": "backup bytes", "Photos/new.txt": "new file"})
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "Photos"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "Photos/a.txt"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			docs, err := NewLocalRestore(root)
			if err != nil {
				t.Fatal(err)
			}
			defer docs.Close()
			stats, err := restoreSelectedReporting(t.Context(), fs, []string{"Photos"}, mode, docs, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			old, _ := os.ReadFile(filepath.Join(root, "Photos/a.txt"))
			want := "original"
			if mode == "replace" {
				want = "backup bytes"
			}
			if string(old) != want {
				t.Fatalf("mode %s: original target=%q", mode, old)
			}
			added, _ := os.ReadFile(filepath.Join(root, "Photos/new.txt"))
			if string(added) != "new file" {
				t.Fatal("missing file was not restored")
			}
			if mode == "rename" && stats.Renamed != 1 {
				t.Fatal("conflict was not renamed")
			}
		})
	}
}

func TestLocalRestoreFreshDirectoryIsExclusiveAndIndependentlyOwned(t *testing.T) {
	root := t.TempDir()
	docs, err := NewLocalRestore(root)
	if err != nil {
		t.Fatal(err)
	}
	child, err := docs.NewDirectory("fresh")
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if _, err := docs.NewDirectory("fresh"); err == nil {
		t.Fatal("reused existing restore directory")
	}
	if _, err := docs.NewDirectory("../escape"); err == nil {
		t.Fatal("accepted parent traversal")
	}
	if err := docs.Close(); err != nil {
		t.Fatal(err)
	}
	writeLocalStage(t, child, ".pbsx-stage-file", "new bytes")
	if err := child.CommitFile(".pbsx-stage-file", "file", false); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(filepath.Join(root, "fresh/file"))
	if err != nil || string(value) != "new bytes" {
		t.Fatalf("fresh restore lost ownership: %q, %v", value, err)
	}
}
