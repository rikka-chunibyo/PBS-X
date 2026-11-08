//go:build integration

package mobilecore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run only through test-live-pbs.sh on the disposable test PBS VM as root.
// All real credentials remain in this process/on that VM, never in tool output.
func TestLiveEnrollmentKeys(t *testing.T) {
	enrollment := os.Getenv("PBSX_ANDROID_TEST_ENROLLMENT")
	namespace := os.Getenv("PBSX_ANDROID_TEST_NAMESPACE")
	jobID := os.Getenv("PBSX_ANDROID_TEST_DISABLED_JOB")
	if enrollment == "" || namespace == "" || jobID == "" {
		t.Skip("requires disposable PBS fixture")
	}
	code, err := os.ReadFile(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Enroll(string(code), "Enrollment lifecycle fixture A")
	if err != nil {
		t.Fatal(err)
	}
	var cfg configuration
	if err := json.Unmarshal([]byte(first), &cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.HostID, "android-smoke-") || cfg.HostID != namespace || jobID != namespace {
		t.Fatal("refusing to mutate anything except the disposable fixture")
	}
	api := func(method, endpoint string, args ...string) map[string]json.RawMessage {
		t.Helper()
		command := append([]string{"api", method, endpoint}, args...)
		command = append(command, "--output-format", "json")
		raw, err := exec.Command("proxmox-backup-debug", command...).Output()
		if err != nil {
			t.Fatalf("fixture API %s %s failed: %v", method, endpoint, err)
		}
		var value map[string]json.RawMessage
		if len(raw) > 0 && json.Unmarshal(raw, &value) != nil {
			t.Fatal("invalid fixture API response")
		}
		return value
	}
	field := func(value map[string]json.RawMessage, name string) string {
		t.Helper()
		var result string
		if json.Unmarshal(value[name], &result) != nil || result == "" {
			t.Fatal("fixture response missing " + name)
		}
		return result
	}
	client := func(config string) *Client {
		t.Helper()
		c, err := NewClient(config)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	grant := func(host string, revoke *bool) string {
		args := []string{"--store", "backup", "--ns", namespace + "/enrollment"}
		if revoke != nil {
			args = append(args, "--revoke-existing-keys", map[bool]string{false: "false", true: "true"}[*revoke])
		}
		value := api("create", "/config/host/"+host+"/enrollment-token", args...)
		raw, _ := json.Marshal(map[string]any{"v": 1, "host_id": host, "server_url": cfg.Server, "server_ca_fingerprint": cfg.Fingerprint, "bootstrap_token": field(value, "token")})
		return "pbsx1." + base64.RawURLEncoding.EncodeToString(raw)
	}
	enroll := func(code string) string {
		t.Helper()
		raw, err := Enroll(code, "Enrollment lifecycle fixture")
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	list := func(c *Client, source string) []int64 {
		t.Helper()
		raw, err := c.ListSnapshots("backup", namespace, source)
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Data []struct {
				Time int64 `json:"backup-time"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &response); err != nil {
			t.Fatal(err)
		}
		result := []int64{}
		for _, item := range response.Data {
			result = append(result, item.Time)
		}
		return result
	}
	control := func(c *Client, host string, allowed bool) {
		t.Helper()
		_, err := c.request(context.Background(), "GET", "/pbsx/agent/next?host-id="+host, nil, false)
		if (err == nil) != allowed {
			t.Fatalf("agent control allowed=%v, got %v", allowed, err)
		}
	}
	api("set", "/config/host-backup/"+jobID, "--enable", "true")
	a := client(first)
	control(a, cfg.HostID, true)
	source := &fsDocuments{root: t.TempDir()}
	writeFixture(t, source, "Documents/keep.txt", "existing backup bytes")
	backup := func(c *Client) {
		t.Helper()
		raw, err := c.BackupJob("backup", namespace, "enrollment-lifecycle", "Key lifecycle fixture", source, nil)
		if err != nil {
			t.Fatal(err)
		}
		var result backupResult
		if json.Unmarshal([]byte(raw), &result) != nil || result.Warning != "" {
			t.Fatal("backup job note was not saved")
		}
	}
	backup(a)
	stamps := list(a, cfg.HostID)
	if len(stamps) != 1 {
		t.Fatal("missing initial snapshot")
	}
	stamp := stamps[0]
	restore := func(c *Client) {
		t.Helper()
		dest := &fsDocuments{root: t.TempDir()}
		if err := c.Restore("backup", namespace, cfg.HostID, stamp, dest, nil); err != nil {
			t.Fatal(err)
		}
		assertContent(t, dest, "Documents/keep.txt", "existing backup bytes")
	}

	// Omission is the safe default. Issuing a grant alone does not change keys.
	codeB := grant(cfg.HostID, nil)
	control(a, cfg.HostID, true)
	b := client(enroll(codeB))
	if a.cfg.AuthID == b.cfg.AuthID || a.cfg.Secret == b.cfg.Secret {
		t.Fatal("enrollments did not get independent keys")
	}
	list(a, cfg.HostID)
	list(b, cfg.HostID)
	restore(a)
	restore(b)
	control(a, cfg.HostID, false)
	control(b, cfg.HostID, true)
	time.Sleep(1100 * time.Millisecond)
	backup(a)
	time.Sleep(1100 * time.Millisecond)
	backup(b)
	t.Log("Default enrollment preserves both keys and existing-group backup/restore; only the latest enrollment controls agent jobs")

	// Job edits refresh every retained key, not only the newest controller.
	api("set", "/config/host-backup/"+jobID, "--enable", "false")
	for _, c := range []*Client{a, b} {
		if _, err := c.ListSnapshots("backup", namespace, cfg.HostID); err == nil {
			t.Fatal("disabled target retained backup access")
		}
	}
	api("set", "/config/host-backup/"+jobID, "--enable", "true")
	list(a, cfg.HostID)
	list(b, cfg.HostID)

	// An unrelated generated host key, plus an independently managed reader.
	other := cfg.HostID + "-other"
	otherJob := jobID + "-b"
	api("create", "/config/host", "--name", other)
	t.Cleanup(func() { api("delete", "/config/host/"+other) })
	api("create", "/config/host-backup", "--host", other, "--id", otherJob, "--sources", "/fixture", "--store", "backup", "--ns", namespace, "--enable", "true")
	t.Cleanup(func() { api("delete", "/config/host-backup/"+otherJob) })
	c := client(enroll(grant(other, nil)))
	backup(c)
	control(c, other, true)
	if _, err := b.ListArchive("backup", namespace, other, list(c, other)[0], ".", 0, 10); err == nil {
		t.Fatal("generated key read another host")
	}
	manualName := "manual-" + namespace
	manualValue := api("create", "/access/users/root@pam/token/"+manualName, "--comment", "Disposable enrollment lifecycle reader")
	t.Cleanup(func() { api("delete", "/access/users/root@pam/token/"+manualName) })
	manualCfg := cfg
	manualCfg.AuthID = field(manualValue, "tokenid")
	manualCfg.Secret = field(manualValue, "value")
	manualCfg.Certificate = ""
	manualCfg.PrivateKey = ""
	api("set", "/access/acl", "--path", "/datastore/backup/"+namespace, "--auth-id", manualCfg.AuthID, "--role", "DatastoreReader", "--propagate", "false")
	manualJSON, _ := json.Marshal(manualCfg)
	manual := client(string(manualJSON))
	restore(manual)
	control(manual, cfg.HostID, false)

	revoke := true
	expired := grant(cfg.HostID, &revoke)
	// Change only the dedicated fixture's grant to exercise real expiry handling.
	grantPath := filepath.Join("/var/lib/proxmox-backup/pbsx-enrollment", cfg.HostID+".json")
	raw, err := os.ReadFile(grantPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if json.Unmarshal(raw, &saved) != nil {
		t.Fatal("bad grant record")
	}
	saved["expires"] = time.Now().Unix() - 1
	raw, _ = json.Marshal(saved)
	if err := os.WriteFile(grantPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Enroll(expired, "Expired fixture"); err == nil {
		t.Fatal("expired enrollment accepted")
	}
	list(a, cfg.HostID)
	list(b, cfg.HostID)
	codeD := grant(cfg.HostID, &revoke)
	// Invalid redemption cannot consume a valid grant or revoke earlier keys.
	payload, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(codeD, "pbsx1."))
	var bad map[string]any
	json.Unmarshal(payload, &bad)
	bad["bootstrap_token"] = "invalid-fixture-token"
	payload, _ = json.Marshal(bad)
	if _, err := Enroll("pbsx1."+base64.RawURLEncoding.EncodeToString(payload), "Invalid fixture"); err == nil {
		t.Fatal("invalid token accepted")
	}
	list(a, cfg.HostID)
	list(b, cfg.HostID)
	control(b, cfg.HostID, true)
	d := client(enroll(codeD))
	for _, old := range []*Client{a, b} {
		if _, err := old.ListSnapshots("backup", namespace, cfg.HostID); err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("revoked key was not rejected by authentication: %v", err)
		}
	}
	restore(d)
	control(d, cfg.HostID, true)
	time.Sleep(1100 * time.Millisecond)
	backup(d)
	restore(manual)
	list(c, other)
	control(c, other, true)
	if _, err := Enroll(codeD, "Replay fixture"); err == nil {
		t.Fatal("one-time grant replay accepted")
	}
	groupJSON, err := exec.Command("proxmox-backup-debug", "api", "get", "/admin/datastore/backup/groups", "--ns", namespace, "--output-format", "json").Output()
	if err != nil {
		t.Fatal(err)
	}
	var groups []struct {
		ID    string `json:"backup-id"`
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(groupJSON, &groups); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range groups {
		if group.ID == cfg.HostID {
			found = true
			if group.Owner != a.cfg.AuthID {
				t.Fatal("enrollment changed the backup group owner")
			}
		}
	}
	if !found {
		t.Fatal("original backup group missing")
	}
	t.Log("Opt-in revocation invalidates only this host's earlier generated keys; old snapshots, unrelated generated keys and manual API keys remain usable; expired, invalid and replayed grants are rejected")
}
