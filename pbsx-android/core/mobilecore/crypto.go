package mobilecore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pbs-plus/pxar/datastore"
	"golang.org/x/crypto/scrypt"
)

const settingsFormat = "pbsx-settings-encrypted"
const settingsLimit = 16 << 20

var settingsAAD = []byte("pbsx-settings-encrypted:1:scrypt-65536-8-1:aes-256-gcm")

func parseBackupKey(raw string) (*datastore.CryptConfig, error) {
	data, err := hex.DecodeString(raw)
	if err != nil || len(data) != 32 {
		return nil, errors.New("backup encryption key must be 64 hexadecimal characters")
	}
	var key [32]byte
	copy(key[:], data)
	return datastore.NewCryptConfig(key)
}
func EncryptionFingerprint(raw string) (string, error) {
	cc, err := parseBackupKey(raw)
	if err != nil {
		return "", err
	}
	return datastore.FormatFingerprint(cc.Fingerprint()), nil
}
func GenerateEncryptionKey() (string, error) {
	key, err := datastore.CreateRandomKey()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(key[:]), nil
}
func connectionKeys(cfg configuration) (*datastore.CryptConfig, map[string]*datastore.CryptConfig, error) {
	keys := map[string]*datastore.CryptConfig{}
	if len(cfg.DecryptionKeys) > 100 {
		return nil, nil, errors.New("at most 100 encryption keys are supported")
	}
	var selected *datastore.CryptConfig
	if cfg.EncryptionEnabled {
		var err error
		selected, err = parseBackupKey(cfg.EncryptionKey)
		if err != nil {
			return nil, nil, errors.New("encryption is enabled but its key is missing or invalid")
		}
		keys[datastore.FormatFingerprint(selected.Fingerprint())] = selected
	}
	for _, raw := range cfg.DecryptionKeys {
		key, err := parseBackupKey(raw)
		if err != nil {
			return nil, nil, err
		}
		keys[datastore.FormatFingerprint(key.Fingerprint())] = key
	}
	return selected, keys, nil
}

type settingsEnvelope struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	KDF     string `json:"kdf"`
	Salt    []byte `json:"salt"`
	Nonce   []byte `json:"nonce"`
	Data    []byte `json:"data"`
}

// Export passwords are used for this file only, never as backup keys.
func EncryptSettings(plain, password string) (string, error) {
	if password == "" {
		return "", errors.New("enter an export password")
	}
	if len(plain) > settingsLimit || !json.Valid([]byte(plain)) {
		return "", errors.New("invalid or oversized settings JSON")
	}
	salt := make([]byte, 32)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	aead, err := settingsCipher(password, salt)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(settingsEnvelope{settingsFormat, 1, "scrypt-65536-8-1", salt, nonce, aead.Seal(nil, nonce, []byte(plain), settingsAAD)})
	return string(raw), err
}
func settingsCipher(password string, salt []byte) (cipher.AEAD, error) {
	key, err := scrypt.Key([]byte(password), salt, 65536, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func DecryptSettings(export, password string) (string, error) {
	if len(export) > settingsLimit*2 || !json.Valid([]byte(export)) {
		return "", errors.New("settings export is too large")
	}
	var e settingsEnvelope
	decoder := json.NewDecoder(strings.NewReader(export))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&e); err != nil {
		return "", errors.New("invalid encrypted settings export")
	}
	if e.Format != settingsFormat || e.Version != 1 || e.KDF != "scrypt-65536-8-1" || len(e.Salt) != 32 || len(e.Nonce) != 12 || len(e.Data) > settingsLimit+16 || len(e.Data) < 16 {
		return "", errors.New("unsupported or damaged settings encryption envelope")
	}
	if password == "" {
		return "", errors.New("enter the export password")
	}
	aead, err := settingsCipher(password, e.Salt)
	if err != nil {
		return "", err
	}
	plain, err := aead.Open(nil, e.Nonce, e.Data, settingsAAD)
	if err != nil {
		return "", errors.New("incorrect password or damaged settings export")
	}
	if !json.Valid(plain) {
		return "", errors.New("decrypted settings are not valid JSON")
	}
	return string(plain), nil
}

// Verify the signed manifest before accepting indexes or selecting a local key.
func (c *Client) snapshotManifest(raw []byte, kind, id string, stamp int64) (*datastore.Manifest, *datastore.CryptConfig, error) {
	plain, err := datastore.DecodeBlob(nil, raw)
	if err != nil {
		return nil, nil, err
	}
	m, err := datastore.UnmarshalManifest(plain)
	if err != nil {
		return nil, nil, err
	}
	if m.BackupType != kind || m.BackupID != id || m.BackupTime != stamp {
		return nil, nil, errors.New("snapshot manifest identity mismatch")
	}
	encrypted := m.CryptMode == string(datastore.CryptModeEncrypt)
	for _, f := range m.Files {
		encrypted = encrypted || f.CryptMode == string(datastore.CryptModeEncrypt)
	}
	if !encrypted && m.Signature == "" {
		return m, nil, nil
	}
	var info datastore.UnprotectedInfo
	if err := json.Unmarshal(m.Unprotected, &info); err != nil {
		return nil, nil, errors.New("missing encryption key fingerprint")
	}
	key := c.keys[info.KeyFingerprint]
	if key == nil {
		return nil, nil, fmt.Errorf("import the encryption key for this snapshot: %s", info.KeyFingerprint)
	}
	if err := datastore.VerifyManifestSignature(m, key); err != nil {
		return nil, nil, errors.New("snapshot signature verification failed")
	}
	return m, key, nil
}
func verifySnapshotIndex(m *datastore.Manifest, name string, data []byte) error {
	idx, err := datastore.ParseDynamicIndex(data)
	if err != nil {
		return err
	}
	sum, size := idx.ComputeCsum()
	if idx.IndexCsum() != sum {
		return errors.New("archive index checksum mismatch")
	}
	return m.VerifyFile(name, hex.EncodeToString(sum[:]), size)
}

type verifiedChunkSource struct {
	inner     datastore.ChunkSource
	key       *datastore.CryptConfig
	encrypted bool
}

func (s *verifiedChunkSource) GetChunk(digest [32]byte) ([]byte, error) {
	raw, err := s.inner.GetChunk(digest)
	if err != nil {
		return nil, err
	}
	var data []byte
	if s.encrypted {
		data, err = datastore.DecodeEncryptedBlob(nil, raw, s.key)
	} else {
		data, err = datastore.DecodeBlob(nil, raw)
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if s.encrypted {
		sum = s.key.ComputeDigest(data)
	}
	if sum != digest {
		return nil, errors.New("archive chunk digest mismatch")
	}
	return datastore.EncodeBlob(nil, data)
}
