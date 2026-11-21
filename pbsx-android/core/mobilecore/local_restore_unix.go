//go:build linux || android

package mobilecore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// LocalRestore is a descriptor-anchored destination used only inside the
// authorized Shizuku service. Java validates the selected profile/root first.
// All child traversal rejects symlinks, writes create exclusive staging files,
// and no-replace commits use the kernel's atomic no-replace rename operation.
type LocalRestore struct {
	mu sync.RWMutex
	fd int
}

var _ Documents = (*LocalRestore)(nil)

func NewLocalRestore(absolute string) (*LocalRestore, error) {
	if !strings.HasPrefix(absolute, "/") || path.Clean(absolute) != absolute {
		return nil, errors.New("restore root must be a canonical absolute directory")
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, name := range strings.Split(strings.TrimPrefix(absolute, "/"), "/") {
		if name == "" {
			continue
		}
		next, openErr := unix.Openat(fd, name, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	opened, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	unix.Close(fd)
	if err != nil {
		return nil, err
	}
	return &LocalRestore{fd: opened}, nil
}

func (r *LocalRestore) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fd < 0 {
		return nil
	}
	err := unix.Close(r.fd)
	r.fd = -1
	return err
}

// NewDirectory creates an exclusive fresh restore directory and returns an
// independently owned descriptor rooted there. Existing directories are refused.
func (r *LocalRestore) NewDirectory(relative string) (*LocalRestore, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fd, name, err := r.parent(relative)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	if err = unix.Mkdirat(fd, name, 0700); err != nil {
		return nil, err
	}
	child, err := openSyncedRestoreDirectory(fd, name)
	if err != nil {
		return nil, err
	}
	return &LocalRestore{fd: child}, nil
}

func openSyncedRestoreDirectory(parent int, name string) (int, error) {
	child, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	if err = unix.Fsync(child); err != nil {
		unix.Close(child)
		return -1, fmt.Errorf("sync restore directory %q: %w", name, err)
	}
	if err = unix.Fsync(parent); err != nil {
		unix.Close(child)
		return -1, fmt.Errorf("sync parent of restore directory %q: %w", name, err)
	}
	return child, nil
}

func (r *LocalRestore) parent(relative string) (int, string, error) {
	if err := safePath(relative); err != nil {
		return -1, "", err
	}
	if r.fd < 0 {
		return -1, "", errors.New("restore destination is closed")
	}
	fd, err := unix.FcntlInt(uintptr(r.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, "", err
	}
	parts := strings.Split(relative, "/")
	for _, name := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(fd, name, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if openErr != nil {
			return -1, "", openErr
		}
		fd = next
	}
	opened, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	unix.Close(fd)
	if err != nil {
		return -1, "", err
	}
	return opened, parts[len(parts)-1], nil
}

func restoreStat(fd int, name string) (map[string]any, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, err
	}
	mode := stat.Mode & unix.S_IFMT
	if mode != unix.S_IFDIR && mode != unix.S_IFREG {
		return nil, errors.New("restore destination contains a symlink or special file")
	}
	return map[string]any{"name": name, "directory": mode == unix.S_IFDIR, "size": stat.Size, "modified": stat.Mtim.Sec*1000 + stat.Mtim.Nsec/1000000, "exists": true}, nil
}

func (r *LocalRestore) StatJSON(relative string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fd, name, err := r.parent(relative)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	stat, err := restoreStat(fd, name)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(stat)
	return string(raw), err
}

func (r *LocalRestore) InspectJSON(relative string) (string, error) {
	raw, err := r.StatJSON(relative)
	if errors.Is(err, unix.ENOENT) {
		return `{"exists":false}`, nil
	}
	return raw, err
}

func (r *LocalRestore) ListJSON(relative string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fd, name, err := r.parent(relative)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	child, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(child), relative)
	defer file.Close()
	entries, err := file.ReadDir(40001)
	if err != nil && err != io.EOF {
		return "", err
	}
	if len(entries) > 40000 {
		return "", errors.New("restore directory exceeds 40,000 entries")
	}
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		stat, err := restoreStat(child, entry.Name())
		if err != nil {
			return "", err
		}
		out = append(out, stat)
	}
	raw, err := json.Marshal(out)
	if len(raw) > 350000 {
		return "", errors.New("directory exceeds Binder message size")
	}
	return string(raw), err
}

func (r *LocalRestore) OpenRead(relative string) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fd, name, err := r.parent(relative)
	if err != nil {
		return -1, err
	}
	defer unix.Close(fd)
	file, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	var stat unix.Stat_t
	err = unix.Fstat(file, &stat)
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(file)
		return -1, fmt.Errorf("not a readable regular restore file: %v", err)
	}
	return int64(file), nil
}

func (r *LocalRestore) MakeDirectory(relative string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fd, name, err := r.parent(relative)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err = unix.Mkdirat(fd, name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	stat, err := restoreStat(fd, name)
	if err != nil {
		return err
	}
	if !stat["directory"].(bool) {
		return errors.New("restore directory conflicts with an existing file")
	}
	// Sync on reuse too: a previous mkdir may have succeeded before its
	// durability barrier failed. Opening under the same descriptor rejects links.
	child, err := openSyncedRestoreDirectory(fd, name)
	if err != nil {
		return err
	}
	return unix.Close(child)
}

func stagingName(relative string) bool { return strings.HasPrefix(path.Base(relative), ".pbsx-stage-") }

func (r *LocalRestore) OpenWrite(relative string) (int64, error) {
	if !stagingName(relative) {
		return -1, errors.New("restore writes must use a staging file")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	fd, name, err := r.parent(relative)
	if err != nil {
		return -1, err
	}
	defer unix.Close(fd)
	file, err := unix.Openat(fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	return int64(file), err
}

func (r *LocalRestore) CommitFile(temporary, relative string, replace bool) error {
	if !stagingName(temporary) || path.Dir(temporary) != path.Dir(relative) || temporary == relative {
		return errors.New("restore staging file must be a distinct sibling")
	}
	if err := safePath(temporary); err != nil {
		return err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	fd, name, err := r.parent(relative)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	staged := path.Base(temporary)
	stat, err := restoreStat(fd, staged)
	if err != nil {
		return err
	}
	if stat["directory"].(bool) {
		return errors.New("staging source is not a regular file")
	}
	if target, err := restoreStat(fd, name); err == nil {
		if !replace {
			return errors.New("restore destination appeared before commit")
		}
		if target["directory"].(bool) {
			return errors.New("cannot replace a directory with a file")
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	file, err := unix.Openat(fd, staged, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	err = unix.Fsync(file)
	unix.Close(file)
	if err != nil {
		return err
	}
	flags := uint(0)
	if !replace {
		flags = unix.RENAME_NOREPLACE
	}
	if err = unix.Renameat2(fd, staged, fd, name, flags); err != nil {
		return err
	}
	if err = unix.Fsync(fd); err != nil {
		return fmt.Errorf("file restored but destination directory sync failed: %w", err)
	}
	return nil
}

func (r *LocalRestore) DeleteFile(relative string) error {
	if !stagingName(relative) {
		return errors.New("only restore staging files may be removed")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	fd, name, err := r.parent(relative)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	stat, err := restoreStat(fd, name)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if stat["directory"].(bool) {
		return errors.New("cannot remove a directory as a staging file")
	}
	return unix.Unlinkat(fd, name, 0)
}
