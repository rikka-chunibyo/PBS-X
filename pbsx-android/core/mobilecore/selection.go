package mobilecore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/vfs"
)

type restoreStats struct {
	Files   int64 `json:"files"`
	Bytes   int64 `json:"bytes"`
	Skipped int64 `json:"skipped"`
	Renamed int64 `json:"renamed"`
}
type destinationEntry struct {
	Exists    bool  `json:"exists"`
	Directory bool  `json:"directory"`
	Size      int64 `json:"size"`
}

func selections(raw string) ([]string, error) {
	if len(raw) > 512<<10 {
		return nil, errors.New("restore selection is too large")
	}
	var paths []string
	if json.Unmarshal([]byte(raw), &paths) != nil || len(paths) == 0 || len(paths) > 1000 {
		return nil, errors.New("select 1–1000 files or folders")
	}
	for _, p := range paths {
		if err := safePath(p); err != nil {
			return nil, err
		}
	}
	sort.Strings(paths)
	out := []string{}
	for _, p := range paths {
		covered := false
		for _, parent := range out {
			if parent == "." || p == parent || strings.HasPrefix(p, parent+"/") {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, p)
		}
	}
	return out, nil
}

// RestoreSelected preserves selected archive paths under the supplied target.
// Modes: fail (fresh folder), skip (existing files untouched), replace (only
// differing files overwritten), rename (conflicting incoming files keep both).
func (c *Client) RestoreSelected(store, namespace, source string, stamp int64, selectionJSON, mode string, docs Documents, progress Progress) (string, error) {
	paths, err := selections(selectionJSON)
	if err != nil {
		return "", err
	}
	if mode != "fail" && mode != "skip" && mode != "replace" && mode != "rename" {
		return "", errors.New("invalid restore mode")
	}
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	snapshot, err := c.openSnapshot(ctx, store, namespace, source, stamp)
	if err != nil {
		return "", err
	}
	defer snapshot.close()
	stats, err := restoreSelected(ctx, snapshot.fs, paths, mode, docs, progress)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(stats)
	return string(raw), err
}

type selectionRestore struct {
	ctx         context.Context
	fs          vfs.FileSystem
	docs        Documents
	mode        string
	progress    Progress
	stats       restoreStats
	directories map[string]string
}

func restoreSelected(ctx context.Context, fs vfs.FileSystem, paths []string, mode string, docs Documents, progress Progress) (restoreStats, error) {
	// Validate the complete selected metadata before touching destination files.
	count := 0
	for _, p := range paths {
		if err := validateRestore(ctx, fs, p, 0, &count); err != nil {
			return restoreStats{}, err
		}
	}
	r := &selectionRestore{ctx: ctx, fs: fs, docs: docs, mode: mode, progress: progress, directories: map[string]string{".": "."}}
	for _, p := range paths {
		if err := r.walk(p, 0); err != nil {
			return r.stats, err
		}
	}
	return r.stats, nil
}
func validateRestore(ctx context.Context, fs vfs.FileSystem, p string, depth int, count *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 128 {
		return errors.New("restore nesting exceeds 128")
	}
	*count++
	if *count > 1000000 {
		return errors.New("selection exceeds one million entries")
	}
	e, err := lookup(fs, p)
	if err != nil {
		return err
	}
	if !supported(e) {
		return fmt.Errorf("Android cannot restore special entry %s; select regular files or folders", p)
	}
	if e.IsDir() {
		entries, err := directoryEntries(fs, e)
		if err != nil {
			return err
		}
		for i := range entries {
			if err := validateRestore(ctx, fs, path.Join(p, entries[i].Name()), depth+1, count); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r *selectionRestore) inspect(p string) (destinationEntry, error) {
	if err := safePath(p); err != nil {
		return destinationEntry{}, err
	}
	raw, err := r.docs.InspectJSON(p)
	if err != nil {
		return destinationEntry{}, err
	}
	if len(raw) > 16384 {
		return destinationEntry{}, errors.New("oversized destination metadata")
	}
	var e struct {
		Exists    *bool  `json:"exists"`
		Directory *bool  `json:"directory"`
		Size      *int64 `json:"size"`
	}
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return destinationEntry{}, err
	}
	if e.Exists == nil {
		return destinationEntry{}, errors.New("destination provider did not report whether the path exists")
	}
	if !*e.Exists {
		return destinationEntry{}, nil
	}
	if e.Directory == nil {
		return destinationEntry{}, errors.New("destination provider did not report a file type")
	}
	size := int64(-1)
	if e.Size != nil {
		size = *e.Size
	}
	return destinationEntry{Exists: true, Directory: *e.Directory, Size: size}, nil
}
func (r *selectionRestore) unused(p string) (string, error) {
	ext := path.Ext(p)
	if path.Base(p) == ext {
		ext = ""
	}
	stem := strings.TrimSuffix(path.Base(p), ext)
	parent := path.Dir(p)
	for i := 1; i <= 10000; i++ {
		candidate := path.Join(parent, fmt.Sprintf("%s (restored %d)%s", stem, i, ext))
		e, err := r.inspect(candidate)
		if err != nil {
			return "", err
		}
		if !e.Exists {
			r.stats.Renamed++
			return candidate, nil
		}
	}
	return "", errors.New("could not allocate a unique restore name")
}

// Directory mappings preserve hierarchy for individual-file selections, and
// permit rename mode to work around file/directory type conflicts safely.
func (r *selectionRestore) directory(archivePath string) (string, error) {
	if p, ok := r.directories[archivePath]; ok {
		return p, nil
	}
	if err := r.ctx.Err(); err != nil {
		return "", err
	}
	parent, err := r.directory(path.Dir(archivePath))
	if err != nil || parent == "" {
		return "", err
	}
	target := path.Join(parent, path.Base(archivePath))
	e, err := r.inspect(target)
	if err != nil {
		return "", err
	}
	if e.Exists && !e.Directory {
		switch r.mode {
		case "skip":
			r.stats.Skipped++
			r.directories[archivePath] = ""
			return "", nil
		case "rename":
			target, err = r.unused(target)
			if err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("destination %s is a file, but the backup needs a folder; choose Skip or Rename", target)
		}
	}
	if !e.Exists || !e.Directory {
		if err := r.docs.MakeDirectory(target); err != nil {
			return "", err
		}
	}
	r.directories[archivePath] = target
	return target, nil
}
func (r *selectionRestore) walk(p string, depth int) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if depth > 128 {
		return errors.New("restore nesting exceeds 128")
	}
	e, err := lookup(r.fs, p)
	if err != nil {
		return err
	}
	if e.IsDir() {
		target, err := r.directory(p)
		if err != nil || target == "" {
			return err
		}
		entries, err := directoryEntries(r.fs, e)
		if err != nil {
			return err
		}
		for i := range entries {
			if err := r.walk(path.Join(p, entries[i].Name()), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	parent, err := r.directory(path.Dir(p))
	if err != nil || parent == "" {
		return err
	}
	target := path.Join(parent, path.Base(p))
	existing, err := r.inspect(target)
	if err != nil {
		return err
	}
	replace := false
	if existing.Exists {
		switch r.mode {
		case "skip":
			r.stats.Skipped++
			return nil
		case "rename":
			target, err = r.unused(target)
			if err != nil {
				return err
			}
		case "replace":
			if existing.Directory {
				return fmt.Errorf("destination %s is a folder; Replace does not delete existing folders—choose Skip or Rename", target)
			}
			same, err := r.identical(p, target, e, existing)
			if err != nil {
				return err
			}
			if same {
				r.stats.Skipped++
				return nil
			}
			replace = true
		default:
			return fmt.Errorf("restore refused to overwrite existing document: %s", target)
		}
	}
	// Lookup again before opening: a large directory may have evicted this entry
	// from the bounded pxar content cache during preflight or sibling traversal.
	e, err = lookup(r.fs, p)
	if err != nil {
		return err
	}
	content, err := r.fs.ReadContentReader(e.ContentRange[0], e.ContentRange[1])
	if err != nil {
		return err
	}
	defer content.Close()
	var fd int64
	if replace {
		fd, err = r.docs.OpenReplace(target)
	} else {
		fd, err = r.docs.OpenWrite(target)
	}
	if err != nil {
		return err
	}
	out, err := documentFile(fd, target)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(r.ctx, func() { out.Close() })
	n, copyErr := io.Copy(out, &cancelReader{ctx: r.ctx, reader: content})
	stop()
	closeErr := out.Close()
	if copyErr == nil && uint64(n) != e.RawSize {
		copyErr = errors.New("restored file size does not match archive metadata")
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		return fmt.Errorf("restore %s: %w", target, err)
	}
	r.stats.Files++
	r.stats.Bytes += n
	if r.progress != nil {
		r.progress.Update("Restoring", r.stats.Files, r.stats.Bytes)
	}
	return nil
}
func (r *selectionRestore) identical(source, target string, e *pxar.FileInfo, existing destinationEntry) (bool, error) {
	if existing.Size >= 0 && uint64(existing.Size) != e.RawSize {
		return false, nil
	}
	fd, err := r.docs.OpenRead(target)
	if err != nil {
		return false, err
	}
	file, err := documentFile(fd, target)
	if err != nil {
		return false, err
	}
	defer file.Close()
	stop := context.AfterFunc(r.ctx, func() { file.Close() })
	defer stop()
	existingHash := sha256.New()
	n, err := io.Copy(existingHash, &cancelReader{ctx: r.ctx, reader: file})
	if err != nil {
		return false, err
	}
	if uint64(n) != e.RawSize {
		return false, nil
	}
	current, err := lookup(r.fs, source)
	if err != nil {
		return false, err
	}
	content, err := r.fs.ReadContentReader(current.ContentRange[0], current.ContentRange[1])
	if err != nil {
		return false, err
	}
	defer content.Close()
	archiveHash := sha256.New()
	if _, err := io.Copy(archiveHash, &cancelReader{ctx: r.ctx, reader: content}); err != nil {
		return false, err
	}
	return string(existingHash.Sum(nil)) == string(archiveHash.Sum(nil)), nil
}
