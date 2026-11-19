package backupproxy

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
)

type previousIndexProtocol struct {
	*mockPBSProtocol
	previous map[string][]byte
	loaded   []string
}

func (p *previousIndexProtocol) downloadPrevious(name string) ([]byte, error) {
	p.loaded = append(p.loaded, name)
	if raw, ok := p.previous[name]; ok {
		return raw, nil
	}
	return nil, errors.New("previous index not available")
}

func TestPBSSplitRegistersBothPreviousIndexesBeforeSkippingUploads(t *testing.T) {
	session, mock := newTestPBSSession(t)
	metadata, payload := []byte("metadata"), []byte("phone photo payload")
	protocol := &previousIndexProtocol{mockPBSProtocol: mock, previous: make(map[string][]byte)}
	for name, data := range map[string][]byte{"root.mpxar.didx": metadata, "root.ppxar.didx": payload} {
		index := datastore.NewDynamicIndexWriter(0)
		index.Add(uint64(len(data)), chunkDigest(data, nil))
		raw, err := index.Finish()
		if err != nil {
			t.Fatal(err)
		}
		protocol.previous[name] = raw
	}
	session.proto = protocol
	session.config.PreviousBackup = &PreviousBackupRef{BackupID: "test", BackupType: datastore.BackupHost, BackupTime: 1}
	var progress UploadProgress
	session.config.OnUploadProgress = func(p UploadProgress) { progress = p }
	if _, err := session.UploadSplitArchive(context.Background(), "root.mpxar.didx", bytes.NewReader(metadata), "root.ppxar.didx", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if len(protocol.loaded) != 2 || len(mock.chunks) != 0 || progress.UploadedBytes != 0 || progress.ProcessedBytes != uint64(len(metadata)+len(payload)) {
		t.Fatalf("previous loads=%v uploaded chunks=%d progress=%+v", protocol.loaded, len(mock.chunks), progress)
	}
}

func TestPBSUnregisteredPreviousChunksAreUploaded(t *testing.T) {
	session, mock := newTestPBSSession(t)
	protocol := &previousIndexProtocol{mockPBSProtocol: mock, previous: make(map[string][]byte)}
	session.proto = protocol
	session.config.PreviousBackup = &PreviousBackupRef{BackupID: "test", BackupType: datastore.BackupHost, BackupTime: 1}
	if _, err := session.UploadArchive(context.Background(), "root.ppxar.didx", bytes.NewReader([]byte("must be transmitted"))); err != nil {
		t.Fatal(err)
	}
	if len(mock.chunks) == 0 {
		t.Fatal("skipped chunks without registering them in the server session")
	}
}
