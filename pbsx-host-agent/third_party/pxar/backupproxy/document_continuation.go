package backupproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"path"
	"strings"

	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/encoder"
	"github.com/pbs-plus/pxar/format"
)

// DocumentContinuation is the resumable archive producer for Android's regular
// files and directories. It is deliberately separate from the legacy uploader:
// a coordinator must durably pair its checkpoint with acknowledged output on the
// server before this can be used for a resumable backup. It never finalizes a
// snapshot, and its existence alone does not make the current uploader resumable.
type DocumentContinuation struct {
	source           ClientProvider
	enc              *encoder.Encoder
	state            documentWalkCheckpoint
	fw               *encoder.FileWriter
	reader           io.ReadCloser
	prefix           hash.Hash
	finished         bool
	failed           error
	ProgressCallback func(DocumentProgress)
}

type DocumentProgress struct {
	Stage     string
	Path      string
	Files     uint64
	Bytes     uint64
	FileBytes uint64
	FileTotal int64
}

func (w *DocumentContinuation) Progress() DocumentProgress {
	p := DocumentProgress{Stage: "Scanning folders", Files: w.state.Files, Bytes: w.state.Bytes, FileTotal: -1}
	if f := w.state.File; f != nil {
		p.Stage = "Backing up"
		p.Path = f.Path
		p.FileBytes = f.Offset
		p.FileTotal = int64(f.Size)
	} else if len(w.state.Frames) > 0 {
		p.Path = w.state.Frames[len(w.state.Frames)-1].Path
	}
	return p
}

func (w *DocumentContinuation) OutputPositions() (uint64, uint64) {
	return w.enc.MetadataPosition(), w.enc.PayloadPosition()
}

type documentFrame struct {
	Path    string     `json:"path"`
	Entries []DirEntry `json:"entries"`
	Next    int        `json:"next"`
}

type documentFileCheckpoint struct {
	Path      string      `json:"path"`
	Size      uint64      `json:"size"`
	Stat      format.Stat `json:"stat"`
	Offset    uint64      `json:"offset"`
	HashState []byte      `json:"hash_state"`
	Digest    string      `json:"digest"`
}

type documentWalkCheckpoint struct {
	Version int                     `json:"version"`
	Encoder json.RawMessage         `json:"encoder"`
	Frames  []documentFrame         `json:"frames"`
	File    *documentFileCheckpoint `json:"file,omitempty"`
	Files   uint64                  `json:"files"`
	Bytes   uint64                  `json:"bytes"`
}

const maxDocumentCheckpoint = 48 << 20

func documentEntries(entries []DirEntry) error {
	if len(entries) > 40000 {
		return errors.New("document directory exceeds 40,000 entries")
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if format.CheckFilename([]byte(entry.Name)) != nil || strings.ContainsAny(entry.Name, "\\\x00") || entry.Size > math.MaxInt64 || seen[entry.Name] || (!entry.Stat.IsDir() && !entry.Stat.IsRegularFile()) {
			return errors.New("invalid or unsupported document directory entry")
		}
		seen[entry.Name] = true
	}
	return nil
}

func NewDocumentContinuation(ctx context.Context, source ClientProvider, metadata, payload io.Writer) (*DocumentContinuation, error) {
	if source == nil || metadata == nil {
		return nil, errors.New("document source and metadata output are required")
	}
	stat, err := source.Stat(ctx, ".")
	if err != nil {
		return nil, err
	}
	if !stat.IsDir() {
		return nil, errors.New("document backup root must be a directory")
	}
	entries, err := source.ReadDir(ctx, ".")
	if err != nil {
		return nil, err
	}
	if err = documentEntries(entries); err != nil {
		return nil, err
	}
	w := &DocumentContinuation{source: source, enc: encoder.NewEncoder(metadata, payload, &pxar.Metadata{Stat: stat}, nil), state: documentWalkCheckpoint{Version: 1, Frames: []documentFrame{{Path: ".", Entries: entries}}}}
	if _, err = w.enc.Checkpoint(); err != nil {
		return nil, err
	}
	return w, nil
}

// Checkpoint must be called between Step calls. The coordinator must first
// flush its streams and bind this state to their exact acknowledged lengths.
func (w *DocumentContinuation) Checkpoint() ([]byte, error) {
	if w.failed != nil {
		return nil, w.failed
	}
	if w.finished {
		return nil, errors.New("cannot checkpoint a finalized document archive")
	}
	var err error
	w.state.Encoder, err = w.enc.Checkpoint()
	if err != nil {
		return nil, err
	}
	if w.state.File != nil {
		w.state.File.HashState, err = w.prefix.(encoding.BinaryMarshaler).MarshalBinary()
		if err != nil {
			return nil, err
		}
		w.state.File.Digest = hex.EncodeToString(w.prefix.Sum(nil))
	}
	raw, err := json.Marshal(w.state)
	if len(raw) > maxDocumentCheckpoint {
		return nil, errors.New("document checkpoint exceeds 48 MiB")
	}
	return raw, err
}

// ResumeDocumentContinuation never reopens completed files or relists completed
// directories. For the interrupted file only, its saved prefix is rehashed to
// reject changes during suspension. Those bytes are not encoded/uploaded again.
// Checkpoint bytes must be authenticated by the coordinator before calling this.
func ResumeDocumentContinuation(source ClientProvider, metadata, payload io.Writer, raw []byte) (*DocumentContinuation, error) {
	if len(raw) > maxDocumentCheckpoint {
		return nil, errors.New("document checkpoint exceeds 48 MiB")
	}
	w := &DocumentContinuation{source: source}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&w.state); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("trailing document checkpoint data")
	}
	if w.state.Version != 1 || len(w.state.Frames) == 0 || len(w.state.Frames) > 129 {
		return nil, errors.New("invalid document checkpoint version or depth")
	}
	count := 0
	for i, frame := range w.state.Frames {
		count += len(frame.Entries)
		if count > 350000 || frame.Next < 0 || frame.Next > len(frame.Entries) {
			return nil, errors.New("invalid document checkpoint cursor")
		}
		if err := documentEntries(frame.Entries); err != nil {
			return nil, err
		}
		if i == 0 {
			if frame.Path != "." {
				return nil, errors.New("invalid document checkpoint root")
			}
		} else {
			parent := w.state.Frames[i-1]
			if parent.Next == 0 || !parent.Entries[parent.Next-1].Stat.IsDir() || frame.Path != path.Join(parent.Path, parent.Entries[parent.Next-1].Name) {
				return nil, errors.New("invalid document checkpoint directory stack")
			}
		}
	}
	var err error
	w.enc, w.fw, err = encoder.ResumeEncoder(metadata, payload, w.state.Encoder)
	if err != nil {
		return nil, err
	}
	if w.enc.DirectoryDepth() != len(w.state.Frames) {
		return nil, errors.New("document checkpoint and encoder directory depths disagree")
	}
	if (w.fw != nil) != (w.state.File != nil) {
		return nil, errors.New("document checkpoint file and encoder disagree")
	}
	if f := w.state.File; f != nil {
		frame := w.state.Frames[len(w.state.Frames)-1]
		if frame.Next == 0 || !frame.Entries[frame.Next-1].Stat.IsRegularFile() || f.Path != path.Join(frame.Path, frame.Entries[frame.Next-1].Name) || f.Stat != frame.Entries[frame.Next-1].Stat || f.Size != frame.Entries[frame.Next-1].Size || f.Size != w.fw.FileSize() || f.Size > math.MaxInt64 || f.Offset > f.Size || f.Offset != w.fw.FileProgress() || w.state.Bytes < f.Offset || len(f.HashState) > 256 {
			return nil, errors.New("invalid document checkpoint active file")
		}
		w.prefix = sha256.New()
		if err = w.prefix.(encoding.BinaryUnmarshaler).UnmarshalBinary(f.HashState); err != nil {
			return nil, err
		}
		if hex.EncodeToString(w.prefix.Sum(nil)) != f.Digest {
			return nil, errors.New("document checkpoint prefix hash disagrees")
		}
	}
	return w, nil
}

func (w *DocumentContinuation) metadata(ctx context.Context, name string, stat format.Stat) (*pxar.Metadata, error) {
	m := &pxar.Metadata{Stat: stat}
	var err error
	m.XAttrs, err = w.source.GetXAttrs(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("read document xattrs: %w", err)
	}
	m.ACL, err = w.source.GetACL(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("read document ACL: %w", err)
	}
	m.FCaps, err = w.source.GetFCaps(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("read document capabilities: %w", err)
	}
	return m, nil
}

func (w *DocumentContinuation) openCurrent(ctx context.Context) error {
	f := w.state.File
	report := func(validated uint64) {
		if w.ProgressCallback != nil {
			progress := w.Progress()
			progress.Stage = "Checking saved file"
			progress.FileBytes = validated
			progress.FileTotal = int64(f.Offset)
			w.ProgressCallback(progress)
		}
	}
	report(0)
	stat, err := w.source.Stat(ctx, f.Path)
	if err != nil {
		return err
	}
	if stat != f.Stat {
		return fmt.Errorf("interrupted source file %q changed; cancel this incomplete run before starting a new backup", f.Path)
	}
	r, size, err := w.source.OpenFile(ctx, f.Path)
	if err != nil {
		return err
	}
	if size != f.Size {
		r.Close()
		return errors.New("interrupted source file size changed")
	}
	if f.Offset > 0 {
		current := sha256.New()
		var validated uint64
		if _, err = io.CopyN(current, contextDocumentReader{ctx: ctx, reader: r, read: func(count int) { validated += uint64(count); report(validated) }}, int64(f.Offset)); err != nil {
			r.Close()
			return err
		}
		if hex.EncodeToString(current.Sum(nil)) != f.Digest {
			r.Close()
			return fmt.Errorf("interrupted source file %q contents changed", f.Path)
		}
	}
	w.reader = r
	return nil
}

type contextDocumentReader struct {
	ctx    context.Context
	reader io.Reader
	read   func(int)
}

func (r contextDocumentReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if n > 0 && r.read != nil {
		r.read(n)
	}
	return n, err
}

// Step processes at most one directory entry or 64 KiB of the current file.
// Returning done means the archive was finalized, not that a PBS snapshot exists.
// Errors require closing this producer and recovering from the last durable
// coordinated checkpoint; never continue writing after an output error.
func (w *DocumentContinuation) Step(ctx context.Context) (done bool, err error) {
	if w.failed != nil {
		return false, w.failed
	}
	defer func() {
		if err != nil {
			w.failed = err
		}
	}()
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if w.finished {
		return true, nil
	}
	if f := w.state.File; f != nil {
		if w.reader == nil {
			if err = w.openCurrent(ctx); err != nil {
				return false, err
			}
		}
		var buffer [64 << 10]byte
		remaining := f.Size - f.Offset
		if remaining > uint64(len(buffer)) {
			remaining = uint64(len(buffer))
		}
		if remaining > 0 {
			n, readErr := io.ReadFull(w.reader, buffer[:remaining])
			if readErr != nil {
				return false, fmt.Errorf("read interrupted document: %w", readErr)
			}
			if err = w.fw.WriteAll(buffer[:n]); err != nil {
				return false, err
			}
			w.prefix.Write(buffer[:n])
			f.Offset += uint64(n)
			w.state.Bytes += uint64(n)
			return false, nil
		}
		if n, probeErr := w.reader.Read(buffer[:1]); n != 0 || probeErr != io.EOF {
			return false, errors.New("source file changed or could not verify EOF")
		}
		err = w.reader.Close()
		w.reader = nil
		if err != nil {
			return false, err
		}
		if err = w.fw.Close(); err != nil {
			return false, err
		}
		w.fw = nil
		w.state.File = nil
		w.prefix = nil
		w.state.Files++
		return false, nil
	}
	frame := &w.state.Frames[len(w.state.Frames)-1]
	if frame.Next == len(frame.Entries) {
		if len(w.state.Frames) == 1 {
			if err = w.enc.Close(); err != nil {
				return false, err
			}
			w.finished = true
			return true, nil
		}
		if err = w.enc.Finish(); err != nil {
			return false, err
		}
		w.state.Frames = w.state.Frames[:len(w.state.Frames)-1]
		return false, nil
	}
	entry := frame.Entries[frame.Next]
	name := path.Join(frame.Path, entry.Name)
	meta, err := w.metadata(ctx, name, entry.Stat)
	if err != nil {
		return false, err
	}
	if entry.Stat.IsDir() {
		if len(w.state.Frames) >= 129 {
			return false, errors.New("document directory depth exceeds 128")
		}
		entries, err := w.source.ReadDir(ctx, name)
		if err != nil {
			return false, err
		}
		if err = documentEntries(entries); err != nil {
			return false, err
		}
		if err = w.enc.CreateDirectory(entry.Name, meta); err != nil {
			return false, err
		}
		frame.Next++
		w.state.Frames = append(w.state.Frames, documentFrame{Path: name, Entries: entries})
	} else {
		stat, err := w.source.Stat(ctx, name)
		if err != nil {
			return false, err
		}
		if stat != entry.Stat {
			return false, errors.New("source file changed after directory listing")
		}
		r, size, err := w.source.OpenFile(ctx, name)
		if err != nil {
			return false, err
		}
		if size != entry.Size {
			r.Close()
			return false, errors.New("source size changed after directory listing")
		}
		fw, err := w.enc.CreateFile(meta, entry.Name, size)
		if err != nil {
			r.Close()
			return false, err
		}
		frame.Next++
		w.reader = r
		w.fw = fw
		w.prefix = sha256.New()
		w.state.File = &documentFileCheckpoint{Path: name, Size: size, Stat: stat}
	}
	return false, nil
}

// Close releases only the source handle. It never closes/finalizes the archive.
// A partially consumed source can report an incomplete-read error on Close;
// saved checkpoint validity is determined before closing the source.
func (w *DocumentContinuation) Close() error {
	if w.reader == nil {
		return nil
	}
	err := w.reader.Close()
	w.reader = nil
	return err
}
