package pxar

import (
	"os"
	"path/filepath"
	"testing"

	pxarlib "github.com/pbs-plus/pxar"
)

func TestHardlinkFailurePreservesDestination(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		name := "immediate"
		if deferred {
			name = "deferred"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "existing.txt")
			if err := os.WriteFile(dest, []byte("LOCAL-ORIGINAL"), 0600); err != nil {
				t.Fatal(err)
			}
			root, err := openRestoreFS(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.root.Close()
			links := newHardlinkIndex()
			// A previously restored canonical file can disappear before a link
			// is processed. The archive alias has no independent content.
			links.register("canonical.txt", filepath.Join(dir, "missing.txt"))
			state := &restoreState{target: root, hl: links}
			job := restoreJob{dest: dest, srcPath: "alias.txt", info: pxarlib.FileInfo{FileType: pxarlib.FileTypeHardlink, LinkTarget: "canonical.txt"}}
			if deferred {
				links.deferLink(job)
				err = resolveDeferredHardlinks(t.Context(), state)
			} else {
				err = processJob(t.Context(), state, job)
			}
			if err == nil {
				t.Fatal("missing hardlink target was accepted")
			}
			got, err := os.ReadFile(dest)
			if err != nil || string(got) != "LOCAL-ORIGINAL" {
				t.Fatalf("original destination lost: %q %v", got, err)
			}
			if _, ok := links.resolved["alias.txt"]; ok {
				t.Fatal("failed hardlink registered as successfully restored")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("temporary hardlink leaked: %v %v", entries, err)
			}
		})
	}
}

func TestHardlinkReplacementAndRepeatedRestore(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	dest := filepath.Join(dir, "dest")
	if err := os.WriteFile(source, []byte("ARCHIVED-CONTENT"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("LOCAL-ORIGINAL"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := linkFile(source, dest); err != nil {
			t.Fatal(err)
		}
		a, err := os.Stat(source)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.Stat(dest)
		if err != nil || !os.SameFile(a, b) {
			t.Fatalf("destination is not a hardlink: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 2 {
			t.Fatalf("temporary link leaked on restore %d: %v %v", i, entries, err)
		}
	}
}

func TestHardlinkReplacementFailureKeepsNonemptyDirectory(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	dest := filepath.Join(dir, "dest")
	if err := os.WriteFile(source, []byte("ARCHIVED"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dest, "keep")
	if err := os.WriteFile(child, []byte("LOCAL-ORIGINAL"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := linkFile(source, dest); err == nil {
		t.Fatal("nonempty directory replaced")
	}
	got, err := os.ReadFile(child)
	if err != nil || string(got) != "LOCAL-ORIGINAL" {
		t.Fatalf("directory contents changed: %q %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary link leaked: %v %v", entries, err)
	}
}
