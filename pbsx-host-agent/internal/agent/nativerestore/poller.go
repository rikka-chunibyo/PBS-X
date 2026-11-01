package nativerestore

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/workgate"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
)

type runCommand struct {
	RunID       string   `json:"run-id"`
	HostID      string   `json:"host-id"`
	BackupID    string   `json:"backup-id,omitempty"`
	Store       string   `json:"store"`
	Namespace   string   `json:"ns,omitempty"`
	BackupTime  int64    `json:"backup-time"`
	Sources     []string `json:"sources"`
	Destination string   `json:"destination"`
	Mode        string   `json:"mode"`
	RequestedAt int64    `json:"requested-at"`
}

type runFinish struct {
	HostID  string `json:"host-id"`
	RunID   string `json:"run-id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type runProgress struct {
	HostID         string `json:"host-id"`
	RunID          string `json:"run-id"`
	Stage          string `json:"stage,omitempty"`
	ProcessedBytes uint64 `json:"processed-bytes,omitempty"`
	Files          uint64 `json:"files,omitempty"`
}

func postProgress(ctx context.Context, hostID, runID, stage string, files, processedBytes uint64) {
	progress := runProgress{
		HostID: hostID, RunID: runID, Stage: stage, Files: files, ProcessedBytes: processedBytes,
	}
	if err := agent.PBSAPIRequest(ctx, http.MethodPost, "/pbsx/agent/restore-progress", progress, nil); err != nil && ctx.Err() == nil {
		log.Warn("failed to report native restore progress", "run_id", runID, "error", err.Error())
	}
}

func Poll(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	for {
		if err := pollOnce(ctx); err != nil && ctx.Err() == nil {
			log.Warn("native restore poll failed", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func pollOnce(ctx context.Context) error {
	workgate.Mu.Lock()
	defer workgate.Mu.Unlock()

	hostID, err := agent.AgentHostID()
	if err != nil {
		return err
	}
	var runs []runCommand
	path := "/pbsx/agent/restore-next?host-id=" + url.QueryEscape(hostID)
	if err := agent.PBSAPIRequest(ctx, http.MethodGet, path, nil, &runs); err != nil {
		return err
	}
	if len(runs) == 0 {
		return nil
	}
	run := runs[0]
	if run.HostID != hostID {
		return fmt.Errorf("server returned restore %q for host %q to host %q", run.RunID, run.HostID, hostID)
	}

	log.Info("claimed native host restore", "run_id", run.RunID, "store", run.Store, "backup_time", run.BackupTime, "destination", run.Destination)
	postProgress(ctx, hostID, run.RunID, "opening", 0, 0)

	var files atomic.Uint64
	var processedBytes atomic.Uint64
	progressCtx, stopProgress := context.WithCancel(ctx)
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-progressCtx.Done():
				return
			case <-ticker.C:
				postProgress(progressCtx, hostID, run.RunID, "restoring", files.Load(), processedBytes.Load())
			}
		}
	}()

	runErr := Run(ctx, Job{
		ID: run.RunID, HostID: run.HostID, BackupID: run.BackupID, Datastore: run.Store,
		Namespace: run.Namespace, BackupTime: run.BackupTime,
		Sources: run.Sources, Destination: run.Destination, Mode: run.Mode,
		OnProgress: func(fileCount, byteCount uint64) {
			files.Store(fileCount)
			processedBytes.Store(byteCount)
		},
	})
	stopProgress()
	<-progressDone
	postProgress(ctx, hostID, run.RunID, "finishing", files.Load(), processedBytes.Load())
	if ctx.Err() != nil {
		return ctx.Err()
	}

	finish := runFinish{HostID: hostID, RunID: run.RunID, Success: runErr == nil}
	if runErr != nil {
		finish.Error = truncateError(runErr.Error(), 8192)
	}
	for {
		if err := agent.PBSAPIRequest(ctx, http.MethodPost, "/pbsx/agent/restore-finish", finish, nil); err == nil {
			if runErr != nil {
				log.Error(runErr, "native restore completed with failure", "run_id", run.RunID)
			} else {
				log.Info("native restore acknowledged", "run_id", run.RunID)
			}
			return nil
		} else {
			log.Warn("failed to report native restore completion; retrying", "run_id", run.RunID, "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func truncateError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
