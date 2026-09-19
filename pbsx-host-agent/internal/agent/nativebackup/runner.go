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
	"strings"
	"time"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
)

// Job is the agent-side form of a PBS-X host backup job. HostID is the PBS
// backup group identity and must match the identity enrolled on this machine.
type Job struct {
	ID                  string   `json:"id" cbor:"id"`
	HostID              string   `json:"host" cbor:"host"`
	Datastore           string   `json:"store" cbor:"store"`
	Namespace           string   `json:"ns,omitempty" cbor:"ns,omitempty"`
	Sources             []string `json:"sources" cbor:"sources"`
	SnapshotMode        string   `json:"snapshot-mode" cbor:"snapshot-mode"`
	ChangeDetectionMode string   `json:"change-detection-mode" cbor:"change-detection-mode"`
	BackupTime          int64    `json:"backup-time" cbor:"backup-time"`
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

	snaps, err := createSnapshots(job)
	if err != nil {
		return nil, err
	}
	defer cleanupSnapshots(snaps)

	vfs, err := agentfs.NewVirtualAccessor(job.ID, "standard", snaps)
	if err != nil {
		return nil, err
	}
	defer vfs.Close()

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

	res, err := serverRunner.RunBackupWithMode(ctx, "/", config)
	if err != nil {
		return nil, fmt.Errorf("native PBS backup failed: %w", err)
	}
	return &Result{
		BackupTime:      backupTime,
		Files:           res.FileCount,
		Directories:     res.DirCount,
		LogicalBytes:    res.TotalBytes,
		Duration:        res.Duration,
		CatalogUploaded: res.CatalogUploaded,
	}, nil
}

func createSnapshots(job Job) ([]snapshots.Snapshot, error) {
	mode := strings.ToLower(strings.TrimSpace(job.SnapshotMode))
	if mode == "" {
		mode = "auto"
	}
	result := make([]snapshots.Snapshot, 0, len(job.Sources))
	for index, source := range job.Sources {
		source = strings.TrimSpace(source)
		if source == "" {
			cleanupSnapshots(result)
			return nil, fmt.Errorf("backup source %d is empty", index)
		}
		if mode == "direct" {
			result = append(result, directSnapshot(source))
			continue
		}
		snap, err := snapshots.Manager.CreateSnapshot(job.ID, source)
		if err == nil {
			result = append(result, snap)
			continue
		}
		if mode == "snapshot" {
			cleanupSnapshots(result)
			return nil, fmt.Errorf("snapshot source %q: %w", source, err)
		}
		log.Warn("snapshot provider unavailable; using direct read for this source", "source", source, "error", err.Error())
		result = append(result, directSnapshot(source))
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
	server, err := registry.GetEntry(registry.CONFIG, "ServerURL", false)
	if err != nil || server == nil || strings.TrimSpace(server.Value) == "" {
		return "", "", nil, fmt.Errorf("PBS-X server URL is not configured")
	}
	authID, err := registry.GetEntry(registry.AUTH, "BackupAuthID", false)
	if err != nil || authID == nil || strings.TrimSpace(authID.Value) == "" {
		return "", "", nil, fmt.Errorf("PBS-X backup upload auth ID is missing; re-enroll the host")
	}
	secret, err := registry.GetEntry(registry.AUTH, "BackupSecret", true)
	if err != nil || secret == nil || strings.TrimSpace(secret.Value) == "" {
		return "", "", nil, fmt.Errorf("PBS-X backup upload secret is missing; re-enroll the host")
	}
	tlsConfig, err := agent.GetTLSConfig()
	if err != nil {
		return "", "", nil, err
	}
	return server.Value, strings.TrimSpace(authID.Value) + ":" + strings.TrimSpace(secret.Value), tlsConfig, nil
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
