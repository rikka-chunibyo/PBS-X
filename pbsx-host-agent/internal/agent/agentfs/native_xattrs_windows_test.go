//go:build windows

package agentfs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestEnumerateAlternateDataStreamsDirectory(t *testing.T) {
	streams, err := enumerateAlternateDataStreams(t.TempDir())
	if err != nil {
		t.Fatalf("enumerate directory streams: %v", err)
	}
	if len(streams) != 0 {
		t.Fatalf("directory streams = %q, want none", streams)
	}
}

func TestEnumerateAlternateDataStreamsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ads.txt")
	if err := os.WriteFile(path, []byte("primary data"), 0o600); err != nil {
		t.Fatalf("create primary stream: %v", err)
	}
	if err := os.WriteFile(path+":pbsx.meta", []byte("alternate data"), 0o600); err != nil {
		t.Fatalf("create alternate stream: %v", err)
	}

	streams, err := enumerateAlternateDataStreams(path)
	if err != nil {
		t.Fatalf("enumerate file streams: %v", err)
	}
	if len(streams) != 1 || streams[0] != "pbsx.meta" {
		t.Fatalf("file streams = %q, want [pbsx.meta]", streams)
	}

	attrs, err := nativePlatformXAttrs(path)
	if err != nil {
		t.Fatalf("encode alternate stream metadata: %v", err)
	}
	if len(attrs) < 2 {
		t.Fatalf("encoded ADS attrs = %d, want manifest and payload", len(attrs))
	}
}

func TestNoMoreAlternateDataStreams(t *testing.T) {
	for _, err := range []error{windows.ERROR_HANDLE_EOF, windows.ERROR_NO_MORE_FILES} {
		if !noMoreAlternateDataStreams(err) {
			t.Errorf("%v was not recognized as stream enumeration completion", err)
		}
	}
}
