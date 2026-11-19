//go:build integration

package mobilecore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fixtureRunJournal struct {
	raw      string
	filename string
}

func (j *fixtureRunJournal) SaveJSON(raw string) error {
	file, err := os.OpenFile(j.filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = file.WriteString(raw); err != nil {
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
	j.raw = raw
	return nil
}
func mobileResumeFixture(t *testing.T) (*Client, configuration, string) {
	t.Helper()
	filename := os.Getenv("PBSX_RESUME_FIXTURE_CONFIG")
	if filename == "" {
		t.Skip("requires the isolated VM104 resume fixture")
	}
	st, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0077 != 0 || st.Size() > 65536 {
		t.Fatal("fixture config must be private and bounded")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		URL   string `json:"url"`
		Store string `json:"datastore"`
		Token string `json:"auth_token"`
		Pin   string `json:"fingerprint"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.URL != "https://10.0.1.21:8007/api2/json" || fixture.Store != "pbsx-resume-fixture-20261119" || !strings.HasPrefix(fixture.Token, "resume-fixture-20261119@pbs!resume:") {
		t.Fatal("refusing non-fixture endpoint")
	}
	id, secret, _ := strings.Cut(fixture.Token, ":")
	key, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := configuration{Server: "https://10.0.1.21:8007", HostID: "resume-phone-fixture", AuthID: id, Secret: secret, Fingerprint: fixture.Pin, EncryptionEnabled: true, EncryptionKey: key}
	raw, _ = json.Marshal(cfg)
	client, err := NewClient(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return client, cfg, fixture.Store
}
func TestIntegration_MobileResumeEncryptedRunAndVerifyFinalCandidate(t *testing.T) {
	client, cfg, store := mobileResumeFixture(t)
	id := "mobile-resume-" + time.Now().UTC().Format("150405.000000000")
	source := strings.Repeat("ab", 32)
	descriptor, err := client.NewBackupRun(store, "", id, "test-job", "Synthetic Android resume", source)
	if err != nil {
		t.Fatal(err)
	}
	journal := &fixtureRunJournal{filename: filepath.Join(t.TempDir(), "journal"), raw: descriptor}
	if err = journal.SaveJSON(descriptor); err != nil {
		t.Fatal(err)
	}
	run, _ := parseBackupRun(descriptor)
	t.Cleanup(func() {
		_, _ = client.request(t.Context(), "DELETE", "/admin/datastore/"+store+"/snapshots?backup-type=host&backup-id="+id+"&backup-time="+strconv.FormatInt(run.Stamp, 10), nil, true)
	})
	payload := bytes.Repeat([]byte("synthetic private Android file\n"), 18000)
	docs := &fsDocuments{root: t.TempDir()}
	if err = os.WriteFile(filepath.Join(docs.root, "payload.txt"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	docs.opened = func() { client.Pause() }
	result, err := client.BackupGroupResumable(descriptor, source, docs, journal, nil)
	if err != nil {
		t.Fatal(err)
	}
	var paused struct {
		Paused bool `json:"paused"`
	}
	if err = json.Unmarshal([]byte(result), &paused); err != nil || !paused.Paused {
		t.Fatal("run did not pause", err)
	}
	saved, err := os.ReadFile(journal.filename)
	if err != nil {
		t.Fatal(err)
	}
	partial, _ := parseBackupRun(string(saved))
	if partial.Confirmed < 2 || partial.Bytes >= uint64(len(payload)) {
		t.Fatal("expected an acknowledged checkpoint after opening the file")
	}
	raw, _ := json.Marshal(cfg)
	client, err = NewClient(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	docs.opened = nil
	if _, err = client.BackupGroupResumable(string(saved), source, docs, journal, nil); err != nil {
		t.Fatal(err)
	}
	final, _ := parseBackupRun(journal.raw)
	if final.Stamp != run.Stamp || final.Token != run.Token || !validCandidate(final.Candidate) {
		t.Fatal("resume changed original identity or omitted candidate")
	}
	// Simulate app death after the server finished but before Android cleared its record.
	verified, err := client.CompletedBackupRun(journal.raw, source)
	if err != nil || verified == "" {
		t.Fatal("could not verify uncertain final response", err)
	}
	altered := *final
	altered.Candidate = append(altered.Candidate[:0:0], final.Candidate...)
	altered.Candidate[0].CSum = strings.Repeat("00", 32)
	bad, _ := json.Marshal(altered)
	if _, err = client.CompletedBackupRun(string(bad), source); err == nil {
		t.Fatal("accepted an incorrect final candidate")
	}
	if err = client.DiscardBackupRun(journal.raw, source); err == nil {
		t.Fatal("discarded a completed snapshot")
	}
	output := &fsDocuments{root: t.TempDir()}
	if _, err = client.RestoreGroupSelected(store, "", "host", id, run.Stamp, `["."]`, "skip", output, nil); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(output.root, "payload.txt"))
	if err != nil || !bytes.Equal(payload, restored) {
		t.Fatal("encrypted resumed bytes differ", err)
	}
}
