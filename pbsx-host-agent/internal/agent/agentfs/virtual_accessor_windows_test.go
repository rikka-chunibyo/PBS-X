//go:build windows

package agentfs

import "testing"

func TestSourceAliasPreservesWindowsRootIdentity(t *testing.T) {
	tests := map[string]string{
		`C:\`:                  "C",
		`C:\Users\test\source`: "C-Users-test-source",
		`D:\Users\test\source`: "D-Users-test-source",
	}
	for source, want := range tests {
		if got := sourceAlias(source); got != want {
			t.Errorf("sourceAlias(%q) = %q, want %q", source, got, want)
		}
	}
}
