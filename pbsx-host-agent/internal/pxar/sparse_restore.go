package pxar

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
)

// sparseRestoreWriter creates holes only from zero bytes actually received
// from the verified archive. A saved hole map is a hint: direct backups can
// capture it before an application fills a hole with new data.
type sparseRestoreWriter struct {
	file   *os.File
	holes  []sparseHole
	offset uint64
	next   int
}

func (w *sparseRestoreWriter) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		for w.next < len(w.holes) && w.offset >= w.holes[w.next].Offset+w.holes[w.next].Length {
			w.next++
		}
		count := len(data)
		inHole := false
		if w.next < len(w.holes) {
			hole := w.holes[w.next]
			if w.offset < hole.Offset {
				count = int(min(uint64(count), hole.Offset-w.offset))
			} else {
				inHole = true
				count = int(min(uint64(count), hole.Offset+hole.Length-w.offset))
			}
		}
		part := data[:count]
		var n int
		var err error
		if inHole && bytes.Count(part, []byte{0}) == len(part) {
			_, err = w.file.Seek(int64(count), io.SeekCurrent)
			if err == nil {
				n = count
			}
		} else {
			n, err = w.file.Write(part)
		}
		written += n
		w.offset += uint64(n)
		if err != nil {
			return written, err
		}
		if n != count {
			return written, io.ErrShortWrite
		}
		data = data[count:]
	}
	return written, nil
}

func restoreContentWriter(ctx context.Context, st *restoreState, file *os.File, job restoreJob) (io.Writer, error) {
	if st.noAttr {
		return file, nil
	}
	attrs, err := st.client.ListXAttrs(ctx, job.info.EntryRangeStart, job.info.EntryRangeEnd)
	if err != nil {
		return nil, fmt.Errorf("read sparse layout for %q: %w", job.dest, err)
	}
	holes, _, err := decodeSparseHoles(attrs, job.info.RawSize)
	if err != nil {
		return nil, fmt.Errorf("decode sparse layout for %q: %w", job.dest, err)
	}
	if len(holes) == 0 {
		return file, nil
	}
	enabled, err := enableSparseFile(file)
	if err != nil {
		return nil, fmt.Errorf("prepare sparse file for %q: %w", job.dest, err)
	}
	if !enabled {
		return file, nil
	}
	return &sparseRestoreWriter{file: file, holes: holes}, nil
}
