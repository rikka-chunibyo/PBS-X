package mobilecore

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
	"golang.org/x/net/http2"
)

// Use the real TLS upgrade and HTTP/2 reader against dummy in-memory snapshots.
// This exercises Android's actual format selection, not just the shared helper.
func snapshotReaderFixture(t *testing.T, files map[string][]byte) (*Client, func(string) int) {
	t.Helper()
	var mu sync.Mutex
	requests := map[string]int{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("file-name")
		mu.Lock()
		requests[name]++
		mu.Unlock()
		if r.URL.Path == "/download" && files[name] != nil {
			_, _ = w.Write(files[name])
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/reader" {
			http.NotFound(w, r)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: proxmox-backup-reader-protocol-v1\r\n\r\n")
		if rw.Flush() != nil {
			return
		}
		(&http2.Server{}).ServeConn(conn, &http2.ServeConnOpts{Handler: handler})
	}))
	t.Cleanup(server.Close)
	c := &Client{cfg: configuration{Server: server.URL, AuthID: "fixture@pbs!test", Secret: "dummy", HostID: "fixture"},
		tls: server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()}
	return c, func(name string) int { mu.Lock(); defer mu.Unlock(); return requests[name] }
}

func TestAndroidReaderCannotFallBackAfterDeclaredSplitDownloadFails(t *testing.T) {
	for _, missing := range []string{"root.mpxar.didx", "root.ppxar.didx"} {
		t.Run(missing, func(t *testing.T) {
			files := map[string][]byte{}
			m := &datastore.Manifest{BackupType: "host", BackupID: "fixture", BackupTime: 123}
			for _, name := range []string{"root.mpxar.didx", "root.ppxar.didx", "root.pxar.didx"} {
				writer := datastore.NewDynamicIndexWriter(123)
				writer.Add(1, [32]byte{})
				data, err := writer.Finish()
				if err != nil {
					t.Fatal(err)
				}
				sum := writer.Csum()
				files[name] = data
				m.Files = append(m.Files, datastore.BackupFileInfo{Filename: name, CSum: hex.EncodeToString(sum[:]), Size: 1})
			}
			plain, err := m.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			files["index.json.blob"], err = datastore.EncodeBlob(nil, plain)
			if err != nil {
				t.Fatal(err)
			}
			delete(files, missing)
			client, requests := snapshotReaderFixture(t, files)
			snapshot, err := client.openSnapshot(t.Context(), "fixture", "", "fixture", 123)
			if snapshot != nil {
				snapshot.close()
			}
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("split download did not fail: %v", err)
			}
			if requests("root.pxar.didx") != 0 {
				t.Fatal("failed split restore requested a different legacy archive")
			}
		})
	}
}
