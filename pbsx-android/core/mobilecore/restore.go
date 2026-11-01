package mobilecore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"

	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/transfer"
	"github.com/pbs-plus/pxar/vfs"
)

func (c *Client) Restore(store, namespace, sourceHost string, stamp int64, docs Documents, progress Progress) error {
	cfg, err := c.pbsConfig(store, namespace)
	if err != nil {
		return err
	}
	if sourceHost == "" {
		sourceHost = c.cfg.HostID
	}
	if sourceHost == "" || safePath(sourceHost) != nil || stamp <= 0 {
		return errors.New("invalid snapshot")
	}
	ctx, done, err := c.begin()
	if err != nil {
		return err
	}
	defer done()
	reader := backupproxy.NewPBSReader(cfg, "host", sourceHost, stamp)
	if err = reader.Connect(ctx); err != nil {
		return err
	}
	defer reader.Close()
	// PBS archive reads are synchronous; close the connection on cancellation.
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			reader.Close()
		case <-stopped:
		}
	}()
	meta, metaErr := reader.DownloadFile("root.mpxar.didx")
	var archive transfer.ArchiveReader
	if metaErr == nil {
		payload, e := reader.DownloadFile("root.ppxar.didx")
		if e != nil {
			return e
		}
		archive, err = transfer.NewSplitReader(meta, payload, reader.AsChunkSource())
	} else {
		legacy, e := reader.DownloadFile("root.pxar.didx")
		if e != nil {
			return fmt.Errorf("no readable root archive: %w", metaErr)
		}
		archive, err = transfer.NewChunkedReader(legacy, reader.AsChunkSource())
	}
	if err != nil {
		return err
	}
	fs := vfs.NewLocalFS(archive).SetMaxCache(4096)
	defer fs.Close()
	root, err := fs.Root()
	if err != nil {
		return err
	}
	var files, bytes int64
	return restoreDirectory(ctx, fs, root, ".", docs, progress, &files, &bytes, 0)
}

func restoreDirectory(ctx context.Context, fs vfs.FileSystem, dir *pxar.FileInfo, relative string, docs Documents, progress Progress, files, bytes *int64, depth int) error {
	if depth > 128 {
		return errors.New("restore nesting exceeds 128")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := fs.ReadDir(dir.EntryRangeEnd)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for i := range entries {
		e := &entries[i]
		name := e.Name()
		if name == "." || safePath(name) != nil || path.Base(name) != name || seen[name] {
			return errors.New("unsafe or duplicate archive entry")
		}
		seen[name] = true
		p := path.Join(relative, name)
		if err := safePath(p); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir() {
			if err := docs.MakeDirectory(p); err != nil {
				return err
			}
			if err := restoreDirectory(ctx, fs, e, p, docs, progress, files, bytes, depth+1); err != nil {
				return err
			}
			continue
		}
		if !e.IsFile() {
			return fmt.Errorf("restore cannot represent special entry %s on Android", p)
		}
		if len(e.ContentRange) != 2 {
			return fmt.Errorf("missing payload for %s", p)
		}
		content, err := fs.ReadContentReader(e.ContentRange[0], e.ContentRange[1])
		if err != nil {
			return err
		}
		fd, err := docs.OpenWrite(p)
		if err != nil {
			content.Close()
			return err
		}
		out, fileErr := documentFile(fd, p)
		if fileErr != nil {
			content.Close()
			return fileErr
		}
		stopWrite := context.AfterFunc(ctx, func() { out.Close() })
		n, copyErr := io.Copy(out, &cancelReader{ctx: ctx, reader: content})
		stopWrite()
		if copyErr == nil && uint64(n) != e.RawSize {
			copyErr = errors.New("restored file size does not match archive metadata")
		}
		closeErr := out.Close()
		readErr := content.Close()
		if err := errors.Join(copyErr, closeErr, readErr); err != nil {
			return fmt.Errorf("restore %s: %w", p, err)
		}
		*files++
		*bytes += n
		if progress != nil {
			progress.Update("Restoring", *files, *bytes)
		}
	}
	return nil
}

type cancelReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
