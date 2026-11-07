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

func TestCustomPassphraseRecoveryIsDeviceIndependent(t *testing.T) {
	const passphrase = "fixture custom key 0123456789A" // Exactly 30 characters.
	// Independently computed with Python hashlib.scrypt. This is a permanent
	// recovery vector: changing v1 must never make a memorized key unusable.
	const expected = "462800aab5ba6f765e55ea99fae5591e9718dbe906c26c1fcf2c713079a51c9d"
	first, err := DeriveEncryptionKey(passphrase)
	if err != nil || first != expected {
		t.Fatal("v1 recovery contract changed", err)
	}
	recovered, err := DeriveEncryptionKey(passphrase)
	if err != nil || recovered != first {
		t.Fatal("same passphrase did not recover key", err)
	}
	cc, _ := parseBackupKey(first)
	other, _ := parseBackupKey(recovered)
	blob, err := datastore.EncodeEncryptedBlob(nil, []byte("restore after phone reset"), cc, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := datastore.DecodeEncryptedBlob(nil, blob, other)
	if err != nil || string(data) != "restore after phone reset" {
		t.Fatal("recovered key cannot decrypt backup", err)
	}
	for _, changed := range []string{passphrase + " ", "Fixture custom key 0123456789A", " " + passphrase} {
		key, e := DeriveEncryptionKey(changed)
		if e != nil || key == first {
			t.Fatal("passphrase silently normalized", e)
		}
	}
	for _, bad := range []string{"", strings.Repeat("x", 4097)} {
		if _, e := DeriveEncryptionKey(bad); e == nil {
			t.Fatal("invalid passphrase accepted")
		}
	}
}
