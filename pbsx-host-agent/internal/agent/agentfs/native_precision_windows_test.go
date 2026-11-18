//go:build windows

package agentfs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
	"golang.org/x/sys/windows"
)

func TestNativeWindowsReadExcludesWriters(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("stable"), 0600); err != nil {
		t.Fatal(err)
	}
	a := NewNativeAccessor("protected", "standard", snapshots.Snapshot{Path: root, SourcePath: root, Direct: true})
	defer a.Close()
	writer, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, _, err := a.OpenFile("file")
	if reader != nil {
		reader.Close()
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("existing writer accepted: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, _, err = a.OpenFile("file")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := os.WriteFile(path, []byte("changed"), 0600); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("new writer accepted: %v", err)
	}
	if err := os.Remove(path); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("delete accepted during read: %v", err)
	}
	if got, err := io.ReadAll(reader); err != nil || string(got) != "stable" {
		t.Fatalf("protected content = %q: %v", got, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("updated"), 0600); err != nil {
		t.Fatalf("read left the file protected after close: %v", err)
	}
}

func TestNativeWindowsAccessorKeepsSubsecondMtime(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("AAAA"), 0600); err != nil {
		t.Fatal(err)
	}
	a := NewNativeAccessor("precise", "standard", snapshots.Snapshot{Path: root, SourcePath: root, Direct: true})
	defer a.Close()
	var previous time.Time
	for _, nanos := range []int64{100000000, 900000000} {
		want := time.Unix(1700000000, nanos)
		if err := os.Chtimes(path, want, want); err != nil {
			t.Fatal(err)
		}
		info, err := a.Stat("file")
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mtime.Time().Equal(want) || info.Mtime.Time().Equal(previous) {
			t.Fatalf("mtime = %s, expected %s", info.Mtime.Time(), want)
		}
		entries, err := a.ReadDir(".")
		if err != nil || len(entries) != 1 || entries[0].Stat.Mtime != info.Mtime {
			t.Fatalf("directory precision mismatch: %+v %v", entries, err)
		}
		previous = info.Mtime.Time()
	}
}
