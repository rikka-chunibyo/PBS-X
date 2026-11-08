//go:build integration

package mobilecore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run only through test-live-pbs.sh on the disposable PBS VM. The temporary
// grant is added while holding the ordinary ACL writer lock and removed afterward.
func TestLiveHostDeletionSerializesACLUpdates(t *testing.T) {
	file := os.Getenv("PBSX_ANDROID_TEST_ENROLLMENT")
	ns := os.Getenv("PBSX_ANDROID_TEST_NAMESPACE")
	if file == "" || !strings.HasPrefix(ns, "android-smoke-") {
		t.Skip("requires disposable PBS fixture")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	config, err := Enroll(string(raw), "ACL serialization fixture")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	host := fmt.Sprintf("acl-delete-%x", time.Now().UnixNano())
	marker := host + "-m"
	api := func(method, path string, args ...string) map[string]json.RawMessage {
		t.Helper()
		cmd := append([]string{"api", method, path}, args...)
		output, err := exec.Command("proxmox-backup-debug", append(cmd, "--output-format", "json")...).Output()
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				t.Fatalf("fixture %s %s: %s", method, path, e.Stderr)
			}
			t.Fatal(err)
		}
		var value map[string]json.RawMessage
		_ = json.Unmarshal(output, &value)
		return value
	}
	api("create", "/config/host", "--name", host)
	deleted := false
	defer func() {
		if !deleted {
			api("delete", "/config/host/"+host)
		}
	}()
	api("create", "/config/host", "--name", marker)
	defer api("delete", "/config/host/"+marker)
	grant := api("create", "/config/host/"+host+"/enrollment-token", "--store", "backup", "--ns", ns+"/enrollment")
	var token string
	_ = json.Unmarshal(grant["token"], &token)
	payload, _ := json.Marshal(map[string]any{"v": 1, "host_id": host, "server_url": client.cfg.Server, "server_ca_fingerprint": client.cfg.Fingerprint, "bootstrap_token": token})
	enrolled, err := Enroll("pbsx1."+base64.RawURLEncoding.EncodeToString(payload), "Host to delete")
	if err != nil {
		t.Fatal(err)
	}
	old, err := NewClient(enrolled)
	if err != nil {
		t.Fatal(err)
	}
	// Using the real lock proves deletion waits for an ongoing ACL transaction.
	lock, err := os.OpenFile("/etc/proxmox-backup/.acl.lck", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "proxmox-backup-debug", "api", "delete", "/config/host/"+host)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("host deletion ignored the ACL lock: %v: %s", err, output.String())
	case <-time.After(750 * time.Millisecond):
	}
	whileLocked, err := os.ReadFile("/etc/proxmox-backup/acl.cfg")
	if err != nil || !bytes.Contains(whileLocked, []byte("/host/"+host+":")) {
		t.Fatal("host deletion modified ACLs while another writer held the lock", err)
	}
	// Simulate the pending administrator transaction committing a grant unrelated
	// to the deleted host. Only this fixture's new ACL line is appended.
	line := fmt.Sprintf("acl:0:/host/%s:%s:HostAudit\n", marker, client.cfg.AuthID)
	acl, err := os.OpenFile("/etc/proxmox-backup/acl.cfg", os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = acl.WriteString(line)
	if err == nil {
		err = acl.Sync()
	}
	_ = acl.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("host deletion failed/deadlocked after lock release: %v: %s", err, output.String())
	}
	deleted = true
	data, err := os.ReadFile("/etc/proxmox-backup/acl.cfg")
	if err != nil || !bytes.Contains(data, []byte("/host/"+marker)) {
		t.Fatal("concurrent ACL grant was lost", err)
	}
	if _, err := old.ListSnapshots("backup", ns+"/enrollment", host); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatal("host credential cleanup did not revoke its generated key", err)
	}
	t.Log("Host deletion waited for the ACL writer, preserved its new grant, then completed credential cleanup without deadlock")
}
