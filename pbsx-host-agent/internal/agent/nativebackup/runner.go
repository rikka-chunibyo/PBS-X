package nativebackup

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/winpath"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
)

// Job is the agent-side form of a PBS-X host backup job. HostID is the PBS
// backup group identity and must match the identity enrolled on this machine.
type Job struct {
	ID                  string         `json:"id" cbor:"id"`
	HostID              string         `json:"host" cbor:"host"`
	Datastore           string         `json:"store" cbor:"store"`
	Namespace           string         `json:"ns,omitempty" cbor:"ns,omitempty"`
	Sources             []string       `json:"sources" cbor:"sources"`
	Excludes            []string       `json:"excludes,omitempty" cbor:"excludes,omitempty"`
	SnapshotMode        string         `json:"snapshot-mode" cbor:"snapshot-mode"`
	ChangeDetectionMode string         `json:"change-detection-mode" cbor:"change-detection-mode"`
	BackupTime          int64          `json:"backup-time" cbor:"backup-time"`
	Progress            func(Progress) `json:"-" cbor:"-"`
}

type Progress struct {
	Stage           string
	ProcessedBytes  uint64
	UploadedBytes   uint64
	ProcessedChunks uint64
	UploadedChunks  uint64
	Files           uint64
}

func emitProgress(job Job, progress Progress) {
	if job.Progress != nil {
		job.Progress(progress)
	}
}

type Result struct {
	BackupTime      int64         `json:"backup-time" cbor:"backup-time"`
	Files           int           `json:"files" cbor:"files"`
	Directories     int           `json:"directories" cbor:"directories"`
	LogicalBytes    int64         `json:"logical-bytes" cbor:"logical-bytes"`
	Duration        time.Duration `json:"duration" cbor:"duration"`
	CatalogUploaded bool          `json:"catalog-uploaded" cbor:"catalog-uploaded"`
}

func Run(ctx context.Context, job Job) (*Result, error) {
	if strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.HostID) == "" {
		return nil, fmt.Errorf("native backup job ID and host ID are required")
	}
	if strings.TrimSpace(job.Datastore) == "" {
		return nil, fmt.Errorf("native backup datastore is required")
	}
	if len(job.Sources) == 0 {
		return nil, fmt.Errorf("native backup requires at least one source")
	}

	enrolledHostID, err := agent.AgentHostID()
	if err != nil {
		return nil, fmt.Errorf("read enrolled host ID: %w", err)
	}
	if job.HostID != enrolledHostID {
		return nil, fmt.Errorf("backup job host %q does not match enrolled host %q", job.HostID, enrolledHostID)
	}

	server, authToken, tlsConfig, err := uploadCredentials()
	if err != nil {
		return nil, err
	}

	backupTime := job.BackupTime
	if backupTime <= 0 {
		backupTime = time.Now().UTC().Unix()
	}
	pbsURL, err := pbsAPIBase(server)
	if err != nil {
		return nil, err
	}

	// The server keeps a claimed run durable until it receives our completion report. If the
	// service crashed after PBS committed the snapshot but before that report reached PBS-X, the
	// same run is returned after restart. Reuse the server-assigned timestamp to recognize that
	// completed snapshot instead of creating a duplicate recovery point.
	times, listErr := listSnapshotTimes(ctx, pbsURL, job, authToken, tlsConfig)
	if listErr == nil {
		for _, existing := range times {
			if existing == backupTime {
				log.Info("native PBS host backup already committed; recovering completion", "job", job.ID, "backup_time", backupTime)
				return &Result{BackupTime: backupTime}, nil
			}
		}
	}

	emitProgress(job, Progress{Stage: "planning"})
	plans, err := planSources(job.Sources, job.Excludes)
	if err != nil {
		return nil, err
	}
	emitProgress(job, Progress{Stage: "snapshotting"})
	snaps, err := createSnapshots(job, plans)
	if err != nil {
		return nil, err
	}
	defer cleanupSnapshots(snaps)

	vfs, err := agentfs.NewVirtualAccessor(job.ID, "standard", snaps)
	if err != nil {
		return nil, err
	}
	defer vfs.Close()
	emitProgress(job, Progress{Stage: "scanning"})

	previous, err := findPreviousFromTimes(job, times, listErr, backupTime)
	if err != nil {
		// A first backup and a temporarily unavailable snapshot-list endpoint
		// should not prevent a full backup. Metadata mode simply falls back to
		// streaming data mode when no previous snapshot can be established.
		log.Warn("native backup could not resolve previous snapshot; performing full data scan", "error", err.Error())
	}

	detection := detectionMode(job.ChangeDetectionMode, previous != nil)
	config := backupproxy.BackupConfig{
		BackupID:       job.HostID,
		Namespace:      job.Namespace,
		CryptMode:      datastore.CryptModeNone,
		ChunkConfig:    buzhash.DefaultConfig(),
		BackupType:     datastore.BackupHost,
		BackupTime:     backupTime,
		DetectionMode:  detection,
		PreviousBackup: previous,
		Compress:       true,
		StrictMetadata: true,
		OnUploadProgress: func(progress backupproxy.UploadProgress) {
			emitProgress(job, Progress{
				Stage:           "uploading",
				ProcessedBytes:  progress.ProcessedBytes,
				UploadedBytes:   progress.UploadedBytes,
				ProcessedChunks: progress.ProcessedChunks,
				UploadedChunks:  progress.UploadedChunks,
			})
		},
	}

	store := backupproxy.NewPBSStore(backupproxy.PBSConfig{
		BaseURL:   pbsURL,
		Datastore: job.Datastore,
		AuthToken: authToken,
		Namespace: job.Namespace,
		TLSConfig: tlsConfig,
	}, buzhash.DefaultConfig(), true)
	client := backupproxy.NewLocalClient(vfs)
	serverRunner := backupproxy.NewServer(client, store)

	log.Info("native PBS host backup starting",
		"job", job.ID,
		"host", job.HostID,
		"store", job.Datastore,
		"namespace", job.Namespace,
		"sources", len(job.Sources),
		"mode", detection.String())

	res, err := serverRunner.RunBackupWithMode(ctx, ".", config)
	if err != nil {
		return nil, fmt.Errorf("native PBS backup failed: %w", err)
	}
	emitProgress(job, Progress{
		Stage:          "finishing",
		ProcessedBytes: uint64(max(res.TotalBytes, 0)),
		Files:          uint64(max(res.FileCount, 0)),
	})
	return &Result{
		BackupTime:      backupTime,
		Files:           res.FileCount,
		Directories:     res.DirCount,
		LogicalBytes:    res.TotalBytes,
		Duration:        res.Duration,
		CatalogUploaded: res.CatalogUploaded,
	}, nil
}

type sourcePlan struct {
	Source   string
	Excludes []string
}

func planSources(sources, excludes []string) ([]sourcePlan, error) {
	return planSourcesForGOOS(sources, excludes, runtime.GOOS)
}

func planSourcesForGOOS(sources, excludes []string, goos string) ([]sourcePlan, error) {
	if goos == "windows" {
		return planWindowsSources(sources, excludes)
	}
	return planNativeSources(sources, excludes)
}

func planNativeSources(sources, excludes []string) ([]sourcePlan, error) {
	plans := make([]sourcePlan, 0, len(sources))
	for index, rawSource := range sources {
		source := strings.TrimSpace(rawSource)
		if source == "" {
			return nil, fmt.Errorf("backup source %d is empty", index)
		}
		sourceAbs, err := filepath.Abs(source)
		if err != nil {
			return nil, fmt.Errorf("resolve backup source %q: %w", source, err)
		}
		plan := sourcePlan{Source: source}
		seen := make(map[string]struct{})
		excludeRoot := false
		for _, rawExclude := range excludes {
			exclude := strings.TrimSpace(rawExclude)
			if exclude == "" {
				continue
			}
			var rel string
			if isAbsoluteExclude(exclude) {
				excludeAbs, err := filepath.Abs(exclude)
				if err != nil {
					return nil, fmt.Errorf("resolve exclusion %q: %w", exclude, err)
				}
				rel, err = filepath.Rel(sourceAbs, excludeAbs)
				if err != nil {
					continue // different Windows volume, therefore not under this source
				}
				if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					continue
				}
			} else {
				rel = filepath.FromSlash(strings.ReplaceAll(exclude, `\`, "/"))
				rel = filepath.Clean(rel)
				if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
					return nil, fmt.Errorf("invalid source-relative exclusion %q", exclude)
				}
			}
			if rel == "." || rel == "" {
				excludeRoot = true
				break
			}
			rel = filepath.Clean(rel)
			if _, ok := seen[rel]; !ok {
				seen[rel] = struct{}{}
				plan.Excludes = append(plan.Excludes, rel)
			}
		}
		if excludeRoot {
			log.Info("backup source excluded by job", "source", source)
			continue
		}
		plans = append(plans, plan)
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("all configured backup sources are excluded")
	}
	return plans, nil
}

func planWindowsSources(sources, excludes []string) ([]sourcePlan, error) {
	plans := make([]sourcePlan, 0, len(sources))
	for index, rawSource := range sources {
		source := strings.TrimSpace(rawSource)
		if source == "" {
			return nil, fmt.Errorf("backup source %d is empty", index)
		}
		absoluteSource := source
		parsedSource, err := winpath.ParseAbsolute(absoluteSource)
		if err != nil && runtime.GOOS == "windows" {
			absoluteSource, err = filepath.Abs(source)
			if err == nil {
				parsedSource, err = winpath.ParseAbsolute(absoluteSource)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("resolve Windows backup source %q: %w", source, err)
		}

		plan := sourcePlan{Source: source}
		seen := make(map[string]struct{})
		excludeRoot := false
		for _, rawExclude := range excludes {
			exclude := strings.TrimSpace(rawExclude)
			if exclude == "" {
				continue
			}

			var relative string
			var inside bool
			if parsedExclude, parseErr := winpath.ParseAbsolute(exclude); parseErr == nil {
				relative, inside = winpath.RelativeTo(parsedSource, parsedExclude)
				if !inside {
					continue
				}
			} else if strings.HasPrefix(exclude, `\`) || strings.HasPrefix(exclude, "/") {
				parsedExclude, rootErr := winpath.WithRootRelative(parsedSource, exclude)
				if rootErr != nil {
					return nil, fmt.Errorf("resolve Windows exclusion %q: %w", exclude, rootErr)
				}
				relative, inside = winpath.RelativeTo(parsedSource, parsedExclude)
				if !inside {
					continue
				}
			} else {
				relative, err = winpath.CleanRelative(exclude)
				if err != nil {
					return nil, fmt.Errorf("invalid source-relative exclusion %q: %w", exclude, err)
				}
			}

			if relative == "" {
				excludeRoot = true
				break
			}
			key := strings.ToLower(relative)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				plan.Excludes = append(plan.Excludes, relative)
			}
		}
		if excludeRoot {
			log.Info("backup source excluded by job", "source", source)
			continue
		}
		plans = append(plans, plan)
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("all configured backup sources are excluded")
	}
	return plans, nil
}

func isAbsoluteExclude(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
		return true
	}
	return strings.HasPrefix(value, `\`) || strings.HasPrefix(value, "/")
}

func createSnapshots(job Job, plans []sourcePlan) ([]snapshots.Snapshot, error) {
	mode := strings.ToLower(strings.TrimSpace(job.SnapshotMode))
	if mode == "" {
		mode = "auto"
	}
	result := make([]snapshots.Snapshot, 0, len(plans))
	for index, plan := range plans {
		source := strings.TrimSpace(plan.Source)
		if source == "" {
			cleanupSnapshots(result)
			return nil, fmt.Errorf("backup source %d is empty", index)
		}
		if mode == "direct" {
			snap := directSnapshot(source)
			snap.Excludes = append(snap.Excludes, plan.Excludes...)
			result = append(result, snap)
			continue
		}
		snap, err := snapshots.Manager.CreateSnapshot(job.ID, source)
		if err == nil {
			snap.Excludes = append(snap.Excludes, plan.Excludes...)
			result = append(result, snap)
			continue
		}
		if mode == "snapshot" {
			cleanupSnapshots(result)
			return nil, fmt.Errorf("snapshot source %q: %w", source, err)
		}
		log.Warn("snapshot provider unavailable; using direct read for this source", "source", source, "error", err.Error())
		snap = directSnapshot(source)
		snap.Excludes = append(snap.Excludes, plan.Excludes...)
		result = append(result, snap)
	}
	return result, nil
}

func directSnapshot(source string) snapshots.Snapshot {
	path := source
	if abs, err := filepath.Abs(source); err == nil {
		path = abs
	}
	return snapshots.Snapshot{
		Path:        path,
		SourcePath:  source,
		TimeStarted: time.Now(),
		Direct:      true,
	}
}

func cleanupSnapshots(snaps []snapshots.Snapshot) {
	for i := len(snaps) - 1; i >= 0; i-- {
		snap := snaps[i]
		if snap.Direct || snap.Handler == nil {
			continue
		}
		if err := snap.Handler.DeleteSnapshot(snap); err != nil {
			log.Error(err, "failed to clean up native backup snapshot", "source", snap.SourcePath, "path", snap.Path)
		}
	}
}

func uploadCredentials() (string, string, *tls.Config, error) {
	return agent.PBSConnectionInfo()
}

func pbsAPIBase(server string) (string, error) {
	server = strings.TrimSpace(server)
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}
	u, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("invalid PBS-X server URL %q", server)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("PBS-X native backup requires HTTPS")
	}
	if u.Port() == "" {
		u.Host = u.Hostname() + ":8007"
	}
	u.Path = "/api2/json"
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

type snapshotsResponse struct {
	Data []struct {
		BackupTime int64 `json:"backup-time"`
	} `json:"data"`
}

func listSnapshotTimes(ctx context.Context, baseURL string, job Job, authToken string, tlsConfig *tls.Config) ([]int64, error) {
	u, err := url.Parse(baseURL + "/admin/datastore/" + url.PathEscape(job.Datastore) + "/snapshots")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("backup-type", "host")
	q.Set("backup-id", job.HostID)
	if job.Namespace != "" {
		q.Set("ns", job.Namespace)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "PBSAPIToken "+authToken)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig.Clone()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, fmt.Errorf("list previous PBS snapshots: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload snapshotsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	times := make([]int64, 0, len(payload.Data))
	for _, item := range payload.Data {
		times = append(times, item.BackupTime)
	}
	return times, nil
}

func findPreviousFromTimes(job Job, times []int64, listErr error, before int64) (*backupproxy.PreviousBackupRef, error) {
	if listErr != nil {
		return nil, listErr
	}
	var latest int64
	for _, backupTime := range times {
		if backupTime < before && backupTime > latest {
			latest = backupTime
		}
	}
	if latest == 0 {
		return nil, nil
	}
	return &backupproxy.PreviousBackupRef{
		BackupID:   job.HostID,
		Namespace:  job.Namespace,
		BackupType: datastore.BackupHost,
		BackupTime: latest,
	}, nil
}

func detectionMode(mode string, hasPrevious bool) backupproxy.DetectionMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "legacy":
		return backupproxy.DetectionLegacy
	case "data":
		return backupproxy.DetectionData
	case "metadata", "":
		if hasPrevious {
			return backupproxy.DetectionMetadata
		}
		return backupproxy.DetectionData
	default:
		if hasPrevious {
			return backupproxy.DetectionMetadata
		}
		return backupproxy.DetectionData
	}
}
