// A loopback-only HTTPS fixture for Android socket-binding instrumentation.
// It accepts dummy credentials only and never connects to a real PBS server.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-plus/pxar/format"
	"golang.org/x/net/http2"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	output := flag.String("config", "", "destination for dummy emulator connection JSON")
	flag.Parse()
	if *output == "" {
		panic("-config is required")
	}
	scratch, err := os.MkdirTemp("", "pbsx-android-network-fixture-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(scratch)
	source := filepath.Join(scratch, "source")
	for name, data := range map[string]string{"Photos/a.txt": "backup-A", "Photos/same.txt": "same", "Photos/missing.txt": "new", "Photos/nested/猫.jpg": "image fixture", "Documents/private.txt": "not selected"} {
		p := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			panic(err)
		}
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			panic(err)
		}
	}
	os.MkdirAll(filepath.Join(source, "Empty"), 0700)
	chunkConfig, _ := buzhash.NewConfig(4096)
	storeDir := filepath.Join(scratch, "store")
	store, err := backupproxy.NewLocalStore(storeDir, chunkConfig, true)
	if err != nil {
		panic(err)
	}
	_, err = backupproxy.NewServer(backupproxy.NewLocalClient(&fixtureFS{root: source}), store).RunBackupWithMode(context.Background(), ".", backupproxy.BackupConfig{BackupType: datastore.BackupHost, BackupID: "android-test", BackupTime: 1700000000, ChunkConfig: chunkConfig, DetectionMode: backupproxy.DetectionData, Compress: true})
	if err != nil {
		panic(err)
	}
	chunks, err := datastore.NewChunkStore(storeDir)
	if err != nil {
		panic(err)
	}
	readerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data []byte
		var err error
		switch filepath.Base(r.URL.Path) {
		case "download":
			name := r.URL.Query().Get("file-name")
			if name != "root.mpxar.didx" && name != "root.ppxar.didx" {
				http.NotFound(w, r)
				return
			}
			data, err = os.ReadFile(filepath.Join(storeDir, name))
		case "chunk":
			value, e := hex.DecodeString(r.URL.Query().Get("digest"))
			if e != nil || len(value) != 32 {
				http.Error(w, "invalid fixture digest", 400)
				return
			}
			var digest [32]byte
			copy(digest[:], value)
			data, err = chunks.LoadChunk(digest)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "fixture data unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(r.Header.Get("Authorization"), "PBSAPIToken"), "="))
		if auth != "fixture@pbs!android:instrumentation-fixture-secret" {
			http.Error(w, "unexpected fixture request", 400)
			return
		}
		if r.URL.Path == "/api2/json/reader" {
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: proxmox-backup-reader-protocol-v1\r\n\r\n")
			if err := rw.Flush(); err != nil {
				return
			}
			(&http2.Server{}).ServeConn(conn, &http2.ServeConnOpts{Handler: readerHandler})
			return
		}
		if r.URL.Path != "/api2/json/admin/datastore/backup/snapshots" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"backup-type":"host","backup-id":"android-test","backup-time":1700000000,"comment":"PBS-X Android: Photos"}]}`)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	_, port, err := net.SplitHostPort(endpoint.Host)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(server.Certificate().Raw)
	config, _ := json.Marshal(map[string]string{"server": "https://10.0.2.2:" + port, "host_id": "android-test", "fingerprint": hex.EncodeToString(sum[:]), "auth_id": "fixture@pbs!android", "secret": "instrumentation-fixture-secret"})
	if err := os.WriteFile(*output, config, 0600); err != nil {
		panic(err)
	}
	fmt.Println("Loopback HTTPS fixture ready")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}

type fixtureFS struct {
	backupproxy.NoExtendedAttrs
	root string
}

func (f *fixtureFS) Stat(p string) (format.Stat, error) {
	info, err := os.Stat(filepath.Join(f.root, p))
	if err != nil {
		return format.Stat{}, err
	}
	return fixtureStat(info), nil
}
func fixtureStat(info os.FileInfo) format.Stat {
	mode := uint64(format.ModeIFREG | 0644)
	if info.IsDir() {
		mode = uint64(format.ModeIFDIR | 0755)
	}
	return format.Stat{Mode: mode, Mtime: format.NewStatxTimestamp(info.ModTime().Unix(), uint32(info.ModTime().Nanosecond()))}
}
func (f *fixtureFS) ReadDir(p string) ([]backupproxy.DirEntry, error) {
	items, err := os.ReadDir(filepath.Join(f.root, p))
	if err != nil {
		return nil, err
	}
	out := []backupproxy.DirEntry{}
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, backupproxy.DirEntry{Name: item.Name(), Stat: fixtureStat(info), Size: uint64(info.Size())})
	}
	return out, nil
}
func (f *fixtureFS) OpenFile(p string) (io.ReadCloser, uint64, error) {
	file, err := os.Open(filepath.Join(f.root, p))
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}
	return file, uint64(info.Size()), nil
}
func (f *fixtureFS) ReadLink(string) (string, error) { return "", errors.New("no fixture symlinks") }
