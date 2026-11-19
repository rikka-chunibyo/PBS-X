package encoder

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"

	"github.com/pbs-plus/pxar/format"
)

const maxCheckpointSize = 32 << 20

type directoryCheckpoint struct {
	Items           []format.GoodbyeItem `json:"items"`
	Entry           uint64               `json:"entry"`
	Position        uint64               `json:"position"`
	Payload         uint64               `json:"payload"`
	PreviousPayload uint64               `json:"previous_payload"`
	HasPrevious     bool                 `json:"has_previous"`
	Parent          int                  `json:"parent"`
}

type fileCheckpoint struct {
	Item      format.GoodbyeItem `json:"item"`
	Remaining uint64             `json:"remaining"`
	Size      uint64             `json:"size"`
}

type encoderCheckpoint struct {
	Version     int                   `json:"version"`
	Format      format.FormatVersion  `json:"format"`
	Directories []directoryCheckpoint `json:"directories"`
	File        *fileCheckpoint       `json:"file,omitempty"`
}

func (e *Encoder) ready() error {
	if e.err != nil {
		return e.err
	}
	if e.finished || e.activeFile != nil {
		return errors.New("encoder is finished or a file is still open")
	}
	return nil
}

// Checkpoint serializes a quiescent encoder, including an open file's remaining
// length. It does not flush or persist output. The caller must first durably save
// the exact metadata/payload prefixes and pair this state with those prefixes.
// The encoder must not be used concurrently with this method. No backup client
// may advertise resumability on the strength of encoder state alone.
func (e *Encoder) Checkpoint() ([]byte, error) {
	if e.err != nil {
		return nil, e.err
	}
	if e.finished {
		return nil, errors.New("cannot checkpoint a finished encoder")
	}
	c := encoderCheckpoint{Version: 1, Format: e.version}
	for _, s := range e.state {
		c.Directories = append(c.Directories, directoryCheckpoint{
			Items: s.items, Entry: s.entryOffset, Position: s.writePosition,
			Payload: s.payloadWritePos, PreviousPayload: s.previousPayloadOffset,
			HasPrevious: s.hasPrevPayloadOffset, Parent: s.parentItemIdx,
		})
	}
	if fw := e.activeFile; fw != nil {
		c.File = &fileCheckpoint{fw.goodbyeItem, fw.remaining, fw.fileSize}
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(c)
	if len(raw) > maxCheckpointSize {
		return nil, errors.New("encoder checkpoint exceeds 32 MiB")
	}
	return raw, err
}

func (c *encoderCheckpoint) validate() error {
	bad := errors.New("invalid encoder checkpoint")
	if c.Version != 1 || (c.Format != format.FormatVersion1 && c.Format != format.FormatVersion2) || len(c.Directories) == 0 || len(c.Directories) > 129 {
		return bad
	}
	items := 0
	for i, s := range c.Directories {
		items += len(s.Items)
		if items > 350000 || s.Position < 56 || s.Position > math.MaxInt64 || s.Entry > s.Position || s.Payload > math.MaxInt64 {
			return bad
		}
		if c.Format == format.FormatVersion1 {
			if s.Payload != 0 || s.HasPrevious || s.PreviousPayload != 0 {
				return bad
			}
		} else if s.Payload < format.HeaderSize || s.PreviousPayload > s.Payload || (!s.HasPrevious && s.PreviousPayload != 0) {
			return bad
		}
		for _, item := range s.Items {
			if item.Offset >= s.Position || item.Size > math.MaxInt64 || item.Offset > math.MaxUint64-item.Size {
				return bad
			}
		}
		if i == 0 {
			if s.Parent != -1 || s.Entry != 0 {
				return bad
			}
		} else {
			parent := c.Directories[i-1]
			if s.Parent < 0 || s.Parent != len(parent.Items)-1 || parent.Items[s.Parent].Size != 0 || s.Entry < parent.Items[s.Parent].Offset || parent.Position > s.Position || parent.Payload > s.Payload {
				return bad
			}
		}
	}
	if f := c.File; f != nil {
		s := c.Directories[len(c.Directories)-1]
		if f.Remaining > f.Size || f.Size > math.MaxInt64 || f.Item.Size != 0 || f.Item.Offset >= s.Position {
			return bad
		}
		if c.Format == format.FormatVersion2 && f.Size-f.Remaining > s.Payload {
			return bad
		}
		if c.Format == format.FormatVersion1 && f.Size-f.Remaining > s.Position-f.Item.Offset {
			return bad
		}
	}
	return nil
}

// ResumeEncoder reconstructs an encoder without writing headers or reading any
// earlier source files. Outputs must append immediately after the saved prefixes.
// The second result is non-nil when a file was open; seek its source to
// FileProgress before continuing it. Checkpoint storage/authentication and server
// acknowledgment are responsibilities of the resumable backup coordinator.
func ResumeEncoder(output, payload io.Writer, raw []byte) (*Encoder, *FileWriter, error) {
	if len(raw) > maxCheckpointSize || output == nil {
		return nil, nil, errors.New("invalid encoder checkpoint output or size")
	}
	var c encoderCheckpoint
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, nil, errors.New("trailing encoder checkpoint data")
	}
	if err := c.validate(); err != nil {
		return nil, nil, err
	}
	if (payload != nil) != (c.Format == format.FormatVersion2) {
		return nil, nil, errors.New("encoder checkpoint stream format differs")
	}
	e := &Encoder{output: output, payloadOut: payload, version: c.Format}
	for _, s := range c.Directories {
		e.state = append(e.state, encoderState{
			items: s.Items, entryOffset: s.Entry, writePosition: s.Position,
			payloadWritePos: s.Payload, previousPayloadOffset: s.PreviousPayload,
			hasPrevPayloadOffset: s.HasPrevious, parentItemIdx: s.Parent,
		})
	}
	if f := c.File; f != nil {
		e.activeFile = &FileWriter{enc: e, goodbyeItem: f.Item, remaining: f.Remaining, fileSize: f.Size}
	}
	return e, e.activeFile, nil
}

// FileProgress is the number of source bytes already encoded for this file.
func (fw *FileWriter) FileProgress() uint64 { return fw.fileSize - fw.remaining }

// MetadataPosition is the prefix length associated with Checkpoint.
func (e *Encoder) MetadataPosition() uint64 {
	if len(e.state) == 0 {
		return 0
	}
	return e.currentState().writePosition
}
