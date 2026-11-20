package backupproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
)

type previousIndexProtocol struct {
	*mockPBSProtocol
	previous map[string][]byte
	loaded   []string
	appended map[uint64][]string
}

func (p *previousIndexProtocol) dynamicIndexAppend(wid uint64, digests []string, offsets []uint64) error {
	// Unlike uploads, index appends include references to reused chunks too.
	p.mu.Lock()
	if p.appended == nil {
		p.appended = make(map[uint64][]string)
	}
	p.appended[wid] = append(p.appended[wid], digests...)
	p.mu.Unlock()
	return p.mockPBSProtocol.dynamicIndexAppend(wid, digests, offsets)
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

func TestPBSEncryptedPreviousReuseIsScopedToKey(t *testing.T) {
	previousKey, err := datastore.NewCryptConfig([32]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	archives := map[string][]byte{
		"root.mpxar.didx": []byte("private filename and filesystem metadata"),
		"root.ppxar.didx": []byte("private phone photo payload"),
	}
	for _, changedKey := range []bool{false, true} {
		name := "same-key"
		key := [32]byte{1}
		if changedKey {
			name = "different-key"
			key = [32]byte{2}
		}
		t.Run(name, func(t *testing.T) {
			currentKey, err := datastore.NewCryptConfig(key)
			if err != nil {
				t.Fatal(err)
			}
			session, mock := newTestPBSSession(t)
			protocol := &previousIndexProtocol{mockPBSProtocol: mock, previous: make(map[string][]byte)}
			for name, data := range archives {
				index := datastore.NewDynamicIndexWriter(1)
				index.Add(uint64(len(data)), previousKey.ComputeDigest(data))
				raw, err := index.Finish()
				if err != nil {
					t.Fatal(err)
				}
				protocol.previous[name] = raw
			}
			session.proto = protocol
			session.config.CryptMode = datastore.CryptModeEncrypt
			session.config.CryptConfig = currentKey
			session.config.PreviousBackup = &PreviousBackupRef{BackupID: "test", BackupType: datastore.BackupHost, BackupTime: 1}
			var progress UploadProgress
			session.config.OnUploadProgress = func(p UploadProgress) { progress = p }
			if _, err := session.UploadSplitArchive(t.Context(), "root.mpxar.didx", bytes.NewReader(archives["root.mpxar.didx"]), "root.ppxar.didx", bytes.NewReader(archives["root.ppxar.didx"])); err != nil {
				t.Fatal(err)
			}
			if len(protocol.loaded) != 2 {
				t.Fatal("did not register both previous encrypted indexes")
			}
			if !changedKey && (len(mock.chunks) != 0 || progress.UploadedBytes != 0) {
				t.Fatal("unchanged encrypted data was uploaded again with the same key")
			}
			if changedKey && (len(mock.chunks) != 2 || progress.UploadedBytes == 0) {
				t.Fatal("reused old-key ciphertext after changing the encryption key")
			}
			for wid, index := range mock.indexes {
				data := archives[index.archiveName]
				digest := currentKey.ComputeDigest(data)
				refs := protocol.appended[wid]
				if digest == sha256.Sum256(data) || len(refs) != 1 || refs[0] != hex.EncodeToString(digest[:]) {
					t.Fatal("server references did not use opaque key-dependent chunk identifiers")
				}
				if changedKey {
					blob := mock.chunks[refs[0]]
					plain, err := datastore.DecodeEncryptedBlob(nil, blob, currentKey)
					if err != nil || !bytes.Equal(plain, data) {
						t.Fatal("new-key ciphertext does not restore the original data", err)
					}
					if _, err := datastore.DecodeEncryptedBlob(nil, blob, previousKey); err == nil {
						t.Fatal("old key could decrypt new-key chunk")
					}
				}
			}
		})
	}
}
