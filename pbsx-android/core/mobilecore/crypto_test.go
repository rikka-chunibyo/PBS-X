package mobilecore

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
)

func TestSettingsEncryption(t *testing.T) {
	plain := `{"jobs":[{"name":"Photos"}],"secret":"fixture-secret","keys":["fixture-key"]}`
	encrypted, err := EncryptSettings(plain, "temporary export password")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "fixture-secret") || strings.Contains(encrypted, "Photos") {
		t.Fatal("plaintext leaked")
	}
	decoded, err := DecryptSettings(encrypted, "temporary export password")
	if err != nil || decoded != plain {
		t.Fatal(err, decoded)
	}
	if _, err = DecryptSettings(encrypted, "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
	var e settingsEnvelope
	json.Unmarshal([]byte(encrypted), &e)
	e.Data[len(e.Data)-1] ^= 1
	raw, _ := json.Marshal(e)
	if _, err = DecryptSettings(string(raw), "temporary export password"); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	e.KDF = "scrypt-1073741824-8-1"
	raw, _ = json.Marshal(e)
	if _, err = DecryptSettings(string(raw), "temporary export password"); err == nil {
		t.Fatal("unbounded KDF accepted")
	}
	if _, err = DecryptSettings(encrypted+"{}", "temporary export password"); err == nil {
		t.Fatal("trailing object accepted")
	}
	if _, err = EncryptSettings(plain, ""); err == nil {
		t.Fatal("empty password accepted")
	}
	another, _ := EncryptSettings(plain, "temporary export password")
	if encrypted == another {
		t.Fatal("salt/nonce reused")
	}
}
func TestBackupKeySelectionAndSignedManifest(t *testing.T) {
	key, _ := GenerateEncryptionKey()
	cc, err := parseBackupKey(key)
	if err != nil {
		t.Fatal(err)
	}
	selected, keys, err := connectionKeys(configuration{EncryptionEnabled: true, EncryptionKey: key})
	if err != nil || selected == nil || len(keys) != 1 {
		t.Fatal(err)
	}
	for _, cfg := range []configuration{{EncryptionEnabled: true}, {EncryptionEnabled: true, EncryptionKey: "invalid"}, {DecryptionKeys: []string{"invalid"}}} {
		if _, _, err = connectionKeys(cfg); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	m := &datastore.Manifest{BackupType: "host", BackupID: "encrypted", BackupTime: 123, CryptMode: "encrypt", Files: []datastore.BackupFileInfo{{Filename: "root.mpxar.didx", CryptMode: "encrypt"}}}
	if err = datastore.SignManifest(m, cc); err != nil {
		t.Fatal(err)
	}
	plain, _ := m.Marshal()
	blob, _ := datastore.EncodeBlob(nil, plain)
	c := &Client{keys: keys}
	if _, _, err = c.snapshotManifest(blob, "host", "encrypted", 123); err != nil {
		t.Fatal(err)
	}
	c.keys = nil
	if _, _, err = c.snapshotManifest(blob, "host", "encrypted", 123); err == nil {
		t.Fatal("missing key accepted")
	}
	c.keys = keys
	m.Files[0].Size = 42
	plain, _ = m.Marshal()
	blob, _ = datastore.EncodeBlob(nil, plain)
	if _, _, err = c.snapshotManifest(blob, "host", "encrypted", 123); err == nil {
		t.Fatal("tampered signature accepted")
	}
}

type cryptoChunk struct{ data []byte }

func (c cryptoChunk) GetChunk([32]byte) ([]byte, error) { return c.data, nil }
func TestEncryptedChunkAuthenticationAndDigest(t *testing.T) {
	key, _ := GenerateEncryptionKey()
	cc, _ := parseBackupKey(key)
	data := bytes.Repeat([]byte("sensitive backup fixture"), 1000)
	digest := cc.ComputeDigest(data)
	blob, err := datastore.EncodeEncryptedBlob(nil, data, cc, true)
	if err != nil {
		t.Fatal(err)
	}
	source := verifiedChunkSource{cryptoChunk{blob}, cc, true}
	raw, err := source.GetChunk(digest)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := datastore.DecodeBlob(nil, raw)
	if err != nil || !bytes.Equal(plain, data) {
		t.Fatal(err)
	}
	if _, err = source.GetChunk([32]byte{}); err == nil {
		t.Fatal("substituted chunk accepted")
	}
	blob[len(blob)-1] ^= 1
	if _, err = source.GetChunk(digest); err == nil {
		t.Fatal("tampered chunk accepted")
	}
}
