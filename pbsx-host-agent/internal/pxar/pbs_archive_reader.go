package pxar

import (
	"context"
	"fmt"
	"io"

	pxarlib "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/transfer"
	"github.com/pbs-plus/pxar/vfs"
)

// PBSArchiveReader exposes one PBS snapshot's root PXAR directly through the
// reader protocol. It is cross-platform and never requires the PBS datastore
// to be mounted locally on the protected host.
type PBSArchiveReader struct {
	reader *backupproxy.PBSReader
	fs     *vfs.LocalFS
}

func NewPBSArchiveReader(ctx context.Context, cfg backupproxy.PBSConfig, hostID string, backupTime int64) (*PBSArchiveReader, error) {
	reader := backupproxy.NewPBSReader(cfg, "host", hostID, backupTime)
	if err := reader.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect PBS reader: %w", err)
	}
	closeOnErr := func(err error) (*PBSArchiveReader, error) {
		_ = reader.Close()
		return nil, err
	}

	meta, metaErr := reader.DownloadFile("root.mpxar.didx")
	if metaErr == nil {
		payload, err := reader.DownloadFile("root.ppxar.didx")
		if err != nil {
			return closeOnErr(fmt.Errorf("download PXAR payload index: %w", err))
		}
		archive, err := transfer.NewSplitReader(meta, payload, reader.AsChunkSource())
		if err != nil {
			return closeOnErr(fmt.Errorf("open split PXAR: %w", err))
		}
		return &PBSArchiveReader{reader: reader, fs: vfs.NewLocalFS(archive).SetMaxCache(4096)}, nil
	}

	legacy, err := reader.DownloadFile("root.pxar.didx")
	if err != nil {
		return closeOnErr(fmt.Errorf("snapshot has no readable root PXAR (split: %v; legacy: %w)", metaErr, err))
	}
	archive, err := transfer.NewChunkedReader(legacy, reader.AsChunkSource())
	if err != nil {
		return closeOnErr(fmt.Errorf("open legacy PXAR: %w", err))
	}
	return &PBSArchiveReader{reader: reader, fs: vfs.NewLocalFS(archive).SetMaxCache(4096)}, nil
}

func (r *PBSArchiveReader) Close() error {
	var first error
	if r.fs != nil {
		if err := r.fs.Close(); err != nil {
			first = err
		}
	}
	if r.reader != nil {
		if err := r.reader.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
func (r *PBSArchiveReader) LookupByPath(_ context.Context, path string) (*pxarlib.FileInfo, error) {
	return r.fs.Lookup(path)
}
func (r *PBSArchiveReader) ReadDir(_ context.Context, offset uint64) ([]pxarlib.FileInfo, error) {
	return r.fs.ReadDir(offset)
}
func (r *PBSArchiveReader) ReadLink(_ context.Context, start, _ uint64) ([]byte, error) {
	return r.fs.ReadLink(start)
}
func (r *PBSArchiveReader) ListXAttrs(_ context.Context, start, _ uint64) (map[string][]byte, error) {
	return r.fs.ListXAttrs(start)
}
func (r *PBSArchiveReader) ReadFileContentReader(_ context.Context, start, end uint64) (io.ReadCloser, error) {
	return r.fs.ReadContentReader(start, end)
}
