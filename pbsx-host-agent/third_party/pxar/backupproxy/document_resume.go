package backupproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
)

type DocumentResumeOptions struct {
	StateKey          []byte
	Binding           string
	ConfirmedSequence uint64
	// All journal callbacks must durably commit before returning success.
	PrepareCheckpoint  func(sequence uint64) error
	CommitCheckpoint   func(sequence uint64, progress DocumentProgress, uploaded uint64) error
	PrepareFinish      func(files []datastore.BackupFileInfo) error
	PauseRequested     func() bool
	Progress           func(DocumentProgress)
	CheckpointBytes    uint64
	CheckpointInterval time.Duration
}

type DocumentResumeResult struct {
	Paused   bool
	Sequence uint64
	Warning  string
	Manifest *datastore.Manifest
}

type resumeStatus struct {
	Sequence      uint64 `json:"sequence"`
	StateChecksum string `json:"state_checksum"`
	Streams       []struct {
		WID uint64 `json:"wid"`
		StreamCheckpoint
	} `json:"streams"`
}

func getResumeStatus(protocol *h2Protocol) (resumeStatus, error) {
	var status resumeStatus
	raw, err := protocol.conn.do("GET", "resume", nil, nil, "")
	if err != nil {
		return status, err
	}
	if err = json.Unmarshal(raw, &status); err != nil {
		return status, err
	}
	if status.Sequence == 0 {
		if len(status.Streams) != 0 {
			return status, errors.New("unexpected initial continuation streams")
		}
	} else if len(status.Streams) != 2 {
		return status, errors.New("invalid continuation stream count")
	}
	return status, nil
}

// RunResumableDocumentBackup resumes the original BackupTime/ResumeToken, never
// substituting a new snapshot after failure. Its caller owns the durable run
// descriptor, source/configuration binding, cancellation policy and verification
// of an uncertain final response against PrepareFinish's saved manifest candidate.
func (ps *PBSStore) RunResumableDocumentBackup(ctx context.Context, cfg BackupConfig, source ClientProvider, options DocumentResumeOptions) (*DocumentResumeResult, error) {
	if cfg.ResumeToken == "" || cfg.DiscardResume || len(options.StateKey) != 32 || len(options.Binding) != 64 || options.PrepareCheckpoint == nil || options.CommitCheckpoint == nil || options.PrepareFinish == nil {
		return nil, errors.New("resumable backup requires its durable run/key/identity journal")
	}
	if options.CheckpointBytes == 0 {
		options.CheckpointBytes = 64 << 20
	}
	if options.CheckpointInterval == 0 {
		options.CheckpointInterval = 30 * time.Second
	}
	var uploadedBase uint64
	if callback := cfg.OnUploadProgress; callback != nil {
		cfg.OnUploadProgress = func(progress UploadProgress) { progress.UploadedBytes += uploadedBase; callback(progress) }
	}
	baseSession, err := ps.StartSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := baseSession.(*pbsSession)
	defer s.Close()
	protocol := s.proto.(*h2Protocol)
	status, err := getResumeStatus(protocol)
	if err != nil {
		return nil, err
	}
	if status.Sequence < options.ConfirmedSequence {
		return nil, errors.New("the saved backup checkpoint is missing or older; refusing to restart this run")
	}
	var envelope documentResumeEnvelope
	if status.Sequence > 0 {
		sealed, err := protocol.conn.doRaw("GET", "resume_download", url.Values{"name": {"state"}})
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(sealed)
		if hex.EncodeToString(sum[:]) != status.StateChecksum {
			return nil, errors.New("server continuation state checksum mismatch")
		}
		envelope, err = openResumeEnvelope(options.StateKey, options.Binding, status.Sequence, sealed)
		if err != nil {
			return nil, err
		}
		valid, err := buzhash.NewConfig(envelope.ChunkConfig.AvgChunkSize)
		if err != nil || valid != envelope.ChunkConfig {
			return nil, errors.New("invalid saved continuation chunk configuration")
		}
		s.chunkCfg = envelope.ChunkConfig
		uploadedBase = envelope.UploadedBytes
	}
	var streams [2]*checkpointStream
	for i, name := range []string{"root.mpxar.didx", "root.ppxar.didx"} {
		var wid uint64
		var prefix []byte
		var wanted *StreamCheckpoint
		if status.Sequence > 0 {
			for j := range status.Streams {
				if status.Streams[j].Name == name {
					wid = status.Streams[j].WID
				}
			}
			for j := range envelope.Streams {
				if envelope.Streams[j].Name == name {
					wanted = &envelope.Streams[j]
				}
			}
			if wid == 0 || wanted == nil {
				return nil, errors.New("saved continuation stream is missing")
			}
			prefix, err = protocol.conn.doRaw("GET", "resume_download", url.Values{"name": {name}})
			if err != nil {
				return nil, err
			}
		}
		streams[i], err = newCheckpointStream(ctx, s, name, wid, prefix, wanted)
		if err != nil {
			return nil, err
		}
	}
	var walk *DocumentContinuation
	if status.Sequence > 0 {
		walk, err = ResumeDocumentContinuation(source, streams[0], streams[1], envelope.Walk)
	} else {
		walk, err = NewDocumentContinuation(ctx, source, streams[0], streams[1])
	}
	if err != nil {
		return nil, err
	}
	defer walk.Close()
	walk.ProgressCallback = options.Progress
	metaPos, payloadPos := walk.OutputPositions()
	if status.Sequence > 0 && (metaPos != streams[0].state.Size || payloadPos != streams[1].state.Size) {
		return nil, errors.New("encoder output positions differ from acknowledged server prefixes")
	}
	if status.Sequence > 0 {
		if err = options.CommitCheckpoint(status.Sequence, walk.Progress(), uploadedBase); err != nil {
			return nil, err
		}
	}
	sequence := status.Sequence
	lastCheckpoint := time.Now()
	checkpointBytes := walk.Progress().Bytes
	save := func() error {
		state, err := walk.Checkpoint()
		if err != nil {
			return err
		}
		meta, err := streams[0].Checkpoint()
		if err != nil {
			return err
		}
		payload, err := streams[1].Checkpoint()
		if err != nil {
			return err
		}
		metadata, payloadBytes := walk.OutputPositions()
		if meta.Size != metadata || payload.Size != payloadBytes {
			return errors.New("stream positions changed during checkpoint")
		}
		next := sequence + 1
		if next == 0 {
			return errors.New("continuation sequence overflow")
		}
		if err = options.PrepareCheckpoint(next); err != nil {
			return err
		}
		s.progressMu.Lock()
		uploaded := uploadedBase + s.progress.UploadedBytes
		s.progressMu.Unlock()
		sealed, err := sealResumeEnvelope(options.StateKey, documentResumeEnvelope{Version: 1, Sequence: next, Binding: options.Binding, Streams: []StreamCheckpoint{meta, payload}, Walk: state, UploadedBytes: uploaded, ChunkConfig: s.chunkCfg})
		if err != nil {
			return err
		}
		params := url.Values{"sequence": {strconv.FormatUint(next, 10)}, "encoded-size": {strconv.Itoa(len(sealed))}}
		if _, err = protocol.conn.do("POST", "resume_state", params, sealed, "application/octet-stream"); err != nil {
			return err
		}
		expected, _ := json.Marshal([]StreamCheckpoint{meta, payload})
		if _, err = protocol.conn.do("POST", "resume", url.Values{"sequence": {strconv.FormatUint(next, 10)}, "streams": {string(expected)}}, nil, ""); err != nil {
			return err
		}
		sequence = next
		if err = options.CommitCheckpoint(sequence, walk.Progress(), uploaded); err != nil {
			return err
		}
		checkpointBytes = walk.Progress().Bytes
		lastCheckpoint = time.Now()
		return nil
	}
	// No user file is read before the first recoverable server checkpoint.
	if sequence == 0 {
		if err = save(); err != nil {
			return nil, err
		}
	}
	for {
		if options.Progress != nil {
			options.Progress(walk.Progress())
		}
		if options.PauseRequested != nil && options.PauseRequested() {
			warning := ""
			if err = save(); err != nil {
				warning = "The connection stopped before the latest checkpoint; the prior acknowledged checkpoint is preserved"
			}
			return &DocumentResumeResult{Paused: true, Sequence: sequence, Warning: warning}, nil
		}
		done, err := walk.Step(ctx)
		if err != nil {
			if options.PauseRequested != nil && options.PauseRequested() {
				return &DocumentResumeResult{Paused: true, Sequence: sequence, Warning: "The interrupted operation will continue from its last acknowledged checkpoint"}, nil
			}
			return nil, err
		}
		if done {
			break
		}
		if walk.Progress().Bytes-checkpointBytes >= options.CheckpointBytes || time.Since(lastCheckpoint) >= options.CheckpointInterval {
			if err = save(); err != nil {
				return nil, err
			}
		}
	}
	for _, stream := range streams {
		if _, err = stream.Finish(); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	files := append([]datastore.BackupFileInfo(nil), s.files...)
	s.mu.Unlock()
	if err = options.PrepareFinish(files); err != nil {
		return nil, err
	}
	manifest, err := s.Finish(ctx)
	if err != nil {
		return nil, err
	}
	return &DocumentResumeResult{Sequence: sequence, Manifest: manifest}, nil
}

// DiscardDocumentBackup requires the original auth/token and an unfinished
// snapshot. It can cancel even when the opaque saved state/index is corrupt,
// but cannot authorize deletion of a completed snapshot.
func (ps *PBSStore) DiscardDocumentBackup(ctx context.Context, cfg BackupConfig) error {
	if cfg.ResumeToken == "" {
		return errors.New("discard requires the original resume token")
	}
	cfg.DiscardResume = true
	session, err := ps.StartSession(ctx, cfg)
	if err != nil {
		var rejected *ProtocolUpgradeError
		if errors.As(err, &rejected) && rejected.Status == 404 {
			return nil
		}
		return err
	}
	// This opt-in session deliberately has no recovery/retention state. Closing
	// it runs ordinary unfinished-backup cleanup under the server's group lock.
	return session.Close()
}
