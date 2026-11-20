package mobilecore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
)

// ResumeJournal must atomically encrypt and durably save the complete descriptor
// before returning. It contains secrets and must never be written to history/logs.
type ResumeJournal interface{ SaveJSON(value string) error }

type backupRun struct {
	Version   int                        `json:"version"`
	Token     string                     `json:"token"`
	StateKey  string                     `json:"state_key"`
	Binding   string                     `json:"binding"`
	Source    string                     `json:"source"`
	Store     string                     `json:"store"`
	Namespace string                     `json:"namespace"`
	BackupID  string                     `json:"backup_id"`
	JobID     string                     `json:"job_id"`
	JobName   string                     `json:"job_name"`
	Stamp     int64                      `json:"stamp"`
	Prepared  uint64                     `json:"prepared"`
	Confirmed uint64                     `json:"confirmed"`
	Files     uint64                     `json:"files"`
	Bytes     uint64                     `json:"bytes"`
	Uploaded  uint64                     `json:"uploaded"`
	Candidate []datastore.BackupFileInfo `json:"candidate,omitempty"`
}

func hex32(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && value == strings.ToLower(value)
}
func validJobLabel(id, name string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(id) && len(name) > 0 && len(name) <= 400 && !strings.ContainsAny(name, "\r\n")
}
func parseBackupRun(raw string) (*backupRun, error) {
	if len(raw) > 32768 {
		return nil, errors.New("saved backup descriptor is too large")
	}
	var run backupRun
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&run); err != nil {
		return nil, errors.New("invalid saved backup descriptor")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing saved backup data")
	}
	if run.Version != 1 || !hex32(run.Token) || !hex32(run.StateKey) || !hex32(run.Binding) || !hex32(run.Source) || !validJobLabel(run.JobID, run.JobName) || run.Stamp <= 0 || run.Prepared < run.Confirmed || run.Prepared-run.Confirmed > 1 || validateGroup("host", run.BackupID) != nil || !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`).MatchString(run.Store) || (run.Namespace != "" && safePath(run.Namespace) != nil) || run.Bytes > 1<<63-1 || run.Files > 1<<63-1 || run.Uploaded > 1<<63-1 {
		return nil, errors.New("invalid saved backup identity or checkpoint")
	}
	if run.Candidate != nil && !validCandidate(run.Candidate) {
		return nil, errors.New("invalid final snapshot candidate")
	}
	return &run, nil
}

// ValidateBackupRun validates a descriptor without connecting or exposing secrets.
func ValidateBackupRun(raw string) error { _, err := parseBackupRun(raw); return err }

func validCandidate(files []datastore.BackupFileInfo) bool {
	if len(files) != 2 {
		return false
	}
	seen := map[string]bool{}
	for _, f := range files {
		if (f.Filename != "root.mpxar.didx" && f.Filename != "root.ppxar.didx") || seen[f.Filename] || !hex32(f.CSum) || (f.CryptMode != "none" && f.CryptMode != "encrypt") {
			return false
		}
		seen[f.Filename] = true
	}
	return true
}
func (c *Client) runBinding(run *backupRun) string {
	id, secret := c.cfg.AuthID, c.cfg.Secret
	if c.cfg.StorageAuthID != "" {
		id, secret = c.cfg.StorageAuthID, c.cfg.StorageSecret
	}
	key := ""
	if c.crypt != nil {
		key = datastore.FormatFingerprint(c.crypt.Fingerprint())
	}
	raw, _ := json.Marshal([]any{"pbsx-android-run-v1", c.cfg.Server, strings.ToLower(strings.ReplaceAll(c.cfg.Fingerprint, ":", "")), id, secret, key, run.Token, run.StateKey, run.Source, run.Store, run.Namespace, run.BackupID, run.JobID, run.JobName, run.Stamp})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (c *Client) checkedRun(raw, source string) (*backupRun, error) {
	run, err := parseBackupRun(raw)
	if err != nil {
		return nil, err
	}
	if run.Source != source || c.runBinding(run) != run.Binding {
		return nil, errors.New("saved backup belongs to different source, credentials, encryption key or destination; restore its settings or cancel it")
	}
	return run, nil
}

// NewBackupRun allocates the original snapshot identity once. Save its result
// durably before preparing exports or starting BackupGroupResumable.
func (c *Client) NewBackupRun(store, namespace, backupID, jobID, jobName, sourceIdentity string) (string, error) {
	if _, err := c.pbsConfig(store, namespace); err != nil {
		return "", err
	}
	if validateGroup("host", backupID) != nil || !validJobLabel(jobID, jobName) || !hex32(sourceIdentity) {
		return "", errors.New("invalid backup job/source identity")
	}
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	snapshots, err := c.runSnapshots(ctx, store, namespace, backupID)
	if err != nil {
		return "", err
	}
	stamp := time.Now().Unix()
	for _, item := range snapshots {
		if item.Time >= stamp {
			stamp = item.Time + 1
		}
	}
	if stamp > time.Now().Unix()+30 {
		return "", errors.New("backup group timestamp is ahead of this device's clock")
	}
	random := make([]byte, 64)
	if _, err = rand.Read(random); err != nil {
		return "", err
	}
	run := backupRun{Version: 1, Token: hex.EncodeToString(random[:32]), StateKey: hex.EncodeToString(random[32:]), Source: sourceIdentity, Store: store, Namespace: namespace, BackupID: backupID, JobID: jobID, JobName: jobName, Stamp: stamp}
	run.Binding = c.runBinding(&run)
	raw, err := json.Marshal(run)
	return string(raw), err
}

type runSnapshot struct {
	Type string  `json:"backup-type"`
	ID   string  `json:"backup-id"`
	Time int64   `json:"backup-time"`
	Size *uint64 `json:"size"`
}

func (c *Client) runSnapshots(ctx context.Context, store, namespace, id string) ([]runSnapshot, error) {
	q := url.Values{"backup-type": {"host"}, "backup-id": {id}}
	if namespace != "" {
		q.Set("ns", namespace)
	}
	raw, err := c.request(ctx, "GET", "/admin/datastore/"+url.PathEscape(store)+"/snapshots?"+q.Encode(), nil, true)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []runSnapshot `json:"data"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	for _, item := range result.Data {
		if item.Type != "host" || item.ID != id {
			return nil, errors.New("server returned a different backup group")
		}
	}
	return result.Data, nil
}
func (c *Client) runPresence(ctx context.Context, run *backupRun) (exists, complete bool, err error) {
	snapshots, err := c.runSnapshots(ctx, run.Store, run.Namespace, run.BackupID)
	if err != nil {
		return false, false, err
	}
	for _, item := range snapshots {
		if item.Time == run.Stamp {
			return true, item.Size != nil, nil
		}
	}
	return false, false, nil
}
func (c *Client) verifyRun(ctx context.Context, run *backupRun) (bool, error) {
	_, complete, err := c.runPresence(ctx, run)
	if err != nil || !complete {
		return false, err
	}
	if !validCandidate(run.Candidate) {
		return false, errors.New("snapshot is complete but its final candidate was not saved; review it before clearing this run")
	}
	cfg, err := c.pbsConfig(run.Store, run.Namespace)
	if err != nil {
		return false, err
	}
	reader := backupproxy.NewPBSReader(cfg, "host", run.BackupID, run.Stamp)
	if err = reader.Connect(ctx); err != nil {
		return false, err
	}
	defer reader.Close()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			reader.Close()
		case <-stopped:
		}
	}()
	raw, err := reader.DownloadFile("index.json.blob")
	if err != nil {
		return false, err
	}
	manifest, _, err := c.snapshotManifest(raw, "host", run.BackupID, run.Stamp)
	if err != nil {
		return false, err
	}
	if len(manifest.Files) != len(run.Candidate) {
		return false, errors.New("completed snapshot differs from the saved final candidate")
	}
	for _, wanted := range run.Candidate {
		found := false
		for _, got := range manifest.Files {
			if got == wanted {
				found = true
			}
		}
		if !found {
			return false, errors.New("completed snapshot differs from the saved final candidate")
		}
	}
	if _, err = datastore.LoadRootIndexes(reader, manifest); err != nil {
		return false, err
	}
	return true, nil
}
func runResult(run *backupRun, paused bool, warning string) (string, error) {
	raw, err := json.Marshal(map[string]any{"snapshot": fmt.Sprintf("host/%s/%s", run.BackupID, time.Unix(run.Stamp, 0).UTC().Format("2006-01-02T15:04:05Z")), "paused": paused, "warning": warning})
	return string(raw), err
}

// CompletedBackupRun checks an uncertain final response before Android opens
// source folders or regenerates exports. Empty means the run remains unfinished.
func (c *Client) CompletedBackupRun(raw, source string) (string, error) {
	run, err := c.checkedRun(raw, source)
	if err != nil {
		return "", err
	}
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	complete, err := c.verifyRun(ctx, run)
	if err != nil || !complete {
		return "", err
	}
	return runResult(run, false, "")
}

// Pause requests a checkpoint first. A stalled read/connection is interrupted
// after five seconds; recovery then uses the last acknowledged checkpoint.
func (c *Client) Pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel == nil || !c.resumable || c.control != 0 {
		return
	}
	c.control = 1
	operation := c.operation
	time.AfterFunc(5*time.Second, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.operation == operation && c.control == 1 && c.cancel != nil {
			c.cancel()
		}
	})
}
func (c *Client) pauseRequested() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.control == 1 }

func (c *Client) BackupGroupResumable(raw, source string, docs Documents, journal ResumeJournal, progress Progress) (string, error) {
	run, err := c.checkedRun(raw, source)
	if err != nil {
		return "", err
	}
	if docs == nil || journal == nil {
		return "", errors.New("backup requires documents and a durable journal")
	}
	ctx, done, err := c.beginMode(true)
	if err != nil {
		return "", err
	}
	defer done()
	complete, err := c.verifyRun(ctx, run)
	if err != nil {
		return "", err
	}
	if complete {
		return runResult(run, false, "")
	}
	cfg, err := c.pbsConfig(run.Store, run.Namespace)
	if err != nil {
		return "", err
	}
	previous, previousErr := c.previousForBackup(ctx, run.Store, run.Namespace, run.BackupID, run.Stamp)
	report := newTransferReporter(ctx, c.details)
	defer report.close()
	save := func() error {
		raw, err := json.Marshal(run)
		if err != nil {
			return err
		}
		return journal.SaveJSON(string(raw))
	}
	mode := datastore.CryptModeNone
	if c.crypt != nil {
		mode = datastore.CryptModeEncrypt
	}
	key, _ := hex.DecodeString(run.StateKey)
	store := backupproxy.NewPBSStore(cfg, buzhash.DefaultConfig(), true)
	result, err := store.RunResumableDocumentBackup(ctx, backupproxy.BackupConfig{ResumeToken: run.Token, PreviousBackup: previous, CryptMode: mode, CryptConfig: c.crypt, BackupType: datastore.BackupHost, BackupID: run.BackupID, Namespace: run.Namespace, BackupTime: run.Stamp, ChunkConfig: buzhash.DefaultConfig(), DetectionMode: backupproxy.DetectionData, Compress: true, StrictMetadata: true, OnUploadProgress: func(p backupproxy.UploadProgress) {
		report.uploaded(int64(p.UploadedBytes))
		if progress != nil {
			progress.Update("Uploading", int64(p.UploadedChunks), int64(p.UploadedBytes))
		}
	}}, backupproxy.NewLocalClient(&documentFS{docs: docs, ctx: ctx}), backupproxy.DocumentResumeOptions{
		StateKey: key, Binding: run.Binding, ConfirmedSequence: run.Confirmed,
		PrepareCheckpoint: func(sequence uint64) error { run.Prepared = sequence; return save() },
		CommitCheckpoint: func(sequence uint64, p backupproxy.DocumentProgress, uploaded uint64) error {
			if sequence > run.Prepared {
				return errors.New("server checkpoint was not locally prepared")
			}
			run.Confirmed = sequence
			run.Prepared = sequence
			run.Files, run.Bytes, run.Uploaded = p.Files, p.Bytes, uploaded
			return save()
		},
		PrepareFinish: func(files []datastore.BackupFileInfo) error {
			if !validCandidate(files) {
				return errors.New("unexpected final backup files")
			}
			run.Candidate = append([]datastore.BackupFileInfo(nil), files...)
			return save()
		},
		PauseRequested: c.pauseRequested,
		Progress: func(p backupproxy.DocumentProgress) {
			if report != nil {
				report.mu.Lock()
				report.state.Stage = p.Stage
				report.state.Path = p.Path
				report.state.Files = int64(p.Files)
				report.state.Bytes = int64(p.Bytes)
				report.state.FileBytes = int64(p.FileBytes)
				report.state.FileTotal = p.FileTotal
				report.mu.Unlock()
			}
		},
	})
	if err != nil {
		if c.pauseRequested() {
			return runResult(run, true, "The backup is paused; retry will check and resume its saved checkpoint")
		}
		var upgrade *backupproxy.ProtocolUpgradeError
		if errors.As(err, &upgrade) && upgrade.Status == 400 && strings.Contains(upgrade.Message, "pbsx-resume") {
			return "", errors.New("update the PBS-X server to a version supporting resumable Android backups")
		}
		return "", backupFailure(err, c.cfg.HostID, run.Store, run.Namespace)
	}
	if result.Paused {
		return runResult(run, true, result.Warning)
	}
	warning := ""
	if previousErr != nil {
		warning = "Could not inspect previous snapshot; deduplication used only this run's known chunks"
	}
	if err = c.saveJobNotes(ctx, run.Store, run.Namespace, run.BackupID, run.Stamp, run.JobID, run.JobName); err != nil {
		if warning != "" {
			warning += "; "
		}
		warning += "Snapshot saved; job label could not be attached"
	}
	return runResult(run, false, warning)
}

// DiscardBackupRun never deletes a completed snapshot. It confirms server cleanup
// before the caller may erase the local descriptor and its recovery secret.
func (c *Client) DiscardBackupRun(raw, source string) error {
	run, err := c.checkedRun(raw, source)
	if err != nil {
		return err
	}
	ctx, done, err := c.begin()
	if err != nil {
		return err
	}
	defer done()
	exists, complete, err := c.runPresence(ctx, run)
	if err != nil {
		return err
	}
	if complete {
		return errors.New("backup already completed; verify it before clearing this run")
	}
	if !exists {
		return nil
	}
	cfg, err := c.pbsConfig(run.Store, run.Namespace)
	if err != nil {
		return err
	}
	store := backupproxy.NewPBSStore(cfg, buzhash.DefaultConfig(), true)
	if err = store.DiscardDocumentBackup(ctx, backupproxy.BackupConfig{ResumeToken: run.Token, BackupType: datastore.BackupHost, BackupID: run.BackupID, Namespace: run.Namespace, BackupTime: run.Stamp}); err != nil {
		return err
	}
	for attempts := 0; attempts < 30; attempts++ {
		exists, complete, err = c.runPresence(ctx, run)
		if err != nil {
			return err
		}
		if complete {
			return errors.New("backup completed while cancelling; keep its descriptor for verification")
		}
		if !exists {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("server cleanup is still pending; keep this backup's recovery record and retry Cancel")
}
