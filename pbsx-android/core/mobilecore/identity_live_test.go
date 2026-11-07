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

// Real PBS upload/reader checks. Only run through the disposable test-VM fixture.
func TestLiveBackupIdentities(t *testing.T) {
	enrollment, ns, job := os.Getenv("PBSX_ANDROID_TEST_ENROLLMENT"), os.Getenv("PBSX_ANDROID_TEST_NAMESPACE"), os.Getenv("PBSX_ANDROID_TEST_DISABLED_JOB")
	if enrollment == "" || ns == "" || job == "" {
		t.Skip("requires disposable PBS fixture")
	}
	code, err := os.ReadFile(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Enroll(string(code), "Backup identity fixture")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.cfg
	if !strings.HasPrefix(ns, "android-smoke-") || ns != cfg.HostID || job != ns {
		t.Fatal("refusing non-disposable fixture")
	}
	api := func(method, endpoint string, args ...string) map[string]json.RawMessage {
		t.Helper()
		command := append([]string{"api", method, endpoint}, args...)
		command = append(command, "--output-format", "json")
		result, err := exec.Command("proxmox-backup-debug", command...).Output()
		if err != nil {
			if endpoint == "/config/host-backup" || strings.Contains(endpoint, "/namespace") {
				if exit, ok := err.(*exec.ExitError); ok {
					t.Fatalf("fixture API %s %s: %s", method, endpoint, exit.Stderr)
				}
			}
			t.Fatalf("fixture API %s %s: %v", method, endpoint, err)
		}
		var response map[string]json.RawMessage
		if len(result) > 0 {
			_ = json.Unmarshal(result, &response)
		}
		return response
	}
	api("set", "/config/host-backup/"+job, "--enable", "true")
	photos, documents := &fsDocuments{root: t.TempDir()}, &fsDocuments{root: t.TempDir()}
	for source, path := range map[*fsDocuments]string{photos: "Photos/camera.txt", documents: "Documents/note.txt"} {
		if err := os.MkdirAll(filepath.Dir(source.full(path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source.full(path), []byte(path), 0600); err != nil {
			t.Fatal(err)
		}
	}
	backup := func(c *Client, namespace, id, jobID, label string, source Documents) {
		t.Helper()
		result, err := c.BackupGroupJob("backup", namespace, id, jobID, label, source, nil)
		if err != nil {
			t.Fatal(err)
		}
		var saved backupResult
		if json.Unmarshal([]byte(result), &saved) != nil || saved.Warning != "" || !strings.HasPrefix(saved.Snapshot, "host/"+id+"/") {
			t.Fatal("backup identity or notes lost")
		}
	}
	snapshots := func(c *Client, namespace, id string) []int64 {
		t.Helper()
		result, err := c.ListGroupSnapshots("backup", namespace, "host", id)
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Data []struct {
				Time    int64  `json:"backup-time"`
				Comment string `json:"comment"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(result), &response) != nil {
			t.Fatal("invalid snapshot listing")
		}
		times := make([]int64, 0, len(response.Data))
		for _, snapshot := range response.Data {
			times = append(times, snapshot.Time)
			if !strings.Contains(snapshot.Comment, "PBS-X Android:") {
				t.Fatal("job name missing from notes")
			}
		}
		return times
	}
	restore := func(c *Client, namespace, id string, stamp int64, present, absent string) {
		t.Helper()
		dest := &fsDocuments{root: t.TempDir()}
		if _, err := c.RestoreGroupSelected("backup", namespace, "host", id, stamp, `["."]`, "fail", dest, nil); err != nil {
			t.Fatal(err)
		}
		assertContent(t, dest, present, present)
		if _, err := os.Stat(dest.full(absent)); !os.IsNotExist(err) {
			t.Fatal("shared history merged unrelated sources")
		}
	}
	backup(client, ns, "shared-photos", "scheduled-job", "Scheduled photos", photos)
	first := snapshots(client, ns, "shared-photos")[0]
	time.Sleep(1100 * time.Millisecond)
	backup(client, ns, "shared-photos", "manual-job", "Manual documents", documents)
	times := snapshots(client, ns, "shared-photos")
	if len(times) != 2 {
		t.Fatal("equal backup IDs did not share a history")
	}
	latest := times[0]
	if latest == first {
		latest = times[1]
	}
	restore(client, ns, "shared-photos", first, "Photos/camera.txt", "Documents/note.txt")
	restore(client, ns, "shared-photos", latest, "Documents/note.txt", "Photos/camera.txt")
	time.Sleep(1100 * time.Millisecond)
	backup(client, ns, "shared-photos", "scheduled-job", "Renamed cosmetic job", photos)
	backup(client, ns, "separate-documents", "separate-job", "Documents", documents)
	if len(snapshots(client, ns, "shared-photos")) != 3 || len(snapshots(client, ns, "separate-documents")) != 1 {
		t.Fatal("backup ID separation or cosmetic rename changed history")
	}
	t.Log("Different jobs share snapshots by Backup ID without merging contents; cosmetic renames preserve history; another ID creates another group")

	// An administrator may add a namespace directly to the generated token.
	extra := ns + "/manual"
	api("create", "/admin/datastore/backup/namespace", "--name", "manual", "--parent", ns)
	t.Cleanup(func() { api("delete", "/admin/datastore/backup/namespace", "--ns", extra, "--delete-groups", "true") })
	if _, err := client.ListGroupSnapshots("backup", extra, "host", "manual-share"); err == nil {
		t.Fatal("key accessed an ungranted namespace")
	}
	if _, err := client.BackupGroupJob("backup", extra, "manual-share", "test-job", "Denied", photos, nil); err == nil {
		t.Fatal("key uploaded outside its namespace")
	}
	api("set", "/access/acl", "--path", "/datastore/backup/"+extra, "--auth-id", cfg.AuthID, "--role", "DatastoreBackup", "--propagate", "false")
	backup(client, extra, "manual-share", "manual-grant", "Manual namespace", photos)
	extraTime := snapshots(client, extra, "manual-share")[0]
	restore(client, extra, "manual-share", extraTime, "Photos/camera.txt", "Documents/note.txt")
	api("set", "/config/host-backup/"+job, "--enable", "false")
	if _, err := client.ListGroupSnapshots("backup", ns, "host", "shared-photos"); err == nil {
		t.Fatal("disabled job retained its automatic grant")
	}
	snapshots(client, extra, "manual-share")
	api("set", "/config/host-backup/"+job, "--enable", "true")
	snapshots(client, ns, "shared-photos")
	snapshots(client, extra, "manual-share")
	// A generated key with an administrator-added read role can restore groups
	// owned by an unrelated token. It still cannot append to that owner's group.
	manualName := "id-test-" + ns
	manualGrant := api("create", "/access/users/root@pam/token/"+manualName)
	manualCfg := cfg
	if json.Unmarshal(manualGrant["tokenid"], &manualCfg.AuthID) != nil || json.Unmarshal(manualGrant["value"], &manualCfg.Secret) != nil {
		t.Fatal("missing manual fixture credentials")
	}
	api("set", "/access/acl", "--path", "/datastore/backup/"+extra, "--auth-id", manualCfg.AuthID, "--role", "DatastoreBackup", "--propagate", "false")
	t.Cleanup(func() {
		api("set", "/access/acl", "--path", "/datastore/backup/"+extra, "--auth-id", manualCfg.AuthID, "--role", "DatastoreBackup", "--delete", "true")
		api("delete", "/access/users/root@pam/token/"+manualName)
	})
	manualJSON, _ := json.Marshal(manualCfg)
	manual, err := NewClient(string(manualJSON))
	if err != nil {
		t.Fatal(err)
	}
	backup(manual, extra, "foreign-documents", "foreign-job", "Other owner", documents)
	foreignTime := snapshots(manual, extra, "foreign-documents")[0]
	if _, err := client.ListGroupArchive("backup", extra, "host", "foreign-documents", foreignTime, ".", 0, 100); err == nil {
		t.Fatal("backup-only key read another token's group")
	}
	api("set", "/access/acl", "--path", "/datastore/backup/"+extra, "--auth-id", cfg.AuthID, "--role", "DatastoreReader", "--propagate", "false")
	restore(client, extra, "foreign-documents", foreignTime, "Documents/note.txt", "Photos/camera.txt")
	if _, err := client.BackupGroupJob("backup", extra, "foreign-documents", "wrong-owner", "Must be denied", photos, nil); err == nil {
		t.Fatal("read privilege bypassed append ownership")
	}
	api("set", "/access/acl", "--path", "/datastore/backup/"+extra, "--auth-id", cfg.AuthID, "--role", "DatastoreReader", "--delete", "true")
	api("set", "/access/acl", "--path", "/datastore/backup/"+extra, "--auth-id", cfg.AuthID, "--role", "DatastoreBackup", "--delete", "true")
	if _, err := client.ListGroupSnapshots("backup", extra, "host", "manual-share"); err == nil {
		t.Fatal("removed manual grant remained usable")
	}
	t.Log("Manual namespace grant immediately enables backup/restore, survives job edits, and removal immediately denies access")

	// The administrator can provision nested device namespaces through the job
	// API. The resulting device token itself cannot create namespaces.
	provisionJob := job + "-p"
	api("create", "/config/host-backup", "--id", provisionJob, "--host", cfg.HostID, "--store", "backup", "--ns", ns+"/nested/device", "--sources", "/fixture", "--enable", "false")
	t.Cleanup(func() { api("delete", "/config/host-backup/"+provisionJob) })
	api("set", "/config/host-backup/"+provisionJob, "--enable", "true")
	backup(client, ns+"/nested/device", "provisioned-files", "provision-job", "Provisioned namespace", photos)
	restore(client, ns+"/nested/device", "provisioned-files", snapshots(client, ns+"/nested/device", "provisioned-files")[0], "Photos/camera.txt", "Documents/note.txt")
	payload, _ := json.Marshal(map[string]string{"name": "not-allowed", "parent": ns})
	if _, err := client.request(context.Background(), "POST", "/admin/datastore/backup/namespace", payload, true); err == nil {
		t.Fatal("device key acquired namespace creation privileges")
	}

	// A retained enrollment can append to arbitrary backup IDs owned by an
	// earlier key for this device, without rewriting recorded group ownership.
	grant := api("create", "/config/host/"+cfg.HostID+"/enrollment-token")
	var token string
	if json.Unmarshal(grant["token"], &token) != nil || token == "" {
		t.Fatal("missing enrollment token")
	}
	encoded, _ := json.Marshal(map[string]any{"v": 1, "host_id": cfg.HostID, "server_url": cfg.Server, "server_ca_fingerprint": cfg.Fingerprint, "bootstrap_token": token})
	nextJSON, err := Enroll("pbsx1."+base64.RawURLEncoding.EncodeToString(encoded), "Retained identity fixture")
	if err != nil {
		t.Fatal(err)
	}
	next, err := NewClient(nextJSON)
	if err != nil {
		t.Fatal(err)
	}
	restore(next, ns, "shared-photos", first, "Photos/camera.txt", "Documents/note.txt")
	time.Sleep(1100 * time.Millisecond)
	backup(next, ns, "shared-photos", "manual-job", "Retained key", documents)
	if len(snapshots(client, ns, "shared-photos")) != 4 {
		t.Fatal("retained enrollments lost shared backup history")
	}
	if next.cfg.HostID != cfg.HostID || client.cfg.HostID != cfg.HostID {
		t.Fatal("storage ID replaced device identity")
	}
	t.Log("Nested namespace provisioning requires administrator permission; retained keys share arbitrary IDs without adopting device identity")
}
