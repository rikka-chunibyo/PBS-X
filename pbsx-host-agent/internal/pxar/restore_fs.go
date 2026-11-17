package pxar

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// All restore filesystem operations use one directory capability. Path checks
// alone cannot protect a privileged restore against concurrent symlink changes.
type restoreFS struct {
	root *os.Root
	base string
}

func openRestoreFS(base string) (*restoreFS, error) {
	absolute, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	return &restoreFS{root: root, base: absolute}, nil
}
func (f *restoreFS) relative(p string) (string, error) {
	rel, err := filepath.Rel(f.base, p)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("restore path outside destination: %q", p)
	}
	return rel, nil
}
func (f *restoreFS) open(p string, flags int, mode os.FileMode) (*os.File, error) {
	rel, err := f.relative(p)
	if err != nil {
		return nil, err
	}
	return f.root.OpenFile(rel, flags, mode)
}
func (f *restoreFS) lstat(p string) (os.FileInfo, error) {
	rel, err := f.relative(p)
	if err != nil {
		return nil, err
	}
	return f.root.Lstat(rel)
}
func (f *restoreFS) mkdir(p string) error {
	rel, err := f.relative(p)
	if err != nil {
		return err
	}
	return f.root.MkdirAll(rel, 0755)
}
func (f *restoreFS) remove(p string) error {
	rel, err := f.relative(p)
	if err != nil {
		return err
	}
	return f.root.Remove(rel)
}
func (f *restoreFS) rename(old, new string) error {
	a, err := f.relative(old)
	if err != nil {
		return err
	}
	b, err := f.relative(new)
	if err != nil {
		return err
	}
	return f.root.Rename(a, b)
}
func (f *restoreFS) readlink(p string) (string, error) {
	rel, err := f.relative(p)
	if err != nil {
		return "", err
	}
	return f.root.Readlink(rel)
}
func (f *restoreFS) symlink(target, p string) error {
	rel, err := f.relative(p)
	if err != nil {
		return err
	}
	return f.root.Symlink(target, rel)
}
func (f *restoreFS) link(old, new string) error {
	a, err := f.relative(old)
	if err != nil {
		return err
	}
	b, err := f.relative(new)
	if err != nil {
		return err
	}
	// Prepare the hardlink beside its destination before replacing anything.
	// EXDEV, an unsupported filesystem, or a missing source must leave an
	// existing destination intact, including when no archived fallback exists.
	for i := 0; i < 100; i++ {
		tmp, err := temporaryRestorePath(filepath.Dir(new))
		if err != nil {
			return err
		}
		rel, err := f.relative(tmp)
		if err != nil {
			return err
		}
		if err := f.root.Link(a, rel); os.IsExist(err) {
			continue
		} else if err != nil {
			return err
		}
		// Rename can succeed without consuming tmp if dest already refers to
		// this inode. Always clean up our extra link in that case as well.
		defer f.root.Remove(rel)
		return f.swap(tmp, new)
	}
	return fmt.Errorf("could not allocate restore temporary hardlink for %s", b)
}

func temporaryRestorePath(parent string) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return filepath.Join(parent, ".pxar-restore-"+hex.EncodeToString(nonce[:])), nil
}

func (f *restoreFS) temporary(parent string) (*os.File, error) {
	for i := 0; i < 100; i++ {
		p, err := temporaryRestorePath(parent)
		if err != nil {
			return nil, err
		}
		file, err := f.open(p, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if !os.IsExist(err) {
			return file, err
		}
	}
	return nil, fmt.Errorf("could not allocate restore temporary file")
}
func (f *restoreFS) swap(tmp, dest string) (err error) {
	defer func() {
		if err != nil {
			_ = f.remove(tmp)
		}
	}()
	if err = f.rename(tmp, dest); err == nil {
		return nil
	}
	// Only remove an incompatible empty directory. Never remove an existing
	// file just because rename failed (e.g. a sharing violation on Windows).
	info, statErr := f.lstat(dest)
	if statErr != nil || !info.IsDir() {
		return err
	}
	if removeErr := f.remove(dest); removeErr != nil {
		return fmt.Errorf("replace directory: %w", removeErr)
	}
	return f.rename(tmp, dest)
}

// Internal helpers are also exercised standalone by tests. The public restore
// entry point always supplies a single fixed target for the whole operation.
func (st *restoreState) destinationFS(dest string) (*restoreFS, func(), error) {
	if st.target != nil {
		return st.target, func() {}, nil
	}
	fs, err := openRestoreFS(filepath.Dir(dest))
	if err != nil {
		return nil, nil, err
	}
	return fs, func() { _ = fs.root.Close() }, nil
}
func validRestoreName(name string) error {
	if name == "." || name == ".." || !filepath.IsLocal(name) || strings.ContainsAny(name, "/\x00") || (runtime.GOOS == "windows" && strings.ContainsAny(name, "\\:")) {
		return fmt.Errorf("unsafe archive filename %q", name)
	}
	return nil
}
