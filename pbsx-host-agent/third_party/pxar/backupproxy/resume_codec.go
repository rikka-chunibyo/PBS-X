package backupproxy

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/pbs-plus/pxar/buzhash"
)

const maxResumePlaintext = 50 << 20
const maxResumeCiphertext = 16 << 20

var resumeMagic = []byte("PBSXRS1\x00")

// The resume key is independent of optional backup encryption: checkpoint
// filenames, traversal metadata and file-prefix hashes are always encrypted on
// the server. Only the small per-run key/identity/token live in the device vault.
type documentResumeEnvelope struct {
	Version       int                `json:"version"`
	Sequence      uint64             `json:"sequence"`
	Binding       string             `json:"binding"`
	Streams       []StreamCheckpoint `json:"streams"`
	Walk          json.RawMessage    `json:"walk"`
	UploadedBytes uint64             `json:"uploaded_bytes"`
	ChunkConfig   buzhash.Config     `json:"chunk_config"`
}

func resumeAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("resume state requires a 256-bit key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func resumeAAD(binding string, sequence uint64) []byte {
	var counter [8]byte
	binary.LittleEndian.PutUint64(counter[:], sequence)
	aad := append([]byte("PBS-X resume state v1\x00"), []byte(binding)...)
	return append(aad, counter[:]...)
}

func sealResumeEnvelope(key []byte, envelope documentResumeEnvelope) ([]byte, error) {
	if envelope.Version != 1 || envelope.Sequence == 0 || len(envelope.Binding) != 64 || len(envelope.Streams) != 2 || len(envelope.Walk) == 0 {
		return nil, errors.New("invalid resume envelope")
	}
	aead, err := resumeAEAD(key)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(plain) > maxResumePlaintext {
		return nil, errors.New("resume metadata exceeds 50 MiB")
	}
	compressor, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	compressed := compressor.EncodeAll(plain, nil)
	compressor.Close()
	if len(resumeMagic)+aead.NonceSize()+len(compressed)+aead.Overhead() > maxResumeCiphertext {
		return nil, errors.New("compressed resume metadata exceeds server limit; choose fewer sources")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := append(append([]byte(nil), resumeMagic...), nonce...)
	return aead.Seal(sealed, nonce, compressed, resumeAAD(envelope.Binding, envelope.Sequence)), nil
}

func openResumeEnvelope(key []byte, binding string, sequence uint64, sealed []byte) (documentResumeEnvelope, error) {
	var envelope documentResumeEnvelope
	aead, err := resumeAEAD(key)
	if err != nil {
		return envelope, err
	}
	if len(sealed) > maxResumeCiphertext || len(sealed) < len(resumeMagic)+aead.NonceSize()+aead.Overhead() || !bytes.HasPrefix(sealed, resumeMagic) {
		return envelope, errors.New("invalid encrypted resume state")
	}
	nonce := sealed[len(resumeMagic) : len(resumeMagic)+aead.NonceSize()]
	compressed, err := aead.Open(nil, nonce, sealed[len(resumeMagic)+aead.NonceSize():], resumeAAD(binding, sequence))
	if err != nil {
		return envelope, errors.New("resume state authentication failed; check the saved run/key")
	}
	reader, err := zstd.NewReader(bytes.NewReader(compressed), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(64<<20), zstd.WithDecoderMaxWindow(64<<20))
	if err != nil {
		return envelope, err
	}
	defer reader.Close()
	plain, err := io.ReadAll(io.LimitReader(reader, maxResumePlaintext+1))
	if err != nil {
		return envelope, err
	}
	if len(plain) > maxResumePlaintext {
		return envelope, errors.New("decompressed resume state exceeds limit")
	}
	d := json.NewDecoder(bytes.NewReader(plain))
	d.DisallowUnknownFields()
	if err = d.Decode(&envelope); err != nil {
		return envelope, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return envelope, errors.New("trailing resume envelope data")
	}
	if envelope.Version != 1 || envelope.Sequence != sequence || envelope.Binding != binding || len(envelope.Streams) != 2 || len(envelope.Walk) == 0 {
		return envelope, errors.New("resume state identity or sequence mismatch")
	}
	return envelope, nil
}
