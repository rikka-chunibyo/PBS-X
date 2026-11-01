package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// ProgressDetails receives live logical I/O progress, including deduplicated
// files. The current-file bar is separate from actual network upload bytes.
type ProgressDetails interface{ UpdateJSON(value string) }

type transferState struct {
	Stage         string `json:"stage"`
	Path          string `json:"path"`
	Files         int64  `json:"files"`
	Bytes         int64  `json:"bytes"`
	FileBytes     int64  `json:"file_bytes"`
	FileTotal     int64  `json:"file_total"`
	UploadedBytes int64  `json:"uploaded_bytes"`
}

type transferReporter struct {
	mu     sync.Mutex
	state  transferState
	sink   ProgressDetails
	cancel context.CancelFunc
	done   chan struct{}
}

func newTransferReporter(ctx context.Context, sink ProgressDetails) *transferReporter {
	if sink == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &transferReporter{sink: sink, cancel: cancel, done: make(chan struct{}), state: transferState{Stage: "Connecting to PBS", FileTotal: -1}}
	r.send()
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.send()
			}
		}
	}()
	return r
}
func (r *transferReporter) close() {
	if r != nil {
		r.cancel()
		<-r.done
		r.send()
	}
}
func (r *transferReporter) send() {
	r.mu.Lock()
	state := r.state
	r.mu.Unlock()
	raw, _ := json.Marshal(state)
	r.sink.UpdateJSON(string(raw))
}
func (r *transferReporter) phase(stage, p string, total int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Stage = stage
	r.state.Path = p
	r.state.FileBytes = 0
	r.state.FileTotal = total
}
func (r *transferReporter) add(n int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Bytes += n
	r.state.FileBytes += n
}
func (r *transferReporter) fileDone() {
	if r != nil {
		r.mu.Lock()
		r.state.Files++
		r.mu.Unlock()
	}
}
func (r *transferReporter) uploaded(n int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if n > r.state.UploadedBytes {
		r.state.UploadedBytes = n
	}
	if r.state.FileTotal >= 0 && r.state.FileBytes >= r.state.FileTotal {
		r.state.Stage = "Uploading and saving snapshot"
		r.state.FileTotal = -1
	}
}

// SetProgressDetails attaches a listener before starting a backup or restore.
// Existing Progress callbacks retain their original upload-counter semantics.
func (c *Client) SetProgressDetails(sink ProgressDetails) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return errors.New("an operation is running")
	}
	c.details = sink
	return nil
}
