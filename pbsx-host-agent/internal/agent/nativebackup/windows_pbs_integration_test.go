//go:build windows

package nativebackup_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/nativebackup"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/nativerestore"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/winpath"
	"golang.org/x/sys/windows"
)

// This test writes actual snapshots using the host's enrolled credentials.
// Run only on an elevated, disposable Windows host enrolled in a test datastore.
func TestWindowsPBSRoundTripCases(t *testing.T) {
	if os.Getenv("PBSX_WINDOWS_PBS_INTEGRATION") != "1" {
		t.Skip("set PBSX_WINDOWS_PBS_INTEGRATION=1 on an enrolled Windows test host")
	}
	store := os.Getenv("PBSX_WINDOWS_TEST_DATASTORE")
	if store == "" {
		t.Fatal("PBSX_WINDOWS_TEST_DATASTORE must explicitly select a test datastore")
	}
	host, err := agent.AgentHostID()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	sources := []string{filepath.Join(base, "one", "data"), filepath.Join(base, "two", "data")}
	write := func(t *testing.T, path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i, source := range sources {
		write(t, filepath.Join(source, "same.txt"), []byte(fmt.Sprintf("source number %d", i)))
	}
	source := sources[0]
	write(t, filepath.Join(source, "empty.txt"), nil)
	write(t, filepath.Join(source, "locked.txt"), []byte("VSS reads an exclusively locked file"))
	lockedPath, _ := windows.UTF16PtrFromString(filepath.Join(source, "locked.txt"))
	locked, err := windows.CreateFile(lockedPath, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(locked) })
	write(t, filepath.Join(source, "unicode-雪-café.txt"), []byte("UTF-8 payload 雪"))
	if err := os.Mkdir(filepath.Join(source, "empty-directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(source, "hard-original.txt"), []byte("shared hardlink contents"))
	if err := os.Link(filepath.Join(source, "hard-original.txt"), filepath.Join(source, "hard-copy.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("same.txt", filepath.Join(source, "relative-link.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(source, "Excluded", "omit.txt"), []byte("must not be archived"))
	write(t, filepath.Join(source, "streams.txt"), []byte("main stream"))
	ads := bytes.Repeat([]byte("stream-data-"), 20000) // multiple 60 KiB metadata chunks
	write(t, filepath.Join(source, "streams.txt")+":large", ads)
	write(t, filepath.Join(source, "streams.txt")+":empty", nil)
	write(t, filepath.Join(source, "attributes.txt"), []byte("attribute payload"))
	write(t, filepath.Join(source, "attributes.txt")+":meta", []byte("read-only file ADS"))
	attrPath := filepath.Join(source, "attributes.txt")
	ptr, _ := windows.UTF16PtrFromString(attrPath)
	if err := windows.SetFileAttributes(ptr, windows.FILE_ATTRIBUTE_READONLY|windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.SetFileAttributes(ptr, windows.FILE_ATTRIBUTE_NORMAL) })
	longRel := filepath.Join(strings.Repeat("a", 90), strings.Repeat("b", 90), strings.Repeat("c", 90), "long.txt")
	write(t, filepath.Join(source, longRel), []byte("long path payload"))
	stamp := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(source, "same.txt"), stamp, stamp); err != nil {
		t.Fatal(err)
	}

	job := nativebackup.Job{ID: "windows-edge-cases", HostID: host, Datastore: store,
		Sources: sources, Excludes: []string{"excluded"}, SnapshotMode: "snapshot", ChangeDetectionMode: "metadata"}
	backup := func(t *testing.T) *nativebackup.Result {
		t.Helper()
		result, err := nativebackup.Run(t.Context(), job)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("committed snapshot=%d files=%d bytes=%d", result.BackupTime, result.Files, result.LogicalBytes)
		return result
	}
	restore := func(t *testing.T, result *nativebackup.Result, name, mode string, paths []string) string {
		t.Helper()
		dest := filepath.Join(base, name)
		if err := nativerestore.Run(t.Context(), nativerestore.Job{ID: name, HostID: host, Datastore: store,
			BackupTime: result.BackupTime, Destination: dest, Mode: mode, Sources: paths}); err != nil {
			t.Fatal(err)
		}
		return dest
	}
	aliases := make(map[string]string)
	usedAliases := make(map[string]int)
	for _, source := range sources {
		parsed, err := winpath.ParseAbsolute(source)
		if err != nil {
			t.Fatal(err)
		}
		name := parsed.Alias()
		usedAliases[name]++
		if usedAliases[name] > 1 {
			name = fmt.Sprintf("%s-%d", name, usedAliases[name])
		}
		aliases[source] = name
	}
	alias := func(t *testing.T, source string) string {
		t.Helper()
		return aliases[source]
	}
	checkFile := func(t *testing.T, path string, expected []byte) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, expected) {
			t.Fatalf("content mismatch: %s", path)
		}
	}

	first := backup(t)
	dest := restore(t, first, "full-restore", "normal", []string{"/"})
	root := filepath.Join(dest, alias(t, source))
	t.Run("multiple-sources", func(t *testing.T) {
		for i, src := range sources {
			checkFile(t, filepath.Join(dest, alias(t, src), "same.txt"), []byte(fmt.Sprintf("source number %d", i)))
		}
	})
	t.Run("empty-and-unicode", func(t *testing.T) {
		checkFile(t, filepath.Join(root, "empty.txt"), nil)
		checkFile(t, filepath.Join(root, "unicode-雪-café.txt"), []byte("UTF-8 payload 雪"))
		entries, err := os.ReadDir(filepath.Join(root, "empty-directory"))
		if err != nil || len(entries) != 0 {
			t.Fatalf("empty directory: entries=%d err=%v", len(entries), err)
		}
	})
	t.Run("exclusive-file-lock", func(t *testing.T) {
		checkFile(t, filepath.Join(root, "locked.txt"), []byte("VSS reads an exclusively locked file"))
	})
	t.Run("hardlink-identity", func(t *testing.T) {
		a, err := os.Stat(filepath.Join(root, "hard-original.txt"))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.Stat(filepath.Join(root, "hard-copy.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(a, b) {
			t.Fatal("restored hardlinks have different identities")
		}
	})
	t.Run("relative-symlink", func(t *testing.T) {
		target, err := os.Readlink(filepath.Join(root, "relative-link.txt"))
		if err != nil || target != "same.txt" {
			t.Fatalf("target=%q err=%v", target, err)
		}
	})
	t.Run("case-insensitive-exclusion", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(root, "Excluded")); !os.IsNotExist(err) {
			t.Fatalf("excluded path remains: %v", err)
		}
	})
	t.Run("chunked-and-empty-ads", func(t *testing.T) {
		checkFile(t, filepath.Join(root, "streams.txt")+":large", ads)
		checkFile(t, filepath.Join(root, "streams.txt")+":empty", nil)
	})
	t.Run("long-path", func(t *testing.T) { checkFile(t, filepath.Join(root, longRel), []byte("long path payload")) })
	t.Run("attributes-and-mtime", func(t *testing.T) {
		checkFile(t, filepath.Join(root, "attributes.txt")+":meta", []byte("read-only file ADS"))
		p, _ := windows.UTF16PtrFromString(filepath.Join(root, "attributes.txt"))
		attrs, err := windows.GetFileAttributes(p)
		if err != nil || attrs&windows.FILE_ATTRIBUTE_READONLY == 0 || attrs&windows.FILE_ATTRIBUTE_HIDDEN == 0 {
			t.Fatalf("attrs=%x err=%v", attrs, err)
		}
		info, err := os.Stat(filepath.Join(root, "same.txt"))
		if err != nil || !info.ModTime().Equal(stamp) {
			t.Fatalf("mtime mismatch: %v", err)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("metadata-incremental-ads-change", func(t *testing.T) {
		path := filepath.Join(source, "streams.txt")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		ads[0] ^= 0x20
		write(t, path+":large", ads)
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(source, "empty.txt")); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(source, "added.txt"), []byte("new in second snapshot"))
		for time.Now().Unix() <= first.BackupTime {
			time.Sleep(20 * time.Millisecond)
		}
		second := backup(t)
		secondDest := restore(t, second, "incremental-restore", "normal", []string{"/"})
		secondRoot := filepath.Join(secondDest, alias(t, source))
		checkFile(t, filepath.Join(secondRoot, "streams.txt")+":large", ads)
		checkFile(t, filepath.Join(secondRoot, "added.txt"), []byte("new in second snapshot"))
		if _, err := os.Stat(filepath.Join(secondRoot, "empty.txt")); !os.IsNotExist(err) {
			t.Fatalf("deleted file restored: %v", err)
		}
		old, err := os.ReadFile(filepath.Join(root, "streams.txt") + ":large")
		if err != nil {
			t.Fatal(err)
		}
		if sha256.Sum256(old) == sha256.Sum256(ads) {
			t.Fatal("first restore unexpectedly changed")
		}
	})
	t.Run("selected-file-noattr", func(t *testing.T) {
		selected := "/" + alias(t, source) + "/same.txt"
		selectedDest := restore(t, first, "selected-restore", "noattr", []string{selected})
		var files []string
		_ = filepath.WalkDir(selectedDest, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				files = append(files, path)
			}
			return nil
		})
		if len(files) != 1 {
			t.Fatalf("selected restore produced %d files", len(files))
		}
		checkFile(t, files[0], []byte("source number 0"))
	})
}

func TestWindowsPBSRejectsOversizedADS(t *testing.T) {
	if os.Getenv("PBSX_WINDOWS_PBS_INTEGRATION") != "1" {
		t.Skip("requires opted-in Windows test host")
	}
	store := os.Getenv("PBSX_WINDOWS_TEST_DATASTORE")
	if store == "" {
		t.Fatal("explicit test datastore required")
	}
	host, err := agent.AgentHostID()
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	path := filepath.Join(source, "oversized.txt")
	if err := os.WriteFile(path, []byte("main stream"), 0o600); err != nil {
		t.Fatal(err)
	}
	stream, err := os.Create(path + ":oversized")
	if err != nil {
		t.Fatal(err)
	}
	err = stream.Truncate((32 << 20) + 1)
	closeErr := stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	_, err = nativebackup.Run(t.Context(), nativebackup.Job{
		ID: "windows-oversized-ads", HostID: host, Datastore: store,
		Sources: []string{source}, SnapshotMode: "snapshot", ChangeDetectionMode: "data",
	})
	if err == nil || !strings.Contains(err.Error(), "streams larger than") {
		t.Fatalf("oversized ADS should fail explicitly, got %v", err)
	}
	t.Logf("oversized ADS rejected explicitly: %v", err)
}
