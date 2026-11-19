package backupproxy

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"math"

	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
)

// StreamCheckpoint identifies an acknowledged dynamic-index prefix. It belongs
// inside the authenticated continuation envelope, alongside the encoder/walker
// state. It is not evidence that a complete snapshot exists.
type StreamCheckpoint struct {
	Name     string `json:"name"`
	Size     uint64 `json:"size"`
	Count    int    `json:"count"`
	Checksum string `json:"checksum"`
}

// checkpointStream chunks writes on the producer side so checkpoint boundaries
// can flush partial chunks and acknowledge every reference before saving walker
// state. Legacy UploadArchive uses a reader-side chunker and remains unchanged.
// The resumable document coordinator uses this writer; the ordinary host
// archive uploader retains its existing path.
type checkpointStream struct {
	ctx      context.Context
	session  *pbsSession
	wid      uint64
	scanner  *buzhash.Scanner
	buffer   []byte
	checksum hash.Hash
	state    StreamCheckpoint
	failed   error
	closed   bool
}

// restoredIndex may only come from the authenticated server's resume operation,
// which must have reopened this writer AND registered its prefix chunks. An
// arbitrary reader download is not sufficient registration. expected must be
// taken from the authenticated saved client continuation, not supplied by a UI.
func newCheckpointStream(ctx context.Context, s *pbsSession, name string, restoredWID uint64, restoredIndex []byte, expected *StreamCheckpoint) (*checkpointStream, error) {
	if name != "root.mpxar.didx" && name != "root.ppxar.didx" {
		return nil, errors.New("unsupported continuation stream")
	}
	w := &checkpointStream{ctx: ctx, session: s, checksum: sha256.New(), scanner: buzhash.NewScanner(s.chunkCfg), state: StreamCheckpoint{Name: name}}
	validConfig, configErr := buzhash.NewConfig(s.chunkCfg.AvgChunkSize)
	if configErr != nil || validConfig != s.chunkCfg || s.chunkCfg.MinChunkSize < buzhash.WindowSize || s.chunkCfg.MaxChunkSize > 16<<20 {
		return nil, errors.New("invalid continuation chunk size")
	}
	if expected != nil {
		if restoredWID == 0 || len(restoredIndex) > 64<<20 {
			return nil, errors.New("invalid restored continuation index")
		}
		idx, err := datastore.ParseDynamicIndex(restoredIndex)
		if err != nil {
			return nil, err
		}
		var previous uint64
		for i := 0; i < idx.Count(); i++ {
			entry := idx.Entry(i)
			if entry.EndOffset <= previous || entry.EndOffset > math.MaxInt64 || entry.EndOffset-previous > uint64(s.chunkCfg.MaxChunkSize) {
				return nil, errors.New("invalid continuation chunk range")
			}
			w.addDigest(entry.EndOffset, entry.Digest)
			previous = entry.EndOffset
		}
		sum := w.checksum.Sum(nil)
		if !equalDigest(sum, idx.IndexCsum()) {
			return nil, errors.New("restored continuation index checksum mismatch")
		}
		w.state.Size = previous
		w.state.Count = idx.Count()
		w.state.Checksum = hex.EncodeToString(sum)
		if w.state != *expected {
			return nil, errors.New("server prefix differs from saved continuation")
		}
		w.wid = restoredWID
		// Server recovery has registered these exact references. Do not seed any
		// other chunks from unregistered checkpoint indexes or filesystem probes.
		s.mu.Lock()
		if s.knownChunks == nil {
			s.knownChunks = make(map[[32]byte]bool)
		}
		for i := 0; i < idx.Count(); i++ {
			s.knownChunks[idx.Entry(i).Digest] = true
		}
		s.mu.Unlock()
	} else {
		if restoredWID != 0 || len(restoredIndex) != 0 {
			return nil, errors.New("unexpected restored continuation index")
		}
		var err error
		w.wid, err = s.proto.dynamicIndexCreate(name)
		if err != nil {
			return nil, err
		}
	}
	s.loadPrevious(name)
	return w, nil
}

func equalDigest(raw []byte, want [32]byte) bool {
	if len(raw) != len(want) {
		return false
	}
	var difference byte
	for i, b := range raw {
		difference |= b ^ want[i]
	}
	return difference == 0
}

func (w *checkpointStream) addDigest(end uint64, digest [32]byte) {
	var entry [40]byte
	binary.LittleEndian.PutUint64(entry[:8], end)
	copy(entry[8:], digest[:])
	w.checksum.Write(entry[:])
}

func (w *checkpointStream) flush() error {
	if w.failed != nil {
		return w.failed
	}
	if err := w.ctx.Err(); err != nil {
		w.failed = err
		return err
	}
	if len(w.buffer) == 0 {
		return nil
	}
	if w.state.Size > math.MaxInt64-uint64(len(w.buffer)) {
		w.failed = errors.New("continuation stream exceeds maximum size")
		return w.failed
	}
	digest := chunkDigest(w.buffer, w.session.config.CryptConfig)
	hexDigest := hex.EncodeToString(digest[:])
	w.session.mu.Lock()
	known := w.session.knownChunks[digest]
	w.session.mu.Unlock()
	var uploaded uint64
	if !known {
		blob, bp, err := borrowChunkBlob(w.buffer, w.session.compress, w.session.config.CryptConfig)
		if err != nil {
			w.failed = err
			return err
		}
		uploaded = uint64(len(blob))
		err = w.session.proto.dynamicChunkUpload(w.wid, hexDigest, len(w.buffer), len(blob), blob)
		datastore.PutBlobBuf(bp)
		if err != nil {
			w.failed = err
			return err
		}
	}
	if err := w.session.proto.dynamicIndexAppend(w.wid, []string{hexDigest}, []uint64{w.state.Size}); err != nil {
		w.failed = err
		return err
	}
	w.session.mu.Lock()
	w.session.knownChunks[digest] = true
	w.session.mu.Unlock()
	size := uint64(len(w.buffer))
	w.state.Size += size
	w.state.Count++
	w.addDigest(w.state.Size, digest)
	w.buffer = w.buffer[:0]
	w.scanner.Reset()
	w.session.reportProgress(size, uploaded)
	return nil
}

func (w *checkpointStream) Write(p []byte) (n int, err error) {
	if w.closed {
		return 0, errors.New("continuation stream is closed")
	}
	if w.failed != nil {
		return 0, w.failed
	}
	for len(p) > 0 {
		boundary := w.scanner.Scan(p)
		if boundary == 0 {
			w.buffer = append(w.buffer, p...)
			n += len(p)
			return n, nil
		}
		w.buffer = append(w.buffer, p[:boundary]...)
		n += boundary
		p = p[boundary:]
		if err = w.flush(); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Checkpoint returns only after all source bytes accepted by Write have been
// uploaded (or deduplicated) and appended to the server index. The coordinator
// must still persist the server's checkpoint and authenticated continuation.
func (w *checkpointStream) Checkpoint() (StreamCheckpoint, error) {
	if w.closed {
		return StreamCheckpoint{}, errors.New("continuation stream is closed")
	}
	if err := w.flush(); err != nil {
		return StreamCheckpoint{}, err
	}
	w.state.Checksum = hex.EncodeToString(w.checksum.Sum(nil))
	return w.state, nil
}

func (w *checkpointStream) Finish() (*UploadResult, error) {
	state, err := w.Checkpoint()
	if err != nil {
		return nil, err
	}
	if err = w.session.proto.dynamicIndexClose(w.wid, state.Count, state.Size, state.Checksum); err != nil {
		w.failed = err
		return nil, err
	}
	w.closed = true
	var digest [32]byte
	copy(digest[:], w.checksum.Sum(nil))
	w.session.addFileInfo(state.Name, state.Size, digest)
	return &UploadResult{Filename: state.Name, Size: state.Size, Digest: digest}, nil
}
