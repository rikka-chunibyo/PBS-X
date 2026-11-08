//go:build integration

package mobilecore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Uses only unique disposable hosts/namespaces on the test PBS VM.
func TestLiveEnrollmentNamespace(t *testing.T) {
	file := os.Getenv("PBSX_ANDROID_TEST_ENROLLMENT")
	if file == "" {
		t.Skip("requires disposable PBS fixture")
	}
	initial, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Enroll(string(initial), "Namespace fixture bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := NewClient(raw)
	if err != nil {
		t.Fatal(err)
	}
	host := fmt.Sprintf("Ns-Test-%x", time.Now().UnixNano())
	other := strings.ToLower(host) + "-b"
	ns := strings.ToLower(host)
	api := func(method, endpoint string, args ...string) map[string]json.RawMessage {
		t.Helper()
		command := append([]string{"api", method, endpoint}, args...)
		out, err := exec.Command("proxmox-backup-debug", append(command, "--output-format", "json")...).Output()
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				t.Fatalf("fixture %s %s: %s", method, endpoint, e.Stderr)
			}
			t.Fatal(err)
		}
		var result map[string]json.RawMessage
		if len(out) > 0 {
			_ = json.Unmarshal(out, &result)
		}
		return result
	}
	cleanup := func(method, endpoint string, args ...string) {
		command := append([]string{"api", method, endpoint}, args...)
		if out, err := exec.Command("proxmox-backup-debug", append(command, "--output-format", "json")...).CombinedOutput(); err != nil {
			t.Errorf("cleanup %s: %v: %s", endpoint, err, out)
		}
	}
	api("create", "/config/host", "--name", host)
	defer cleanup("delete", "/config/host/"+host)
	defer func() { cleanup("delete", "/admin/datastore/backup/namespace", "--ns", ns, "--delete-groups", "true") }()
	grant := func(name, namespace string) string {
		t.Helper()
		args := []string{}
		if namespace != "" {
			args = append(args, "--store", "backup", "--ns", namespace)
		}
		result := api("create", "/config/host/"+name+"/enrollment-token", args...)
		var token, store, approved string
		_ = json.Unmarshal(result["token"], &token)
		_ = json.Unmarshal(result["store"], &store)
		_ = json.Unmarshal(result["ns"], &approved)
		if token == "" || store != "backup" || approved != ns {
			t.Fatal("incorrect enrollment destination")
		}
		// Deliberately tamper with metadata. The bootstrap response is authoritative.
		payload, _ := json.Marshal(map[string]any{"v": 1, "host_id": name, "bootstrap_token": token, "server_url": fixture.cfg.Server, "server_ca_fingerprint": fixture.cfg.Fingerprint, "default_namespace": "unapproved"})
		return "pbsx1." + base64.RawURLEncoding.EncodeToString(payload)
	}
	enroll := func(code string) *Client {
		t.Helper()
		config, err := Enroll(code, "Samsung namespace fixture")
		if err != nil {
			t.Fatal(err)
		}
		c, err := NewClient(config)
		if err != nil {
			t.Fatal(err)
		}
		if c.cfg.DefaultNamespace == nil || *c.cfg.DefaultNamespace != ns || c.cfg.DefaultStore != "backup" {
			t.Fatal("client default differs from granted namespace")
		}
		return c
	}
	first := enroll(grant(host, ""))
	jobs, err := exec.Command("proxmox-backup-debug", "api", "get", "/config/host-backup", "--output-format", "json").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(jobs), host) {
		t.Fatal("fixture unexpectedly has a server job")
	}
	stat, err := exec.Command("stat", "-c", "%U:%G", "/media/backup/ns/"+ns).Output()
	if err != nil || strings.TrimSpace(string(stat)) != "backup:backup" {
		t.Fatal("created namespace is not owned by backup")
	}
	source := &fsDocuments{root: t.TempDir()}
	writeFixture(t, source, "Photos/keep.txt", "existing namespace bytes")
	if _, err := first.BackupGroupJob("backup", ns, "photos", "manual", "Phone photos", source, nil); err != nil {
		t.Fatal(err)
	}
	snapshots, err := first.ListGroupSnapshots("backup", ns, "host", "photos")
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Data []struct {
			Time int64 `json:"backup-time"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(snapshots), &listing); err != nil || len(listing.Data) != 1 {
		t.Fatal("snapshot not visible")
	}
	// A different host can explicitly enroll into this non-empty namespace.
	api("create", "/config/host", "--name", other)
	defer cleanup("delete", "/config/host/"+other)
	second := enroll(grant(other, ns))
	dest := &fsDocuments{root: t.TempDir()}
	if _, err := second.RestoreGroupSelected("backup", ns, "host", "photos", listing.Data[0].Time, `["."]`, "fail", dest, nil); err != nil {
		t.Fatal(err)
	}
	assertContent(t, dest, "Photos/keep.txt", "existing namespace bytes")
	catalogue, err := second.ListAccessibleBackups()
	if err != nil {
		t.Fatal(err)
	}
	var visible backupCatalogue
	if json.Unmarshal([]byte(catalogue), &visible) != nil || len(visible.Groups) != 1 || visible.Groups[0].Namespace != ns || visible.Groups[0].ID != "photos" {
		t.Fatal("Restore catalogue did not discover the shared namespace backup")
	}
	if _, err := first.ListGroupSnapshots("backup", ns, "host", "photos"); err != nil {
		t.Fatal("existing key lost access")
	}
	if _, err := second.ListGroupSnapshots("backup", "", "host", "photos"); err == nil {
		t.Fatal("key can read outside its namespace")
	}
	// Device keys have no namespace-management privilege.
	body, _ := json.Marshal(map[string]any{"name": "forbidden", "parent": ns})
	if _, err := first.request(context.Background(), "POST", "/admin/datastore/backup/namespace", body, true); err == nil {
		t.Fatal("device key can create namespaces")
	}
	// A HostAdmin token must not be able to grant root-owned device tokens arbitrary storage access.
	tokenName := "restricted-" + strings.ToLower(host)
	result := api("create", "/access/users/root@pam/token/"+tokenName)
	defer cleanup("delete", "/access/users/root@pam/token/"+tokenName)
	var tokenID, secret string
	_ = json.Unmarshal(result["tokenid"], &tokenID)
	_ = json.Unmarshal(result["value"], &secret)
	api("set", "/access/acl", "--path", "/host/"+host, "--auth-id", tokenID, "--role", "HostAdmin")
	defer cleanup("set", "/access/acl", "--path", "/host/"+host, "--auth-id", tokenID, "--role", "HostAdmin", "--delete", "true")
	restrictedCfg := fixture.cfg
	restrictedCfg.AuthID, restrictedCfg.Secret = tokenID, secret
	encoded, _ := json.Marshal(restrictedCfg)
	restricted, err := NewClient(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restricted.request(context.Background(), "GET", "/config/host/"+host, nil, false); err != nil {
		t.Fatal("restricted fixture did not authenticate with HostAdmin permission", err)
	}
	body, _ = json.Marshal(map[string]any{"store": "backup", "ns": ns + "/unapproved"})
	if _, err := restricted.request(context.Background(), "POST", "/config/host/"+host+"/enrollment-token", body, false); err == nil {
		t.Fatal("HostAdmin can escalate storage permissions")
	}
	// Reissuing a code repairs retained generated keys without rotating their secret.
	api("set", "/access/acl", "--path", "/datastore/backup/"+ns, "--auth-id", first.cfg.AuthID, "--role", "DatastoreBackup", "--delete", "true")
	grant(host, ns)
	api("create", "/admin/datastore/backup/namespace", "--name", "child", "--parent", ns)
	if _, err := first.BackupGroupJob("backup", ns+"/child", "documents", "manual", "Retained phone key", source, nil); err != nil {
		t.Fatal("retained key was not repaired or namespace access did not propagate", err)
	}
	t.Log("No server job required; normalized namespace created; non-empty namespace reused; cross-host restore, retained-key repair, subtree access and permission boundaries passed")
}
