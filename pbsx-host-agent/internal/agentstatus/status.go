// Package agentstatus publishes a deliberately non-secret local status snapshot.
// It is shared by the service, desktop companion and headless CLI.
package agentstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Operation struct {
	Kind           string `json:"kind"`
	Stage          string `json:"stage"`
	Files          uint64 `json:"files"`
	ProcessedBytes uint64 `json:"processed_bytes"`
	UploadedBytes  uint64 `json:"uploaded_bytes"`
}

// Snapshot never contains credentials, endpoint URLs, source filenames or raw
// error messages. Ordinary local users can read this limited status information.
type Snapshot struct {
	Version       string     `json:"version"`
	Phase         string     `json:"phase"`
	Connected     bool       `json:"connected"`
	StartedAt     int64      `json:"started_at"`
	UpdatedAt     int64      `json:"updated_at"`
	LastHeartbeat int64      `json:"last_heartbeat"`
	LastSuccess   *bool      `json:"last_success,omitempty"`
	Operation     *Operation `json:"operation,omitempty"`
}

func (s Snapshot) Running(now time.Time) bool {
	return s.Phase != "stopped" && s.UpdatedAt > 0 && now.Unix()-s.UpdatedAt >= -5 && now.Unix()-s.UpdatedAt <= 15
}

var mu sync.Mutex
var state Snapshot

func Phase(value string) { mu.Lock(); state.Phase = value; mu.Unlock() }

func Heartbeat(ok bool) {
	mu.Lock()
	defer mu.Unlock()
	state.Connected = ok
	if ok {
		state.LastHeartbeat = time.Now().Unix()
	}
}

func Progress(value Operation) {
	mu.Lock()
	defer mu.Unlock()
	state.Operation = &value
}

func Finished(ok bool) {
	mu.Lock()
	defer mu.Unlock()
	state.Operation = nil
	state.LastSuccess = &ok
}

func current() Snapshot {
	mu.Lock()
	defer mu.Unlock()
	result := state
	result.UpdatedAt = time.Now().Unix()
	return result
}

func Publish(ctx context.Context, version string) <-chan error {
	result := make(chan error, 1)
	mu.Lock()
	state = Snapshot{Version: version, Phase: "starting", StartedAt: time.Now().Unix()}
	mu.Unlock()
	go func() {
		defer close(result)
		var reported bool
		write := func() {
			data, err := json.Marshal(current())
			if err == nil {
				err = writeStatus(data)
			}
			if err != nil && !reported {
				result <- fmt.Errorf("publish desktop status: %w", err)
				reported = true
			}
		}
		write()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				mu.Lock()
				state.Phase = "stopped"
				state.Connected = false
				state.Operation = nil
				mu.Unlock()
				write()
				return
			case <-ticker.C:
				write()
			}
		}
	}()
	return result
}

func Read() (Snapshot, error) {
	data, err := readStatus()
	if err != nil {
		return Snapshot{}, err
	}
	if len(data) > 16<<10 {
		return Snapshot{}, fmt.Errorf("agent status exceeds size limit")
	}
	var value Snapshot
	if err := json.Unmarshal(data, &value); err != nil {
		return Snapshot{}, fmt.Errorf("invalid local agent status")
	}
	return value, nil
}
