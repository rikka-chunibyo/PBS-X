package nativebackup

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/workgate"
	"github.com/pbs-x/pbsx-host-agent/internal/agentstatus"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
)

type runCommand struct {
	RunID               string   `json:"run-id"`
	JobID               string   `json:"job-id"`
	HostID              string   `json:"host-id"`
	Store               string   `json:"store"`
	Namespace           string   `json:"ns,omitempty"`
	Sources             []string `json:"sources"`
	Excludes            []string `json:"excludes,omitempty"`
	SnapshotMode        string   `json:"snapshot-mode"`
	ChangeDetectionMode string   `json:"change-detection-mode"`
	RequestedAt         int64    `json:"requested-at"`
	BackupTime          int64    `json:"backup-time"`
}

type runFinish struct {
	HostID       string  `json:"host-id"`
	RunID        string  `json:"run-id"`
	Success      bool    `json:"success"`
	Error        string  `json:"error,omitempty"`
	BackupTime   *int64  `json:"backup-time,omitempty"`
	Files        *uint64 `json:"files,omitempty"`
	Directories  *uint64 `json:"directories,omitempty"`
	LogicalBytes *uint64 `json:"logical-bytes,omitempty"`
	DurationMS   *uint64 `json:"duration-ms,omitempty"`
}

type runProgress struct {
	HostID          string `json:"host-id"`
	RunID           string `json:"run-id"`
	Stage           string `json:"stage,omitempty"`
	ProcessedBytes  uint64 `json:"processed-bytes,omitempty"`
	UploadedBytes   uint64 `json:"uploaded-bytes,omitempty"`
	ProcessedChunks uint64 `json:"processed-chunks,omitempty"`
	UploadedChunks  uint64 `json:"uploaded-chunks,omitempty"`
	Files           uint64 `json:"files,omitempty"`
}

type progressReporter struct {
	ctx       context.Context
	hostID    string
	runID     string
	mu        sync.Mutex
	lastSent  time.Time
	lastStage string
}

func (r *progressReporter) report(progress Progress) {
	agentstatus.Progress(agentstatus.Operation{Kind: "backup", Stage: progress.Stage, Files: progress.Files, ProcessedBytes: progress.ProcessedBytes, UploadedBytes: progress.UploadedBytes})
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	force := progress.Stage != r.lastStage
	if !force && !r.lastSent.IsZero() && now.Sub(r.lastSent) < 2*time.Second {
		return
	}
	payload := runProgress{
		HostID:          r.hostID,
		RunID:           r.runID,
		Stage:           progress.Stage,
		ProcessedBytes:  progress.ProcessedBytes,
		UploadedBytes:   progress.UploadedBytes,
		ProcessedChunks: progress.ProcessedChunks,
		UploadedChunks:  progress.UploadedChunks,
		Files:           progress.Files,
	}
	if err := agent.PBSAPIRequest(r.ctx, http.MethodPost, "/pbsx/agent/progress", payload, nil); err != nil {
		log.Debug("failed to report native backup progress", "run_id", r.runID, "error", err.Error())
		return
	}
	r.lastSent = now
	r.lastStage = progress.Stage
}

// Poll continuously claims server-owned backup runs and executes them one at a
// time. A run is not claimed again while this goroutine is executing it.
func Poll(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	for {
		if err := pollOnce(ctx); err != nil && ctx.Err() == nil {
			log.Warn("native backup poll failed", "error", err.Error())
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
	path := "/pbsx/agent/next?host-id=" + url.QueryEscape(hostID)
	if err := agent.PBSAPIRequest(ctx, http.MethodGet, path, nil, &runs); err != nil {
		return err
	}
	if len(runs) == 0 {
		return nil
	}
	run := runs[0]
	if run.HostID != hostID {
		return fmt.Errorf("server returned run %q for host %q to host %q", run.RunID, run.HostID, hostID)
	}
	log.Info("claimed native host backup", "run_id", run.RunID, "job", run.JobID, "requested_at", run.RequestedAt)

	agentstatus.Progress(agentstatus.Operation{Kind: "backup", Stage: "preparing"})
	reporter := &progressReporter{ctx: ctx, hostID: hostID, runID: run.RunID}
	result, runErr := Run(ctx, Job{
		ID:                  run.JobID,
		HostID:              run.HostID,
		Datastore:           run.Store,
		Namespace:           run.Namespace,
		Sources:             run.Sources,
		Excludes:            run.Excludes,
		SnapshotMode:        run.SnapshotMode,
		ChangeDetectionMode: run.ChangeDetectionMode,
		BackupTime:          run.BackupTime,
		Progress:            reporter.report,
	})
	if ctx.Err() != nil {
		// Leave the command in running/ so a restarted agent can recover it.
		return ctx.Err()
	}

	finish := runFinish{HostID: hostID, RunID: run.RunID, Success: runErr == nil}
	if runErr != nil {
		finish.Error = truncate(runErr.Error(), 8192)
	} else if result != nil {
		finish.BackupTime = &result.BackupTime
		files := uint64(max(result.Files, 0))
		dirs := uint64(max(result.Directories, 0))
		logical := uint64(max(result.LogicalBytes, 0))
		duration := uint64(max(result.Duration.Milliseconds(), 0))
		finish.Files = &files
		finish.Directories = &dirs
		finish.LogicalBytes = &logical
		finish.DurationMS = &duration
	}

	// Do not claim another run until completion is durably acknowledged. This
	// also handles a lost finish response without creating concurrent duplicates.
	for {
		err := agent.PBSAPIRequest(ctx, http.MethodPost, "/pbsx/agent/finish", finish, nil)
		if err == nil {
			agentstatus.Finished(runErr == nil)
			if runErr != nil {
				log.Error(runErr, "native backup run completed with failure", "run_id", run.RunID, "job", run.JobID)
			} else {
				log.Info("native backup run acknowledged", "run_id", run.RunID, "job", run.JobID)
			}
			return nil
		}
		log.Warn("failed to report native backup completion; retrying", "run_id", run.RunID, "error", err.Error())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
