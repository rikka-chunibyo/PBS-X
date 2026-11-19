//go:build integration

package backupproxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pbs-plus/pxar/accessor"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
)

// Deliberately restricted to the dedicated synthetic datastore on lab VM 104.
// These tests must never inherit the phone's PBS endpoint or an ordinary store.
func resumeFixtureConfig(t *testing.T) PBSConfig {
	t.Helper()
	filename := os.Getenv("PBSX_RESUME_FIXTURE_CONFIG")
	if filename == "" {
		t.Skip("requires the isolated PBS-X resume fixture config file")
	}
	stat, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm()&0077 != 0 || stat.Size() > 65536 {
		t.Fatal("fixture credentials must be private and bounded")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		URL         string `json:"url"`
		Datastore   string `json:"datastore"`
		Token       string `json:"auth_token"`
		Fingerprint string `json:"fingerprint"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.URL != "https://10.0.1.21:8007/api2/json" || fixture.Datastore != "pbsx-resume-fixture-20261119" || !strings.HasPrefix(fixture.Token, "resume-fixture-20261119@pbs!resume:") {
		t.Fatal("refusing a non-fixture PBS endpoint/credential")
	}
	pin, err := hex.DecodeString(fixture.Fingerprint)
	if err != nil || len(pin) != 32 {
		t.Fatal("invalid fixture certificate pin")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyConnection: func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("missing fixture certificate")
		}
		sum := sha256.Sum256(state.PeerCertificates[0].Raw)
		if !bytes.Equal(sum[:], pin) {
			return errors.New("fixture certificate pin mismatch")
		}
		return nil
	}}
	return PBSConfig{BaseURL: fixture.URL, Datastore: fixture.Datastore, AuthToken: fixture.Token, TLSConfig: tlsConfig}
}

type liveResumeJournal struct {
	Token     string                     `json:"token"`
	Key       string                     `json:"key"`
	Binding   string                     `json:"binding"`
	Prepared  uint64                     `json:"prepared"`
	Confirmed uint64                     `json:"confirmed"`
	Final     []datastore.BackupFileInfo `json:"final"`
}

func writeLiveJournal(filename string, journal liveResumeJournal) error {
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filename+".next", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(filename+".next", filename); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func liveResumeOptions(t *testing.T, filename string, journal *liveResumeJournal) DocumentResumeOptions {
	t.Helper()
	key, err := hex.DecodeString(journal.Key)
	if err != nil {
		t.Fatal(err)
	}
	return DocumentResumeOptions{StateKey: key, Binding: journal.Binding, ConfirmedSequence: journal.Confirmed, CheckpointBytes: 64 << 10, CheckpointInterval: time.Hour,
		PrepareCheckpoint: func(sequence uint64) error { journal.Prepared = sequence; return writeLiveJournal(filename, *journal) },
		CommitCheckpoint: func(sequence uint64, _ DocumentProgress, _ uint64) error {
			journal.Confirmed = sequence
			return writeLiveJournal(filename, *journal)
		},
		PrepareFinish: func(files []datastore.BackupFileInfo) error {
			journal.Final = append([]datastore.BackupFileInfo(nil), files...)
			return writeLiveJournal(filename, *journal)
		},
	}
}

func fixtureRequest(ctx context.Context, cfg PBSConfig, method, endpoint string) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, method, cfg.BaseURL+endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "PBSAPIToken "+cfg.AuthToken)
	transport := &http.Transport{TLSClientConfig: cfg.TLSConfig}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	return raw, response.StatusCode, err
}

func fixtureSnapshotEndpoint(cfg PBSConfig, backup BackupConfig) string {
	values := url.Values{"backup-type": {"host"}, "backup-id": {backup.BackupID}, "backup-time": {strconv.FormatInt(backup.BackupTime, 10)}}
	return "/admin/datastore/" + url.PathEscape(cfg.Datastore) + "/snapshots?" + values.Encode()
}

func TestIntegration_ResumeSameSnapshotWithoutReopeningCompletedFiles(t *testing.T) {
	runLiveResumeCase(t, false)
}

func TestIntegration_ResumeAfterConnectionLossAndGarbageCollection(t *testing.T) {
	runLiveResumeCase(t, true)
}

func fixtureGarbageCollection(t *testing.T, ctx context.Context, cfg PBSConfig) {
	t.Helper()
	endpoint := "/admin/datastore/" + url.PathEscape(cfg.Datastore) + "/gc"
	raw, status, err := fixtureRequest(ctx, cfg, http.MethodPost, endpoint)
	if err != nil || status != http.StatusOK {
		t.Fatalf("start fixture GC: status=%d error=%v", status, err)
	}
	var started struct {
		Data string `json:"data"`
	}
	if err = json.Unmarshal(raw, &started); err != nil || started.Data == "" {
		t.Fatal("fixture GC did not return a task")
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		raw, status, err = fixtureRequest(ctx, cfg, http.MethodGet, "/nodes/localhost/tasks/"+url.PathEscape(started.Data)+"/status")
		if err != nil || status != http.StatusOK {
			t.Fatalf("read fixture GC task: status=%d error=%v", status, err)
		}
		var result struct {
			Data struct {
				Status string `json:"status"`
				Exit   string `json:"exitstatus"`
			} `json:"data"`
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Data.Status == "stopped" {
			if result.Data.Exit != "OK" {
				t.Fatalf("fixture GC failed: %s", result.Data.Exit)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture GC timed out")
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	raw, status, err = fixtureRequest(ctx, cfg, http.MethodGet, endpoint)
	if err != nil || status != http.StatusOK {
		t.Fatal("could not inspect fixture GC results")
	}
	var result struct {
		Data struct {
			Indexes uint64 `json:"index-file-count"`
			Bytes   uint64 `json:"index-data-bytes"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Indexes < 2 || result.Data.Bytes < 64<<10 {
		t.Fatalf("GC did not mark paused prefixes: indexes=%d bytes=%d", result.Data.Indexes, result.Data.Bytes)
	}
}

func runLiveResumeCase(t *testing.T, disconnect bool) {
	pbs := resumeFixtureConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	chunkConfig, _ := buzhash.NewConfig(4096)
	store := NewPBSStore(pbs, chunkConfig, false)
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	identity := sha256.Sum256(random)
	journal := liveResumeJournal{Token: hex.EncodeToString(random), Key: hex.EncodeToString(key), Binding: hex.EncodeToString(identity[:])}
	filename := filepath.Join(t.TempDir(), "journal.json")
	if err := writeLiveJournal(filename, journal); err != nil {
		t.Fatal(err)
	}
	backup := BackupConfig{BackupType: datastore.BackupHost, BackupID: "resume-fixture-" + journal.Token[:12], BackupTime: time.Now().Unix(), ResumeToken: journal.Token}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _, _ = fixtureRequest(cleanup, pbs, http.MethodDelete, fixtureSnapshotEndpoint(pbs, backup))
	})
	source := newContinuationFixture()
	pause := false
	options := liveResumeOptions(t, filename, &journal)
	runContext, stopRun := context.WithCancel(ctx)
	defer stopRun()
	if disconnect {
		options.CheckpointBytes = 100 << 10
		options.Progress = func(progress DocumentProgress) {
			if progress.Bytes >= 180<<10 {
				stopRun()
			}
		}
	} else {
		options.Progress = func(progress DocumentProgress) {
			if progress.Bytes >= 64<<10 {
				pause = true
			}
		}
		options.PauseRequested = func() bool { return pause }
	}
	first, err := store.RunResumableDocumentBackup(runContext, backup, source.client, options)
	if disconnect {
		if err == nil || journal.Confirmed < 2 {
			t.Fatal("fixture did not disconnect after a durable mid-file checkpoint")
		}
	} else {
		if err != nil {
			t.Fatal(err)
		}
		if !first.Paused || first.Sequence < 1 {
			t.Fatal("pause finalized or lost the backup")
		}
	}
	if len(journal.Final) != 0 {
		t.Fatal("interrupted run prepared a completed manifest")
	}
	if source.opens["first"] != 1 || source.opens["nested/empty"] != 1 {
		t.Fatal("fixture did not finish its first files before pausing")
	}
	// No complete manifest exists while paused. A reader may connect, but it
	// must not be able to load a completed-snapshot manifest.
	reader := NewPBSReader(pbs, "host", backup.BackupID, backup.BackupTime)
	if err = reader.Connect(ctx); err == nil {
		_, err = reader.DownloadFile("index.json.blob")
		reader.Close()
		if err == nil {
			t.Fatal("paused snapshot had a completed manifest")
		}
	}
	wrong := backup
	wrong.ResumeToken = strings.Repeat("0", 64)
	if session, err := store.StartSession(ctx, wrong); err == nil {
		session.Close()
		t.Fatal("wrong resume token was accepted")
	}
	if disconnect {
		fixtureGarbageCollection(t, ctx, pbs)
	}
	// Re-read the durable descriptor rather than reusing the producer/session.
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var recovered liveResumeJournal
	if err = json.Unmarshal(raw, &recovered); err != nil {
		t.Fatal(err)
	}
	second, err := NewPBSStore(pbs, chunkConfig, false).RunResumableDocumentBackup(ctx, backup, source.client, liveResumeOptions(t, filename, &recovered))
	if err != nil {
		t.Fatal(err)
	}
	if second.Paused || second.Manifest == nil || second.Manifest.BackupTime != backup.BackupTime || len(recovered.Final) != 2 {
		t.Fatal("resume changed snapshot identity or did not complete")
	}
	if source.opens["first"] != 1 || source.opens["nested/empty"] != 1 || source.opens["nested/large"] != 2 || source.lists["."] != 1 || source.lists["nested"] != 1 {
		t.Fatalf("completed data was replayed: opens=%v lists=%v", source.opens, source.lists)
	}
	if err = store.DiscardDocumentBackup(ctx, backup); err == nil {
		t.Fatal("cancel accepted a completed snapshot")
	}
	reader = NewPBSReader(pbs, "host", backup.BackupID, backup.BackupTime)
	if err = reader.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var restored [2]bytes.Buffer
	for i, name := range []string{"root.mpxar.didx", "root.ppxar.didx"} {
		raw, err := reader.DownloadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		index, err := datastore.ParseDynamicIndex(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err = reader.RestoreFile(index, &restored[i]); err != nil {
			t.Fatal(err)
		}
	}
	archive := accessor.NewAccessor(bytes.NewReader(restored[0].Bytes()), bytes.NewReader(restored[1].Bytes()))
	if _, err = archive.ReadRoot(); err != nil {
		t.Fatal(err)
	}
	assertArchivesBinaryEqual(t, archive, source.files)
}

func TestIntegration_ResumeCancellationDiscardsOnlyItsIncompleteSnapshot(t *testing.T) {
	pbs := resumeFixtureConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	chunkConfig, _ := buzhash.NewConfig(4096)
	store := NewPBSStore(pbs, chunkConfig, false)
	var secret, key [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	identity := sha256.Sum256(secret[:])
	journal := liveResumeJournal{Token: hex.EncodeToString(secret[:]), Key: hex.EncodeToString(key[:]), Binding: hex.EncodeToString(identity[:])}
	filename := filepath.Join(t.TempDir(), "journal.json")
	if err := writeLiveJournal(filename, journal); err != nil {
		t.Fatal(err)
	}
	backup := BackupConfig{BackupType: datastore.BackupHost, BackupID: "resume-fixture-" + journal.Token[:12], BackupTime: time.Now().Unix(), ResumeToken: journal.Token}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _, _ = fixtureRequest(cleanup, pbs, http.MethodDelete, fixtureSnapshotEndpoint(pbs, backup))
	})
	options := liveResumeOptions(t, filename, &journal)
	options.PauseRequested = func() bool { return true }
	paused, err := store.RunResumableDocumentBackup(ctx, backup, newContinuationFixture().client, options)
	if err != nil {
		t.Fatal(err)
	}
	if !paused.Paused || journal.Confirmed == 0 {
		t.Fatal("fixture was not checkpointed")
	}
	wrong := backup
	wrong.ResumeToken = strings.Repeat("0", 64)
	if err = store.DiscardDocumentBackup(ctx, wrong); err == nil {
		t.Fatal("wrong token discarded the checkpoint")
	}
	if err = store.DiscardDocumentBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	endpoint := "/admin/datastore/" + url.PathEscape(pbs.Datastore) + "/snapshots?" + url.Values{"backup-type": {"host"}, "backup-id": {backup.BackupID}}.Encode()
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, status, err := fixtureRequest(ctx, pbs, http.MethodGet, endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if status == 404 {
			break
		}
		if status != 200 {
			t.Fatalf("read cancelled fixture: HTTP %d", status)
		}
		var snapshots struct {
			Data []struct {
				Time int64 `json:"backup-time"`
			} `json:"data"`
		}
		if err = json.Unmarshal(raw, &snapshots); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, snapshot := range snapshots.Data {
			if snapshot.Time == backup.BackupTime {
				found = true
			}
		}
		if !found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled checkpoint remained on the server")
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err = store.DiscardDocumentBackup(ctx, backup); err != nil {
		t.Fatal("cancelling an already removed checkpoint should be harmless")
	}
}
