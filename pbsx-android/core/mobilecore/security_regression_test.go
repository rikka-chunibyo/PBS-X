package mobilecore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
)

func TestRedirectNeverForwardsAPICredentials(t *testing.T) {
	var destinationHits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationHits.Add(1)
		t.Error("redirect destination was contacted; credentials could have leaked")
	}))
	defer destination.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PBSAPIToken=test@pbs!device:test-secret" {
			t.Error("fixture did not exercise an authenticated request")
		}
		http.Redirect(w, r, destination.URL+"/collect", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := NewClient(configForServer(t, source))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.request(context.Background(), "GET", "/admin/datastore", nil, true); err == nil || !strings.Contains(err.Error(), "redirect refused") {
		t.Fatalf("redirect was not explicitly refused: %v", err)
	}
	if destinationHits.Load() != 0 {
		t.Fatal("redirect target received a request")
	}
}

func TestSettingsEnvelopeRejectsMalformedAndSubstitutedFields(t *testing.T) {
	encrypted, err := EncryptSettings(`{"format":"fixture","secret":"dummy"}`, "fixture password")
	if err != nil {
		t.Fatal(err)
	}
	var original settingsEnvelope
	if err := json.Unmarshal([]byte(encrypted), &original); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*settingsEnvelope)
	}{
		{"foreign-format", func(e *settingsEnvelope) { e.Format = "another-format" }},
		{"future-version", func(e *settingsEnvelope) { e.Version++ }},
		{"unbounded-kdf", func(e *settingsEnvelope) { e.KDF = "scrypt-1073741824-8-1" }},
		{"short-salt", func(e *settingsEnvelope) { e.Salt = e.Salt[:31] }},
		{"short-nonce", func(e *settingsEnvelope) { e.Nonce = e.Nonce[:11] }},
		{"truncated-ciphertext", func(e *settingsEnvelope) { e.Data = e.Data[:15] }},
		{"substituted-salt", func(e *settingsEnvelope) { e.Salt[0] ^= 1 }},
		{"substituted-nonce", func(e *settingsEnvelope) { e.Nonce[0] ^= 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := original
			e.Salt = append([]byte(nil), original.Salt...)
			e.Nonce = append([]byte(nil), original.Nonce...)
			e.Data = append([]byte(nil), original.Data...)
			test.change(&e)
			raw, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecryptSettings(string(raw), "fixture password"); err == nil {
				t.Fatal("damaged envelope accepted")
			}
		})
	}
	var unknown map[string]any
	if err := json.Unmarshal([]byte(encrypted), &unknown); err != nil {
		t.Fatal(err)
	}
	unknown["unexpected-algorithm-override"] = "none"
	raw, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptSettings(string(raw), "fixture password"); err == nil {
		t.Fatal("unknown envelope fields were silently accepted")
	}
	if _, err := DecryptSettings(encrypted, ""); err == nil {
		t.Fatal("empty password accepted")
	}
	plain, err := DecryptSettings(encrypted, "fixture password")
	if err != nil || plain != `{"format":"fixture","secret":"dummy"}` {
		t.Fatal("valid envelope stopped working", err)
	}
}

func TestSignedSnapshotIdentityAndHistoricalKeySelection(t *testing.T) {
	oldKey, err := parseBackupKey(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	currentKey, err := parseBackupKey(strings.Repeat("02", 32))
	if err != nil {
		t.Fatal(err)
	}
	oldFP := datastore.FormatFingerprint(oldKey.Fingerprint())
	currentFP := datastore.FormatFingerprint(currentKey.Fingerprint())
	client := &Client{crypt: currentKey, keys: map[string]*datastore.CryptConfig{oldFP: oldKey, currentFP: currentKey}}
	signed := func(kind, id string, stamp int64) *datastore.Manifest {
		t.Helper()
		m := &datastore.Manifest{BackupType: kind, BackupID: id, BackupTime: stamp,
			Files: []datastore.BackupFileInfo{{Filename: "root.mpxar.didx", CryptMode: "encrypt", CSum: strings.Repeat("00", 32), Size: 10}}}
		if err := datastore.SignManifest(m, oldKey); err != nil {
			t.Fatal(err)
		}
		return m
	}
	blob := func(m *datastore.Manifest) []byte {
		t.Helper()
		plain, err := m.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := datastore.EncodeBlob(nil, plain)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	_, key, err := client.snapshotManifest(blob(signed("host", "photos", 123)), "host", "photos", 123)
	if err != nil || key == nil || datastore.FormatFingerprint(key.Fingerprint()) != oldFP {
		t.Fatal("historical snapshot did not select its own key", err)
	}
	for _, m := range []*datastore.Manifest{signed("ct", "photos", 123), signed("host", "documents", 123), signed("host", "photos", 124)} {
		if _, _, err := client.snapshotManifest(blob(m), "host", "photos", 123); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
			t.Fatal("valid signature from another snapshot was accepted", err)
		}
	}
	for _, change := range []struct {
		name string
		edit func(*datastore.Manifest)
	}{
		{"missing-signature", func(m *datastore.Manifest) { m.Signature = "" }},
		{"changed-file-mode", func(m *datastore.Manifest) { m.Files[0].CryptMode = "none" }},
		{"missing-fingerprint", func(m *datastore.Manifest) { m.Unprotected = nil }},
		{"unknown-fingerprint", func(m *datastore.Manifest) { m.Unprotected = json.RawMessage(`{"key-fingerprint":"unknown"}`) }},
		{"substituted-known-fingerprint", func(m *datastore.Manifest) {
			m.Unprotected, _ = json.Marshal(datastore.UnprotectedInfo{KeyFingerprint: currentFP})
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			m := signed("host", "photos", 123)
			change.edit(m)
			if _, _, err := client.snapshotManifest(blob(m), "host", "photos", 123); err == nil {
				t.Fatal("tampered signed snapshot was accepted")
			}
		})
	}
}

func TestSnapshotIndexRejectsCorruptionAndSubstitution(t *testing.T) {
	makeIndex := func(data string) []byte {
		t.Helper()
		writer := datastore.NewDynamicIndexWriter(123)
		writer.Add(10, sha256.Sum256([]byte(data)))
		raw, err := writer.Finish()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	original := makeIndex("authentic archived data")
	index, err := datastore.ParseDynamicIndex(original)
	if err != nil {
		t.Fatal(err)
	}
	sum, size := index.ComputeCsum()
	manifest := &datastore.Manifest{Files: []datastore.BackupFileInfo{{Filename: "root.mpxar.didx", CSum: hex.EncodeToString(sum[:]), Size: size}}}
	if err := datastore.VerifySnapshotIndex(manifest, "root.mpxar.didx", original); err != nil {
		t.Fatal("valid archive rejected", err)
	}
	corrupted := append([]byte(nil), original...)
	corrupted[len(corrupted)-1] ^= 1
	for name, raw := range map[string][]byte{
		"corrupted-entry":         corrupted,
		"substituted-valid-index": makeIndex("different archived data"),
		"short-header":            original[:7],
		"truncated-entry":         original[:len(original)-1],
	} {
		t.Run(name, func(t *testing.T) {
			if err := datastore.VerifySnapshotIndex(manifest, "root.mpxar.didx", raw); err == nil {
				t.Fatal("invalid archive index accepted")
			}
		})
	}
	if err := datastore.VerifySnapshotIndex(manifest, "root.ppxar.didx", original); err == nil {
		t.Fatal("an index absent from the manifest was accepted")
	}
	wrongSize := *manifest
	wrongSize.Files = append([]datastore.BackupFileInfo(nil), manifest.Files...)
	wrongSize.Files[0].Size++
	if err := datastore.VerifySnapshotIndex(&wrongSize, "root.mpxar.didx", original); err == nil {
		t.Fatal("signed size mismatch accepted")
	}
}

func TestUnicodePassphraseRecoveryVector(t *testing.T) {
	// Independent Python hashlib.scrypt vector; covers UTF-8 accents and a
	// supplementary-plane character also exercised through Android's Go binding.
	key, err := DeriveEncryptionKey("Café recovery 🐱 phrase 2026")
	if err != nil || key != "07871996c1e8b7a9cd520dd9f649e1a11bf7a8ccc798e7ea7cb02e5e6c248fba" {
		t.Fatal("UTF-8 recovery contract changed", err)
	}
}
