// Package mobilecore connects Android document providers to the PBS archive engine.
// The exported API intentionally uses only gomobile-compatible types.
package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/format"
)

// Documents supplies a virtual, portable file tree. File descriptors transfer
// ownership to Go: Java must detachFd(), and Go closes them after use.
// Paths are relative, with "." representing the virtual root. JSON entries have
// name, size, modified (Unix milliseconds), and directory fields.
type Documents interface {
	StatJSON(relative string) (string, error)
	ListJSON(relative string) (string, error)
	OpenRead(relative string) (int64, error)
	MakeDirectory(relative string) error
	OpenWrite(relative string) (int64, error)
	// InspectJSON reports {exists:false} for a missing destination. Permission
	// and provider failures must return errors, never masquerade as absence.
	InspectJSON(relative string) (string, error)
	// CommitFile promotes a completed sibling temporary file. Replacement must
	// preserve the old file on failure; new files must not overwrite a conflict.
	CommitFile(temporary, relative string, replace bool) error
	// DeleteFile removes only a regular file, and succeeds if already absent.
	DeleteFile(relative string) error
}

type Progress interface {
	Update(stage string, files int64, bytes int64)
}

// Providers can return pipes as well as ordinary files. Mark the transferred
// descriptor nonblocking before NewFile so Go's poller can interrupt a blocked
// read/write when cancellation closes it. Regular files ignore O_NONBLOCK.
func documentFile(fd int64, name string) (*os.File, error) {
	if fd < 0 || fd > 1<<31-1 {
		return nil, errors.New("invalid document descriptor")
	}
	if err := syscall.SetNonblock(int(fd), true); err != nil {
		syscall.Close(int(fd))
		return nil, fmt.Errorf("prepare document descriptor: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		syscall.Close(int(fd))
		return nil, errors.New("invalid document descriptor")
	}
	return file, nil
}

type documentEntry struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Modified  int64  `json:"modified"`
	Directory bool   `json:"directory"`
}

func safePath(p string) error {
	if !utf8.ValidString(p) {
		return errors.New("Android cannot represent this non-UTF-8 archive name")
	}
	if p == "." {
		return nil
	}
	if p == "" || len(p) > 4096 || strings.ContainsAny(p, "\x00\\") || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return errors.New("unsafe archive path")
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." || part == "" {
			return errors.New("unsafe archive path")
		}
	}
	if strings.Count(p, "/") > 128 {
		return errors.New("directory nesting exceeds 128")
	}
	return nil
}

func entryStat(e documentEntry) (format.Stat, error) {
	if e.Size < 0 {
		return format.Stat{}, errors.New("provider did not report a valid file size")
	}
	mode := uint64(format.ModeIFREG | 0644)
	if e.Directory {
		mode = uint64(format.ModeIFDIR | 0755)
	}
	return format.Stat{Mode: mode, Mtime: format.NewStatxTimestamp(e.Modified/1000, uint32((e.Modified%1000)*1e6))}, nil
}

type documentFS struct {
	backupproxy.NoExtendedAttrs
	docs   Documents
	ctx    context.Context
	report *transferReporter
}

func (f *documentFS) entry(p string) (documentEntry, error) {
	p = strings.TrimPrefix(p, "./")
	if err := safePath(p); err != nil {
		return documentEntry{}, err
	}
	if err := f.ctx.Err(); err != nil {
		return documentEntry{}, err
	}
	raw, err := f.docs.StatJSON(p)
	if err != nil {
		return documentEntry{}, err
	}
	var e documentEntry
	if len(raw) > 16384 {
		return e, errors.New("oversized document metadata")
	}
	err = json.Unmarshal([]byte(raw), &e)
	return e, err
}
func (f *documentFS) Stat(p string) (format.Stat, error) {
	e, err := f.entry(p)
	if err != nil {
		return format.Stat{}, err
	}
	return entryStat(e)
}
func (f *documentFS) ReadDir(p string) ([]backupproxy.DirEntry, error) {
	p = strings.TrimPrefix(p, "./")
	f.report.phase("Scanning folders", p, -1)
	if err := safePath(p); err != nil {
		return nil, err
	}
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := f.docs.ListJSON(p)
	if err != nil {
		return nil, err
	}
	if len(raw) > 8<<20 {
		return nil, errors.New("directory metadata exceeds 8 MiB")
	}
	var entries []documentEntry
	if err = json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]backupproxy.DirEntry, 0, len(entries))
	for _, e := range entries {
		if e.Name == "." || strings.Contains(e.Name, "/") || safePath(e.Name) != nil || seen[e.Name] {
			return nil, fmt.Errorf("unsafe or duplicate document name %q", e.Name)
		}
		seen[e.Name] = true
		st, err := entryStat(e)
		if err != nil {
			return nil, err
		}
		out = append(out, backupproxy.DirEntry{Name: e.Name, Stat: st, Size: uint64(e.Size)})
	}
	return out, nil
}
func (f *documentFS) OpenFile(p string) (io.ReadCloser, uint64, error) {
	p = strings.TrimPrefix(p, "./")
	e, err := f.entry(p)
	if err != nil {
		return nil, 0, err
	}
	if e.Directory || e.Size < 0 {
		return nil, 0, errors.New("not a sized regular file")
	}
	f.report.phase("Backing up", p, e.Size)
	fd, err := f.docs.OpenRead(p)
	if err != nil {
		return nil, 0, err
	}
	file, err := documentFile(fd, p)
	if err != nil {
		return nil, 0, err
	}
	stop := context.AfterFunc(f.ctx, func() { file.Close() })
	return &checkedDocument{file: file, fs: f, path: p, before: e, stop: stop}, uint64(e.Size), nil
}
func (f *documentFS) ReadLink(string) (string, error) {
	return "", errors.New("Android document trees do not support symlinks")
}

type checkedDocument struct {
	file    *os.File
	fs      *documentFS
	path    string
	before  documentEntry
	read    int64
	checked bool
	stop    func() bool
}

func (r *checkedDocument) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.checked {
		return 0, io.EOF
	}
	if err := r.fs.ctx.Err(); err != nil {
		return 0, err
	}
	// Probe one byte beyond the advertised size so growing files also fail.
	n, err := r.file.Read(p)
	r.read += int64(n)
	r.fs.report.add(int64(n))
	if r.read > r.before.Size {
		return 0, errors.New("file grew while being backed up")
	}
	if err == io.EOF || r.read == r.before.Size {
		if r.read != r.before.Size {
			return 0, errors.New("file shrank while being backed up")
		}
		if err != io.EOF {
			var probe [1]byte
			extra, probeErr := r.file.Read(probe[:])
			if extra != 0 {
				return 0, errors.New("file grew while being backed up")
			}
			if probeErr != io.EOF {
				return 0, errors.New("could not verify source EOF")
			}
		}
		if e, eerr := r.fs.entry(r.path); eerr != nil {
			return 0, eerr
		} else if e.Size != r.before.Size || e.Modified != r.before.Modified {
			return 0, errors.New("file changed while being backed up")
		}
		r.checked = true
		r.fs.report.fileDone()
	}
	return n, err
}
func (r *checkedDocument) Close() error {
	if r.stop != nil {
		defer r.stop()
	}
	// Read verifies EOF and metadata when it reaches the advertised size.
	// Do not read an unfinished pipe while closing a paused/failed traversal:
	// its writer may remain open indefinitely. Empty files still need a probe.
	var check error
	if !r.checked {
		if r.before.Size == 0 && r.read == 0 {
			var b [1]byte
			_, err := r.Read(b[:])
			if err != io.EOF {
				check = err
				if check == nil {
					check = errors.New("could not verify empty source EOF")
				}
			}
		} else {
			check = errors.New("file was not fully consumed or did not pass source validation")
		}
	}
	return errors.Join(check, r.file.Close())
}
