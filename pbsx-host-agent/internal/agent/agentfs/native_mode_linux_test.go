//go:build linux

package agentfs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
	"golang.org/x/sys/unix"
)

func TestNativeAccessorPreservesNanosecondMtime(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "entry")
	if err := os.WriteFile(path, []byte("mtime"), 0600); err != nil {
		t.Fatal(err)
	}
	want := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(path, want, want); err != nil {
		t.Fatal(err)
	}
	a := NewNativeAccessor("mtime", "standard", snapshots.Snapshot{Path: root, SourcePath: root, Direct: true})
	defer a.Close()
	info, err := a.Stat("entry")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mtime.Time().Equal(want) {
		t.Fatalf("native mtime %s, want %s", info.Mtime.Time(), want)
	}
}

func TestNativeAccessorPreservesSpecialPermissions(t *testing.T) {
	for _, tc := range []struct {
		mode uint32
		dir  bool
	}{
		{04755, false}, {02750, false}, {06755, false}, {01777, true}, {02770, true},
	} {
		t.Run(fmt.Sprintf("%04o", tc.mode), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "entry")
			var err error
			if tc.dir {
				err = os.Mkdir(path, 0700)
			} else {
				err = os.WriteFile(path, []byte("mode"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = unix.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			a := NewNativeAccessor("permissions", "standard", snapshots.Snapshot{Path: root, SourcePath: root, Direct: true})
			defer a.Close()
			stat, err := a.Stat("entry")
			if err != nil {
				t.Fatal(err)
			}
			if got := stat.Mode & 07777; got != uint64(tc.mode) {
				t.Fatalf("Stat mode %04o, want %04o", got, tc.mode)
			}
			entries, err := a.ReadDir("/")
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Stat.Mode&07777 != uint64(tc.mode) {
				t.Fatalf("ReadDir lost permissions: %+v", entries)
			}
		})
	}
}
