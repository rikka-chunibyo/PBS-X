package mobilecore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-plus/pxar/transfer"
	"github.com/pbs-plus/pxar/vfs"
)

// fsDocuments models Android's ownership-transferred file descriptors on Linux.
type fsDocuments struct {
	root         string
	statHook     func(string, *documentEntry)
	listOverride string
	opened       func()
}

func (d *fsDocuments) full(p string) string { return filepath.Join(d.root, filepath.FromSlash(p)) }
func (d *fsDocuments) StatJSON(p string) (string, error) {
	st, err := os.Stat(d.full(p))
	if err != nil {
		return "", err
	}
	e := documentEntry{Name: st.Name(), Size: st.Size(), Modified: st.ModTime().UnixMilli(), Directory: st.IsDir()}
	if e.Directory {
		e.Size = 0
	}
	if d.statHook != nil {
		d.statHook(p, &e)
	}
	raw, err := json.Marshal(e)
	return string(raw), err
}
func (d *fsDocuments) ListJSON(p string) (string, error) {
	if d.listOverride != "" {
		return d.listOverride, nil
	}
	entries, err := os.ReadDir(d.full(p))
	if err != nil {
		return "", err
	}
	items := []documentEntry{}
	for _, e := range entries {
		raw, err := d.StatJSON(filepath.ToSlash(filepath.Join(p, e.Name())))
		if err != nil {
			return "", err
		}
		var item documentEntry
		json.Unmarshal([]byte(raw), &item)
		items = append(items, item)
	}
	out, err := json.Marshal(items)
	return string(out), err
}
func (d *fsDocuments) OpenRead(p string) (int64, error) {
	fd, err := syscall.Open(d.full(p), syscall.O_RDONLY, 0)
	if err == nil && d.opened != nil {
		d.opened()
	}
	return int64(fd), err
}
func (d *fsDocuments) OpenWrite(p string) (int64, error) {
	fd, err := syscall.Open(d.full(p), syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	return int64(fd), err
}
func (d *fsDocuments) InspectJSON(p string) (string, error) {
	raw, err := d.StatJSON(p)
	if os.IsNotExist(err) {
		return `{"exists":false}`, nil
	}
	if err != nil {
		return "", err
	}
	var e map[string]any
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return "", err
	}
	e["exists"] = true
	out, err := json.Marshal(e)
	return string(out), err
}
func (d *fsDocuments) OpenReplace(p string) (int64, error) {
	fd, err := syscall.Open(d.full(p), syscall.O_WRONLY|syscall.O_TRUNC, 0600)
	return int64(fd), err
}
func (d *fsDocuments) MakeDirectory(p string) error { return os.Mkdir(d.full(p), 0700) }

func TestDocumentArchiveRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := &fsDocuments{root: t.TempDir()}
	dst := &fsDocuments{root: t.TempDir()}
	files := map[string][]byte{"Photos/猫.jpg": bytes.Repeat([]byte("photo-data"), 20000), "Photos/empty": {}, "Documents/report.txt": []byte("first version"), "Documents/nested/a.txt": []byte("nested"), "Documents/space name.txt": []byte("spaces")}
	for name, data := range files {
		if err := os.MkdirAll(filepath.Dir(src.full(name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(src.full(name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _ := buzhash.NewConfig(4096)
	storeDir := t.TempDir()
	store, err := backupproxy.NewLocalStore(storeDir, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	backup := func(stamp int64) {
		t.Helper()
		spy := &detailSpy{}
		report := newTransferReporter(ctx, spy)
		_, err := backupproxy.NewServer(backupproxy.NewLocalClient(&documentFS{docs: src, ctx: ctx, report: report}), store).RunBackupWithMode(ctx, ".", backupproxy.BackupConfig{BackupType: datastore.BackupHost, BackupID: "android-test", BackupTime: stamp, ChunkConfig: cfg, Compress: true, DetectionMode: backupproxy.DetectionData, StrictMetadata: true, OnUploadProgress: func(p backupproxy.UploadProgress) { report.uploaded(int64(p.UploadedBytes)) }})
		report.close()
		if err != nil {
			t.Fatal(err)
		}
		var expected int64
		for _, value := range files {
			expected += int64(len(value))
		}
		state := spy.latest()
		if state.Bytes != expected || state.Files != int64(len(files)) {
			t.Fatalf("real backup omitted logical progress: %+v", state)
		}
		if stamp == 1700000001 && state.UploadedBytes != 0 {
			t.Fatalf("unchanged backup uploaded new bytes: %+v", state)
		}
	}
	backup(1700000000)
	chunks := chunkCount(t, storeDir)
	if chunks == 0 {
		t.Fatal("backup did not create chunks")
	}
	backup(1700000001)
	if got := chunkCount(t, storeDir); got != chunks {
		t.Fatalf("unchanged backup added chunks: %d -> %d", chunks, got)
	}
	files["Documents/report.txt"] = []byte("second version")
	files["Documents/new.txt"] = []byte("new file")
	delete(files, "Documents/space name.txt")
	os.Remove(src.full("Documents/space name.txt"))
	for _, name := range []string{"Documents/report.txt", "Documents/new.txt"} {
		if err := os.WriteFile(src.full(name), files[name], 0600); err != nil {
			t.Fatal(err)
		}
	}
	backup(1700000002)
	meta, err := os.ReadFile(filepath.Join(storeDir, "root.mpxar.didx"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(storeDir, "root.ppxar.didx"))
	if err != nil {
		t.Fatal(err)
	}
	chunksStore, err := datastore.NewChunkStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := transfer.NewSplitReader(meta, payload, datastore.NewChunkStoreSource(chunksStore))
	if err != nil {
		t.Fatal(err)
	}
	fs := vfs.NewLocalFS(archive)
	defer fs.Close()
	stats, err := restoreSelected(ctx, fs, []string{"."}, "fail", dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(dst.full(name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("restore %q: %v or content mismatch", name, err)
		}
	}
	if _, err := os.Stat(dst.full("Documents/space name.txt")); !os.IsNotExist(err) {
		t.Fatal("deleted source appeared in latest restore")
	}
	if stats.Files != int64(len(files)) {
		t.Fatalf("restored %d files, expected %d", stats.Files, len(files))
	}
	// A second restore to the same destination must refuse to overwrite it.
	if _, err := restoreSelected(ctx, fs, []string{"."}, "fail", dst, nil); err == nil {
		t.Fatal("restore overwrote existing destination")
	}
}
func chunkCount(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	filepath.WalkDir(filepath.Join(dir, ".chunks"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	return count
}

func TestDocumentRejectsUnsafeAndDuplicateNames(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/b", ".", "..", "a\\b", ""} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal([]documentEntry{{Name: name}})
			fs := &documentFS{ctx: context.Background(), docs: &fsDocuments{listOverride: string(raw)}}
			if _, err := fs.ReadDir("."); err == nil {
				t.Fatal("accepted unsafe name")
			}
		})
	}
	fs := &documentFS{ctx: context.Background(), docs: &fsDocuments{listOverride: `[{"name":"a"},{"name":"a"}]`}}
	if _, err := fs.ReadDir("."); err == nil {
		t.Fatal("accepted duplicate name")
	}
	for _, p := range []string{"../x", "a/../b", "/x", "a//b", "a/./b", "a\\b", ""} {
		if safePath(p) == nil {
			t.Fatalf("accepted path %q", p)
		}
	}
}

func TestChangedSourceFailsBeforeLastBytesAreAccepted(t *testing.T) {
	for _, change := range []string{"size", "mtime"} {
		t.Run(change, func(t *testing.T) {
			d := &fsDocuments{root: t.TempDir()}
			os.WriteFile(d.full("file"), []byte("content"), 0600)
			calls := 0
			d.statHook = func(p string, e *documentEntry) {
				calls++
				if calls > 1 {
					if change == "size" {
						e.Size++
					} else {
						e.Modified++
					}
				}
			}
			fs := &documentFS{ctx: context.Background(), docs: d}
			r, _, err := fs.OpenFile("file")
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			buf := make([]byte, 7)
			n, err := r.Read(buf)
			if err == nil || n != 0 {
				t.Fatal("changed source was accepted")
			}
		})
	}
}

func TestDocumentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fs := &documentFS{ctx: ctx, docs: &fsDocuments{}}
	if _, err := fs.ReadDir("."); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCancellationInterruptsBlockedProviderWrite(t *testing.T) {
	descriptors := make([]int, 2)
	if err := syscall.Pipe(descriptors); err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(descriptors[0])
	file, err := documentFile(int64(descriptors[1]), "restore-pipe")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := context.AfterFunc(ctx, func() { file.Close() })
	defer stop()
	finished := make(chan error, 1)
	go func() { _, err := file.Write(make([]byte, 1<<20)); finished <- err }()
	// Confirm the writer has started, then leave the pipe full and cancel it.
	var first [1]byte
	if _, err := syscall.Read(descriptors[0], first[:]); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("expected interrupted provider write, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not interrupt blocked provider write")
	}
}

func TestArchiveRejectsFileChangedDuringRead(t *testing.T) {
	d := &fsDocuments{root: t.TempDir()}
	if err := os.WriteFile(d.full("file.txt"), []byte("original data"), 0600); err != nil {
		t.Fatal(err)
	}
	opened := false
	d.opened = func() { opened = true }
	d.statHook = func(p string, e *documentEntry) {
		if opened && p == "file.txt" {
			e.Modified++
		}
	}
	cfg, _ := buzhash.NewConfig(4096)
	dir := t.TempDir()
	store, err := backupproxy.NewLocalStore(dir, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = backupproxy.NewServer(backupproxy.NewLocalClient(&documentFS{ctx: context.Background(), docs: d}), store).RunBackupWithMode(context.Background(), ".", backupproxy.BackupConfig{BackupType: datastore.BackupHost, BackupID: "changing", ChunkConfig: cfg, DetectionMode: backupproxy.DetectionData})
	if err == nil || !strings.Contains(err.Error(), "file changed") {
		t.Fatalf("changed file backup should fail: %v", err)
	}
}

func configForServer(t *testing.T, s *httptest.Server) string {
	t.Helper()
	sum := sha256.Sum256(s.Certificate().Raw)
	raw, _ := json.Marshal(configuration{Server: s.URL, HostID: "android-test", Fingerprint: hex.EncodeToString(sum[:]), AuthID: "test@pbs!device", Secret: "test-secret"})
	return string(raw)
}
func TestPinnedTLSAndStorageKey(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PBSAPIToken=test@pbs!storage:storage-secret" {
			t.Error("storage credentials were not selected")
		}
		io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()
	var cfg configuration
	json.Unmarshal([]byte(configForServer(t, server)), &cfg)
	cfg.StorageAuthID = "test@pbs!storage"
	cfg.StorageSecret = "storage-secret"
	raw, _ := json.Marshal(cfg)
	client, err := NewClient(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListSnapshots("backup", "family/phone", "android-test"); err != nil {
		t.Fatal(err)
	}
	cfg.Fingerprint = strings.Repeat("00", 32)
	raw, _ = json.Marshal(cfg)
	client, err = NewClient(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListSnapshots("backup", "", ""); err == nil {
		t.Fatal("wrong certificate pin accepted")
	}
	cfg.Server = "http://server:8007"
	raw, _ = json.Marshal(cfg)
	if _, err := NewClient(string(raw)); err == nil {
		t.Fatal("accepted HTTP server")
	}
}

func TestEnrollmentKeepsPrivateKeyOnDevice(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/pbsx/agent/bootstrap" {
			t.Error("wrong enrollment endpoint")
		}
		raw, _ := io.ReadAll(r.Body)
		if bytes.Contains(raw, []byte("PRIVATE KEY")) {
			t.Error("private key sent to server")
		}
		var req struct {
			HostID    string `json:"host-id"`
			Token     string `json:"token"`
			Cert      string `json:"client-certificate"`
			Heartbeat struct {
				OS string `json:"operating-system"`
			} `json:"heartbeat"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Error(err)
		}
		block, _ := pem.Decode([]byte(req.Cert))
		if block == nil {
			t.Error("missing certificate")
			return
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Error(err)
			return
		}
		if cert.Subject.CommonName != "android-test" || cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) != nil {
			t.Error("invalid device identity")
		}
		if req.Token != "one-time-test" || req.Heartbeat.OS != "other" {
			t.Error("incompatible bootstrap request")
		}
		io.WriteString(w, `{"data":{"backup-auth-id":"test@pbs!device","backup-secret":"test-secret","store":"family-store","ns":"family/existing"}}`)
	}))
	defer server.Close()
	sum := sha256.Sum256(server.Certificate().Raw)
	block, _ := json.Marshal(map[string]any{"v": 1, "host_id": "android-test", "server_url": server.URL, "bootstrap_token": "one-time-test", "default_store": "tampered", "default_namespace": "unapproved", "server_ca_fingerprint": hex.EncodeToString(sum[:])})
	result, err := Enroll("pbsx1."+base64.RawURLEncoding.EncodeToString(block), "Samsung S20 FE")
	if err != nil {
		t.Fatal(err)
	}
	var cfg configuration
	json.Unmarshal([]byte(result), &cfg)
	if cfg.PrivateKey == "" || cfg.AuthID != "test@pbs!device" {
		t.Fatal("missing saved device identity or API key")
	}
	if cfg.DefaultStore != "family-store" || cfg.DefaultNamespace == nil || *cfg.DefaultNamespace != "family/existing" {
		t.Fatal("enrollment did not use the server-approved destination")
	}
	if _, err := NewClient(result); err != nil {
		t.Fatal(err)
	}
}

func TestClientOperationLockAndCancel(t *testing.T) {
	c := &Client{}
	ctx, done, err := c.begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.begin(); err == nil {
		t.Fatal("concurrent operation allowed")
	}
	c.Cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel did not propagate")
	}
	done()
	_, done, err = c.begin()
	if err != nil {
		t.Fatal(err)
	}
	done()
}

type failingBinder struct{}

func (failingBinder) BindSocket(int64) error { return errors.New("selected Wi-Fi network unavailable") }

func TestSocketBindingFailureCannotFallBack(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("request escaped failed socket binding") }))
	defer server.Close()
	client, err := NewClient(configForServer(t, server))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetSocketBinder(failingBinder{}); err != nil {
		t.Fatal(err)
	}
	_, err = client.ListSnapshots("backup", "", "")
	if err == nil || !strings.Contains(err.Error(), "Wi-Fi network unavailable") {
		t.Fatalf("REST binding: %v", err)
	}
	_, err = client.Backup("backup", "", &fsDocuments{root: t.TempDir()}, nil)
	if err == nil || !strings.Contains(err.Error(), "Wi-Fi network unavailable") {
		t.Fatalf("upload binding: %v", err)
	}
	err = client.Restore("backup", "", "android-test", 1700000000, &fsDocuments{root: t.TempDir()}, nil)
	if err == nil || !strings.Contains(err.Error(), "Wi-Fi network unavailable") {
		t.Fatalf("restore binding: %v", err)
	}
}

func TestMissingStorageAccessExplainsSetupWithoutReenrollment(t *testing.T) {
	original := errors.New("start session: PBS H2 connect: upgrade failed: HTTP 403: PBS-X host 'phone' has no enabled backup job targeting this datastore/namespace")
	for _, ns := range []string{"", "family/phones"} {
		err := backupFailure(original, "phone", "backup", ns)
		message := err.Error()
		for _, want := range []string{"Hosts → phone → Agent", "datastore backup", "enrollment is already valid"} {
			if !strings.Contains(message, want) {
				t.Fatalf("missing %q in %q", want, message)
			}
		}
		label := ns
		if label == "" {
			label = "(root)"
		}
		if !strings.Contains(message, "namespace "+label) {
			t.Fatal(message)
		}
	}
	other := errors.New("HTTP 403: owner check failed")
	if backupFailure(other, "phone", "backup", "") != other {
		t.Fatal("unrelated access error was rewritten")
	}
	for _, detail := range []string{"permission check failed - missing Datastore.Backup", "missing permissions 'Datastore.Backup' on '/datastore/backup/devices/phone'"} {
		acl := errors.New("start session: upgrade failed: HTTP 403: " + detail)
		if message := backupFailure(acl, "phone", "backup", "devices/phone").Error(); !strings.Contains(message, "storage key's Permissions") || !strings.Contains(message, "namespace devices/phone") {
			t.Fatal(message)
		}
	}
}
