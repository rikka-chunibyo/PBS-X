//go:build windows

package agentfs

import (
	"testing"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
)

func TestAbsRootDoesNotAppendDotSegment(t *testing.T) {
	server := &Server{snapshot: snapshots.Snapshot{Path: `C:\Users\test\source`}}
	want := `\\?\C:\Users\test\source`
	for _, root := range []string{"", ".", "/"} {
		if got := server.abs(root); got != want {
			t.Fatalf("abs(%q) = %q, want %q", root, got, want)
		}
	}
}

func TestAbsJoinsRelativeChild(t *testing.T) {
	server := &Server{snapshot: snapshots.Snapshot{Path: `C:\Users\test\source`}}
	want := `\\?\C:\Users\test\source\child`
	if got := server.abs("child"); got != want {
		t.Fatalf("abs(child) = %q, want %q", got, want)
	}
}
