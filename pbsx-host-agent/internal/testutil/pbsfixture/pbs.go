// Package pbsfixture provides isolated PBS reader fixtures for regression tests.
package pbsfixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-plus/pxar/encoder"
	"github.com/pbs-plus/pxar/format"
	"golang.org/x/net/http2"
)

const HostID = "fixture-host"
const Stamp int64 = 1700000000
const Content = "GOOD-PAYLOAD"

type Snapshot struct {
	Manifest datastore.Manifest
	Files    map[string][]byte
	Chunks   map[[32]byte][]byte
	mu       sync.Mutex
	requests map[string]int
}

func New(t *testing.T, split bool) *Snapshot {
	t.Helper()
	s := &Snapshot{
		Manifest: datastore.Manifest{BackupType: "host", BackupID: HostID, BackupTime: Stamp},
		Files:    make(map[string][]byte), Chunks: make(map[[32]byte][]byte), requests: make(map[string]int),
	}
	var meta, payload bytes.Buffer
	var enc *encoder.Encoder
	root := &pxar.Metadata{Stat: format.Stat{Mode: format.ModeIFDIR | 0755}}
	if split {
		enc = encoder.NewEncoder(&meta, &payload, root, nil)
	} else {
		enc = encoder.NewEncoder(&meta, nil, root, nil)
	}
	if _, err := enc.AddFile(&pxar.Metadata{Stat: format.Stat{Mode: format.ModeIFREG | 0644}}, "file.txt", []byte(Content)); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if split {
		s.addIndex(t, "root.mpxar.didx", meta.Bytes())
		s.addIndex(t, "root.ppxar.didx", payload.Bytes())
	} else {
		s.addIndex(t, "root.pxar.didx", meta.Bytes())
	}
	s.WriteManifest(t)
	return s
}

func (s *Snapshot) addIndex(t *testing.T, name string, data []byte) {
	t.Helper()
	digest := sha256.Sum256(data)
	blob, err := datastore.EncodeCompressedBlob(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	s.Chunks[digest] = blob
	writer := datastore.NewDynamicIndexWriter(Stamp)
	writer.Add(uint64(len(data)), digest)
	index, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	s.Files[name] = index
	sum := writer.Csum()
	s.Manifest.Files = append(s.Manifest.Files, datastore.BackupFileInfo{
		Filename: name, CryptMode: "none", CSum: hex.EncodeToString(sum[:]), Size: uint64(len(data)),
	})
}

func (s *Snapshot) WriteManifest(t *testing.T) {
	t.Helper()
	plain, err := s.Manifest.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	s.Files["index.json.blob"], err = datastore.EncodeCompressedBlob(nil, plain)
	if err != nil {
		t.Fatal(err)
	}
}

func (s *Snapshot) DownloadFile(name string) ([]byte, error) {
	data, ok := s.Files[name]
	if !ok {
		return nil, &http.ProtocolError{ErrorString: "fixture file missing: " + name}
	}
	return data, nil
}

func (s *Snapshot) Requests(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[name]
}

// Start uses the real TLS upgrade and HTTP/2 reader transport, with temporary
// in-memory files and dummy credentials. Configure the snapshot before calling.
func (s *Snapshot) Start(t *testing.T) backupproxy.PBSConfig {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download":
			name := r.URL.Query().Get("file-name")
			s.mu.Lock()
			s.requests[name]++
			s.mu.Unlock()
			if data, ok := s.Files[name]; ok {
				_, _ = w.Write(data)
				return
			}
		case "/chunk":
			digest, err := hex.DecodeString(r.URL.Query().Get("digest"))
			if err == nil && len(digest) == 32 {
				if data, ok := s.Chunks[[32]byte(digest)]; ok {
					_, _ = w.Write(data)
					return
				}
			}
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
	return backupproxy.PBSConfig{
		BaseURL: server.URL + "/api2/json", Datastore: "fixture", AuthToken: "fixture@pbs!test:dummy",
		TLSConfig: server.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
	}
}
