//go:build integration

package mobilecore

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/datastore"
)

func TestLiveEncryption(t *testing.T) {
	enrollment, ns, job := os.Getenv("PBSX_ANDROID_TEST_ENROLLMENT"), os.Getenv("PBSX_ANDROID_TEST_NAMESPACE"), os.Getenv("PBSX_ANDROID_TEST_DISABLED_JOB")
	if enrollment == "" || !strings.HasPrefix(ns, "android-smoke-") || job != ns {
		t.Skip("requires a disposable test-VM fixture")
	}
	code, err := os.ReadFile(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Enroll(string(code), "Encryption fixture")
	if err != nil {
		t.Fatal(err)
	}
	var cfg configuration
	if err = json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.HostID != ns {
		t.Fatal("unexpected fixture identity")
	}
	out, err := exec.Command("proxmox-backup-debug", "api", "set", "/config/host-backup/"+job, "--enable", "true").CombinedOutput()
	if err != nil {
		t.Fatalf("enable fixture: %v %s", err, out)
	}
	key, err := GenerateEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg.EncryptionEnabled = true
	cfg.EncryptionKey = key
	cfg.DecryptionKeys = []string{key}
	makeClient := func(value configuration) *Client {
		t.Helper()
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		client, e := NewClient(string(raw))
		if e != nil {
			t.Fatal(e)
		}
		return client
	}
	client := makeClient(cfg)
	src := &fsDocuments{root: t.TempDir()}
	payload := bytes.Repeat([]byte("private Android encryption fixture; "), 20000)
	if err = os.MkdirAll(filepath.Join(src.root, "Photos"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(src.root, "Photos", "secret.txt"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Backup("backup", ns, src, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	progress := &uploadSpy{}
	if _, err = client.Backup("backup", ns, src, progress); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt64(&progress.bytes) != 0 {
		t.Fatalf("unchanged encrypted snapshot uploaded %d bytes", progress.bytes)
	}
	raw, err = client.ListSnapshots("backup", ns, cfg.HostID)
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Data []struct {
			Time int64 `json:"backup-time"`
		}
	}
	if err = json.Unmarshal([]byte(raw), &listing); err != nil || len(listing.Data) != 2 {
		t.Fatalf("snapshot list: %v count=%d", err, len(listing.Data))
	}
	stamp := listing.Data[0].Time
	pbsCfg, _ := client.pbsConfig("backup", ns)
	reader := backupproxy.NewPBSReader(pbsCfg, "host", cfg.HostID, stamp)
	if err = reader.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	manifestRaw, err := reader.DownloadFile("index.json.blob")
	if err != nil {
		t.Fatal(err)
	}
	manifest, crypt, err := client.snapshotManifest(manifestRaw, "host", cfg.HostID, stamp)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range manifest.Files {
		if strings.HasSuffix(f.Filename, ".didx") && f.CryptMode != "encrypt" {
			t.Fatal("archive was not encrypted")
		}
	}
	idxRaw, err := reader.DownloadFile("root.ppxar.didx")
	if err != nil {
		t.Fatal(err)
	}
	idx, err := datastore.ParseDynamicIndex(idxRaw)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := reader.DownloadChunk(idx.Entry(0).Digest)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(chunk, []byte("private Android encryption fixture")) {
		t.Fatal("plaintext visible in stored chunk")
	}
	if _, err = datastore.DecodeBlob(nil, chunk); err == nil {
		t.Fatal("encrypted chunk readable without key")
	}
	if _, err = datastore.DecodeEncryptedBlob(nil, chunk, crypt); err != nil {
		t.Fatal(err)
	}
	reader.Close()
	// Independent Rust-client verification of the PBS encryption wire format.
	cliDir := t.TempDir()
	rawKey, _ := hex.DecodeString(key)
	now := time.Now().UTC().Format(time.RFC3339)
	keyJSON, _ := json.Marshal(datastore.KeyConfig{Data: rawKey, Created: now, Modified: now, Fingerprint: datastore.FormatFingerprint(crypt.Fingerprint())})
	keyFile := filepath.Join(cliDir, "key.json")
	if err = os.WriteFile(keyFile, keyJSON, 0600); err != nil {
		t.Fatal(err)
	}
	cli := func(args ...string) {
		t.Helper()
		cmd := exec.Command("proxmox-backup-client", args...)
		cmd.Env = append(os.Environ(), "PBS_REPOSITORY="+cfg.AuthID+"@"+strings.TrimPrefix(cfg.Server, "https://")+":backup", "PBS_PASSWORD="+cfg.Secret, "PBS_FINGERPRINT="+colonFingerprint(cfg.Fingerprint), "HOME="+cliDir, "XDG_CONFIG_HOME="+cliDir, "XDG_CACHE_HOME="+cliDir)
		output, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("standard PBS client interoperability: %v: %s", e, output)
		}
	}
	cliOut := filepath.Join(cliDir, "rust-restore")
	cli("restore", fmt.Sprintf("host/%s/%s", cfg.HostID, time.Unix(stamp, 0).UTC().Format("2006-01-02T15:04:05Z")), "root.pxar", cliOut, "--ns", ns, "--keyfile", keyFile)
	cliData, e := os.ReadFile(filepath.Join(cliOut, "Photos", "secret.txt"))
	if e != nil || !bytes.Equal(cliData, payload) {
		t.Fatal("standard PBS restore differs", e)
	}
	restoreConfig := cfg
	restoreConfig.HostID = "replacement-device"
	restoreConfig.EncryptionEnabled = false
	restoreConfig.EncryptionKey = ""
	replacement := makeClient(restoreConfig)
	dst := &fsDocuments{root: t.TempDir()}
	if err = replacement.Restore("backup", ns, cfg.HostID, stamp, dst, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst.root, "Photos", "secret.txt"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("cross-device encrypted restore differs", err)
	}
	for _, keys := range [][]string{nil, {strings.Repeat("ab", 32)}} {
		bad := restoreConfig
		bad.DecryptionKeys = keys
		denied := makeClient(bad)
		untouched := &fsDocuments{root: t.TempDir()}
		if err = denied.Restore("backup", ns, cfg.HostID, stamp, untouched, nil); err == nil {
			t.Fatal("missing or wrong key accepted")
		}
		entries, _ := os.ReadDir(untouched.root)
		if len(entries) != 0 {
			t.Fatal("wrong-key restore modified destination")
		}
	}
	time.Sleep(1100 * time.Millisecond)
	cli("backup", "root.pxar:"+src.root, "--backup-id", cfg.HostID, "--ns", ns, "--keyfile", keyFile, "--change-detection-mode", "data")
	raw, err = replacement.ListSnapshots("backup", ns, cfg.HostID)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(raw), &listing); err != nil {
		t.Fatal(err)
	}
	latest := int64(0)
	for _, snapshot := range listing.Data {
		if snapshot.Time > latest {
			latest = snapshot.Time
		}
	}
	rustTarget := &fsDocuments{root: t.TempDir()}
	if err = replacement.Restore("backup", ns, cfg.HostID, latest, rustTarget, nil); err != nil {
		t.Fatal("restore standard PBS encrypted backup", err)
	}
	cliData, e = os.ReadFile(filepath.Join(rustTarget.root, "Photos", "secret.txt"))
	if e != nil || !bytes.Equal(cliData, payload) {
		t.Fatal("standard PBS encrypted backup differs", e)
	}
	t.Log("Bidirectional encrypted interoperability with the standard Rust proxmox-backup-client passed")
	time.Sleep(1100 * time.Millisecond)
	next, _ := GenerateEncryptionKey()
	cfg.EncryptionKey = next
	cfg.DecryptionKeys = []string{key, next}
	rotated := makeClient(cfg)
	if _, err = rotated.Backup("backup", ns, src, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = rotated.ListArchive("backup", ns, cfg.HostID, stamp, ".", 0, 50); err != nil {
		t.Fatal("historical snapshot after rotation", err)
	}
	t.Log("Encrypted upload, signed manifest, unreadable stored chunks, unchanged deduplication, replacement-device restore, wrong/missing-key refusal and historical key rotation passed")
}

func colonFingerprint(value string) string {
	raw, err := hex.DecodeString(strings.ReplaceAll(value, ":", ""))
	if err != nil || len(raw) != 32 {
		return value
	}
	var fingerprint [32]byte
	copy(fingerprint[:], raw)
	return datastore.FormatFingerprint(fingerprint)
}
