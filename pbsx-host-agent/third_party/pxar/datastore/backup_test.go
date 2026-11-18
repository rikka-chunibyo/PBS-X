package datastore

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBackupTypeString(t *testing.T) {
	tests := []struct {
		bt   BackupType
		want string
	}{
		{bt: BackupVM, want: "vm"},
		{bt: BackupCT, want: "ct"},
		{bt: BackupHost, want: "host"},
	}
	for _, tt := range tests {
		if tt.bt.String() != tt.want {
			t.Errorf("BackupType(%d).String() = %q, want %q", tt.bt, tt.bt.String(), tt.want)
		}
	}
}

func TestBackupGroupPath(t *testing.T) {
	bg := BackupGroup{Type: BackupVM, ID: "100"}
	expected := filepath.Join("vm", "100")
	if bg.Path() != expected {
		t.Errorf("path = %s, want %s", bg.Path(), expected)
	}
}

func TestBackupDirPath(t *testing.T) {
	ts := time.Unix(1700000000, 0)
	bd := BackupDir{
		Group:     BackupGroup{Type: BackupHost, ID: "myhost"},
		Timestamp: ts,
	}
	expected := filepath.Join("host", "myhost", "2023-11-14T22:13:20Z")
	if bd.Path() != expected {
		t.Errorf("path = %s, want %s", bd.Path(), expected)
	}
}

func TestBackupGroupRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PBS datastore snapshot directories require POSIX filenames containing RFC3339 colons")
	}
	dir := t.TempDir()

	bg := BackupGroup{Type: BackupVM, ID: "100", Base: dir}
	groupPath := filepath.Join(dir, bg.Path())
	if err := os.MkdirAll(groupPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create two snapshot dirs
	for _, ts := range []int64{1700000000, 1700100000} {
		snapDir := filepath.Join(dir, BackupDir{
			Group:     bg,
			Timestamp: time.Unix(ts, 0),
		}.Path())
		if err := os.MkdirAll(snapDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	var snapshots []BackupDir
	err := bg.ListSnapshots(func(snap BackupDir) error {
		snapshots = append(snapshots, snap)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 {
		t.Errorf("snapshots = %d, want 2", len(snapshots))
	}
}

func TestBackupInfoFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PBS datastore snapshot directories require POSIX filenames containing RFC3339 colons")
	}
	dir := t.TempDir()
	bd := BackupDir{
		Group:     BackupGroup{Type: BackupCT, ID: "200", Base: dir},
		Timestamp: time.Unix(1700000000, 0),
	}
	snapDir := filepath.Join(dir, bd.Path())
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create some files
	for _, name := range []string{"root.pxar.didx", "config.blob"} {
		if err := os.WriteFile(filepath.Join(snapDir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	info, err := bd.Info()
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Files) != 2 {
		t.Errorf("files = %d, want 2", len(info.Files))
	}
}

func TestBackupGroupDestroy(t *testing.T) {
	dir := t.TempDir()
	bg := BackupGroup{Type: BackupVM, ID: "100", Base: dir}
	groupPath := filepath.Join(dir, bg.Path())
	if err := os.MkdirAll(groupPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := bg.Destroy(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(groupPath); !os.IsNotExist(err) {
		t.Error("group directory should be removed")
	}
}
