package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/transfer"
	"github.com/pbs-plus/pxar/vfs"
)

type openSnapshot struct {
	fs      *vfs.LocalFS
	reader  *backupproxy.PBSReader
	stopped chan struct{}
}

func (s *openSnapshot) close() { close(s.stopped); s.fs.Close(); s.reader.Close() }

func (c *Client) openSnapshot(ctx context.Context, store, namespace, source string, stamp int64) (*openSnapshot, error) {
	cfg, err := c.pbsConfig(store, namespace)
	if err != nil {
		return nil, err
	}
	if source == "" {
		source = c.cfg.HostID
	}
	if source == "." || safePath(source) != nil || strings.Contains(source, "/") || stamp <= 0 {
		return nil, errors.New("invalid snapshot")
	}
	reader := backupproxy.NewPBSReader(cfg, "host", source, stamp)
	if err = reader.Connect(ctx); err != nil {
		return nil, err
	}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			reader.Close()
		case <-stopped:
		}
	}()
	fail := func(err error) (*openSnapshot, error) { close(stopped); reader.Close(); return nil, err }
	meta, metaErr := reader.DownloadFile("root.mpxar.didx")
	var archive transfer.ArchiveReader
	if metaErr == nil {
		payload, e := reader.DownloadFile("root.ppxar.didx")
		if e != nil {
			return fail(e)
		}
		archive, err = transfer.NewSplitReader(meta, payload, reader.AsChunkSource())
	} else {
		legacy, e := reader.DownloadFile("root.pxar.didx")
		if e != nil {
			return fail(fmt.Errorf("no readable root archive: %w", metaErr))
		}
		archive, err = transfer.NewChunkedReader(legacy, reader.AsChunkSource())
	}
	if err != nil {
		return fail(err)
	}
	return &openSnapshot{fs: vfs.NewLocalFS(archive).SetMaxCache(4096), reader: reader, stopped: stopped}, nil
}

func lookup(fs vfs.FileSystem, p string) (*pxar.FileInfo, error) {
	if p == "." {
		return fs.Root()
	}
	return fs.Lookup("/" + p)
}

type archiveItem struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Size      uint64 `json:"size"`
	Modified  int64  `json:"modified"`
	Supported bool   `json:"supported"`
}

func supported(e *pxar.FileInfo) bool { return e.IsDir() || (e.IsFile() && len(e.ContentRange) == 2) }
func directoryEntries(fs vfs.FileSystem, dir *pxar.FileInfo) ([]pxar.FileInfo, error) {
	entries, err := fs.ReadDir(dir.EntryRangeEnd)
	if err != nil {
		return nil, err
	}
	if len(entries) > 100000 {
		return nil, errors.New("snapshot directory exceeds 100,000 entries")
	}
	seen := map[string]bool{}
	for i := range entries {
		name := entries[i].Name()
		if name == "." || safePath(name) != nil || path.Base(name) != name || seen[name] {
			return nil, errors.New("unsafe or duplicate archive entry")
		}
		seen[name] = true
	}
	return entries, nil
}

// ListArchive returns one page of a snapshot directory; payloads are not fetched.
func (c *Client) ListArchive(store, namespace, source string, stamp int64, relative string, offset, limit int64) (string, error) {
	if err := safePath(relative); err != nil {
		return "", err
	}
	if offset < 0 || limit < 1 || limit > 200 {
		return "", errors.New("invalid directory page")
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
	return listArchivePage(snapshot.fs, relative, offset, limit)
}
func listArchivePage(fs vfs.FileSystem, relative string, offset, limit int64) (string, error) {
	dir, err := lookup(fs, relative)
	if err != nil {
		return "", err
	}
	if !dir.IsDir() {
		return "", errors.New("selected path is not a directory")
	}
	entries, err := directoryEntries(fs, dir)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})
	total := int64(len(entries))
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]archiveItem, 0, end-offset)
	for i := offset; i < end; i++ {
		e := &entries[i]
		out = append(out, archiveItem{Name: e.Name(), Path: path.Join(relative, e.Name()), Directory: e.IsDir(), Size: e.RawSize, Modified: e.ModTime().UnixMilli(), Supported: supported(e)})
	}
	raw, err := json.Marshal(map[string]any{"path": relative, "total": total, "entries": out})
	return string(raw), err
}
