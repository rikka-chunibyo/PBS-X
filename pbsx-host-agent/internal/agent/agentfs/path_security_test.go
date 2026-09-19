package agentfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalRelativePath(t *testing.T) {
	good := map[string]string{
		"":          ".",
		".":         ".",
		"foo":       "foo",
		"foo/bar":   Join("foo", "bar"),
		`foo\\bar`:  Join("foo", "bar"),
		"./foo/bar": Join("foo", "bar"),
	}
	for input, want := range good {
		got, err := canonicalRelativePath(input)
		if err != nil {
			t.Fatalf("canonicalRelativePath(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("canonicalRelativePath(%q) = %q, want %q", input, got, want)
		}
	}

	bad := []string{
		"../etc", "foo/../bar", "/etc/passwd", `\\server\\share`,
		`C:\\Windows`, `C:relative`, `\\?\\C:\\Windows`, `\\??\\C:\\Windows`,
		"foo\x00bar",
	}
	for _, input := range bad {
		if got, err := canonicalRelativePath(input); err == nil {
			t.Fatalf("canonicalRelativePath(%q) unexpectedly accepted as %q", input, got)
		}
	}
}

func TestResolvedParentConfinement(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("covered by Windows integration tests")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := ensureResolvedParentWithinRoot(root, filepath.Join(root, "escape", "file")); err == nil {
		t.Fatal("symlink parent escape unexpectedly accepted")
	}
	inside := filepath.Join(root, "inside")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureResolvedParentWithinRoot(root, filepath.Join(inside, "file")); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
}
