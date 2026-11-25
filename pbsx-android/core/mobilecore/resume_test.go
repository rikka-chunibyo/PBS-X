package mobilecore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pbs-plus/pxar/datastore"
	"golang.org/x/net/http2"
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

func TestDiscardRequiresExactAuthenticatedAbsence(t *testing.T) {
	type reply struct {
		status int
		body   string
	}
	missing := reply{http.StatusNotFound, "incomplete checkpoint does not exist"}
	refused := reply{http.StatusBadRequest, "backup owner check failed"}
	accepted := reply{http.StatusSwitchingProtocols, ""}
	for _, tc := range []struct {
		name   string
		list   string
		probes []reply
		fail   string
	}{
		{"absent", `{"data":[]}`, []reply{missing}, ""},
		{"hidden by changed owner", `{"data":[]}`, []reply{refused}, "owner check failed"},
		{"generic not found", `{"data":[]}`, []reply{{http.StatusNotFound, "unknown route"}}, "unknown route"},
		{"acceptance followed by confirmation", `{"data":[]}`, []reply{accepted, missing}, ""},
		{"ownership changes during cleanup", `{"data":[]}`, []reply{accepted, refused}, "owner check failed"},
		{"completed snapshot", `{"data":[{"backup-type":"host","backup-id":"main","backup-time":12345,"size":0}]}`, nil, "already completed"},
		{"omitted list", `{}`, nil, "snapshot list"},
		{"null list", `{"data":null}`, nil, "snapshot list"},
		{"null response", `null`, nil, "snapshot list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, run := runForTest(t)
			var probes atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api2/json/admin/datastore/backups/snapshots":
					_, _ = io.WriteString(w, tc.list)
				case "/api2/json/backup":
					i := int(probes.Add(1)) - 1
					if i >= len(tc.probes) {
						t.Error("unexpected discard probe")
						http.Error(w, "unexpected probe", http.StatusBadRequest)
						return
					}
					q := r.URL.Query()
					if r.Method != http.MethodGet || q.Get("store") != "backups" || q.Get("ns") != "phone" ||
						q.Get("backup-type") != "host" || q.Get("backup-id") != "main" || q.Get("backup-time") != "12345" ||
						q.Get("pbsx-resume-discard") != "1" || r.Header.Get("PBSX-Resume-Token") != run.Token ||
						r.Header.Get("Authorization") != "PBSAPIToken test@pbs!device:test-secret" ||
						strings.Contains(r.URL.RawQuery, run.Token) {
						t.Error("discard did not authenticate the exact target")
					}
					probe := tc.probes[i]
					if probe.status != http.StatusSwitchingProtocols {
						http.Error(w, probe.body, probe.status)
						return
					}
					conn, rw, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: proxmox-backup-protocol-v1\r\n\r\n")
					if rw.Flush() != nil {
						return
					}
					if _, err = io.CopyN(io.Discard, rw, int64(len(http2.ClientPreface))); err != nil {
						return
					}
					framer := http2.NewFramer(conn, rw)
					if err = framer.WriteSettings(); err != nil {
						return
					}
					_, _ = io.Copy(io.Discard, rw)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusBadRequest)
				}
			}))
			defer server.Close()
			client, err := NewClient(configForServer(t, server))
			if err != nil {
				t.Fatal(err)
			}
			run.Binding = client.runBinding(run)
			raw, err := json.Marshal(run)
			if err != nil {
				t.Fatal(err)
			}
			err = client.DiscardBackupRun(string(raw), run.Source)
			if tc.fail == "" && err != nil {
				t.Fatal("confirmed absence was refused:", err)
			}
			if tc.fail != "" && (err == nil || !strings.Contains(err.Error(), tc.fail)) {
				t.Fatal("discard did not preserve the descriptor on uncertainty:", err)
			}
			if got := int(probes.Load()); got != len(tc.probes) {
				t.Fatalf("got %d discard probes, want %d", got, len(tc.probes))
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
