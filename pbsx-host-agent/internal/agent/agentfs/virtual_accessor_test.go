package agentfs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
)

func TestVirtualAccessorReadsSourceRootAndFiltersExcludes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip.txt"), []byte("skip"), 0o600); err != nil {
		t.Fatal(err)
	}

	accessor, err := NewVirtualAccessor("test-job", "standard", []snapshots.Snapshot{{
		Path:       root,
		SourcePath: root,
		Direct:     true,
		Excludes:   []string{"skip.txt"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer accessor.Close()

	rootEntries, err := accessor.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(rootEntries) != 1 {
		t.Fatalf("virtual root has %d entries, want 1", len(rootEntries))
	}
	alias := rootEntries[0].Name

	entries, err := accessor.ReadDir(alias)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	if want := []string{"keep.txt"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("entries = %#v, want %#v", names, want)
	}

	reader, size, err := accessor.OpenFile(alias + "/keep.txt")
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	if size != 4 {
		t.Fatalf("size = %d, want 4", size)
	}
	if _, _, err := accessor.OpenFile(alias + "/skip.txt"); !os.IsNotExist(err) {
		t.Fatalf("excluded file error = %v, want not-exist", err)
	}
}

func TestVirtualAccessorDisambiguatesDuplicateAliases(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	source := "/same/source"
	accessor, err := NewVirtualAccessor("test-job", "standard", []snapshots.Snapshot{
		{Path: first, SourcePath: source, Direct: true},
		{Path: second, SourcePath: source, Direct: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer accessor.Close()

	entries, err := accessor.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	base := sourceAlias(source)
	got := []string{entries[0].Name, entries[1].Name}
	want := []string{base, base + "-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aliases = %#v, want %#v", got, want)
	}
}

func TestNormalizeVirtualExcludes(t *testing.T) {
	got := normalizeVirtualExcludes([]string{`cache\\tmp`, "/cache/tmp/", "../escape", ".", "logs"})
	want := []string{"cache/tmp", "logs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized excludes = %#v, want %#v", got, want)
	}
}
