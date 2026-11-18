//go:build windows

package agentfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
)

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
