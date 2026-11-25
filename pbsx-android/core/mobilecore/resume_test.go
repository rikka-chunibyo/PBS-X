package mobilecore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pbs-plus/pxar/datastore"
)

func runForTest(t *testing.T) (*Client, *backupRun) {
	t.Helper()
	cfg := configuration{Server: "https://pbs.example:8007", HostID: "test-phone", Fingerprint: strings.Repeat("ab", 32), AuthID: "user@pbs!key", Secret: "secret"}
	raw, _ := json.Marshal(cfg)
	client, err := NewClient(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	run := &backupRun{Version: 1, Token: strings.Repeat("12", 32), StateKey: strings.Repeat("34", 32), Source: strings.Repeat("56", 32), Store: "backups", Namespace: "phone", BackupID: "main", JobID: "test-job", JobName: "Main", Stamp: 12345}
	run.Binding = client.runBinding(run)
	return client, run
}
func TestResumeDescriptorBindingRejectsChangedEffectiveSettings(t *testing.T) {
	client, run := runForTest(t)
	raw, _ := json.Marshal(run)
	if _, err := client.checkedRun(string(raw), run.Source); err != nil {
		t.Fatal(err)
	}
	original := client.cfg
	for name, change := range map[string]func(){"server": func() { client.cfg.Server = "https://other.example" }, "pin": func() { client.cfg.Fingerprint = strings.Repeat("cd", 32) }, "credential": func() { client.cfg.Secret = "different" }, "storage-key": func() { client.cfg.StorageAuthID = "other@pbs!key"; client.cfg.StorageSecret = "other" }} {
		t.Run(name, func(t *testing.T) {
			client.cfg = original
			change()
			if _, err := client.checkedRun(string(raw), run.Source); err == nil {
				t.Fatal("accepted changed configuration")
			}
		})
	}
	client.cfg = original
	client.cfg.DefaultStore = "cosmetic"
	client.cfg.DefaultNamespace = new("elsewhere")
	client.cfg.HostID = "renamed-phone"
	if _, err := client.checkedRun(string(raw), run.Source); err != nil {
		t.Fatal("irrelevant defaults changed binding:", err)
	}
	if _, err := client.checkedRun(string(raw), strings.Repeat("ef", 32)); err == nil {
		t.Fatal("accepted different source")
	}
	run.Stamp++
	raw, _ = json.Marshal(run)
	if _, err := client.checkedRun(string(raw), run.Source); err == nil {
		t.Fatal("accepted different original timestamp")
	}
}
func TestResumeDescriptorStrictValidation(t *testing.T) {
	_, run := runForTest(t)
	raw, _ := json.Marshal(run)
	for _, bad := range []string{string(raw) + " {}", strings.Replace(string(raw), `"version":1`, `"version":2`, 1), strings.Replace(string(raw), `"prepared":0`, `"prepared":2`, 1), strings.Replace(string(raw), `"namespace":"phone"`, `"namespace":"../other"`, 1), strings.TrimSuffix(string(raw), "}") + `,"unknown":true}`} {
		if err := ValidateBackupRun(bad); err == nil {
			t.Fatal("accepted invalid descriptor")
		}
	}
	run.Candidate = []datastore.BackupFileInfo{{Filename: "root.mpxar.didx", CryptMode: "none", CSum: strings.Repeat("aa", 32)}, {Filename: "root.ppxar.didx", CryptMode: "none", CSum: strings.Repeat("bb", 32)}}
	raw, _ = json.Marshal(run)
	if err := ValidateBackupRun(string(raw)); err != nil {
		t.Fatal(err)
	}
	run.Candidate[1] = run.Candidate[0]
	raw, _ = json.Marshal(run)
	if err := ValidateBackupRun(string(raw)); err == nil {
		t.Fatal("accepted duplicate final indexes")
	}
}

func TestDiscardRequiresExplicitSnapshotListBeforeForgettingRun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		allow bool
	}{
		{"empty list", `{"data":[]}`, true},
		{"omitted list", `{}`, false},
		{"null list", `{"data":null}`, false},
		{"null response", `null`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api2/json/admin/datastore/backups/snapshots" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := NewClient(configForServer(t, server))
			if err != nil {
				t.Fatal(err)
			}
			_, run := runForTest(t)
			run.Binding = client.runBinding(run)
			raw, err := json.Marshal(run)
			if err != nil {
				t.Fatal(err)
			}
			err = client.DiscardBackupRun(string(raw), run.Source)
			if tc.allow && err != nil {
				t.Fatal("explicit empty snapshot list was refused:", err)
			}
			if !tc.allow && (err == nil || !strings.Contains(err.Error(), "snapshot list")) {
				t.Fatal("discard accepted an ambiguous snapshot list:", err)
			}
		})
	}
}
func TestCancelCannotBeDowngradedToPause(t *testing.T) {
	client, _ := runForTest(t)
	ctx, done, err := client.beginMode(true)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	client.Cancel()
	client.Pause()
	if client.pauseRequested() || ctx.Err() == nil {
		t.Fatal("cancel downgraded to pause")
	}
}

func TestClosingUnfinishedDocumentDoesNotReadFromOpenPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	document := &checkedDocument{file: reader, fs: &documentFS{ctx: context.Background()}, before: documentEntry{Size: 128}}
	finished := make(chan error, 1)
	go func() { finished <- document.Close() }()
	select {
	case err := <-finished:
		if err == nil || !strings.Contains(err.Error(), "not fully consumed") {
			t.Fatal("unfinished source was accepted", err)
		}
	case <-time.After(time.Second):
		reader.Close()
		t.Fatal("closing an unfinished source blocked on its open pipe")
	}
}
