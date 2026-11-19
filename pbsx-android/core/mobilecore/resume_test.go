package mobilecore

import (
	"encoding/json"
	"strings"
	"testing"

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
