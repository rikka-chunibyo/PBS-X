package datastore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
)

// SnapshotFileSource reads encoded manifest blobs and raw snapshot indexes.
type SnapshotFileSource interface {
	DownloadFile(string) ([]byte, error)
}

// VerifiedRootIndexes contains only indexes checked against the completed manifest.
type VerifiedRootIndexes struct {
	Metadata []byte
	Payload  []byte
	Legacy   []byte
}

// LoadPlainRootIndexes checks snapshot identity and integrity without downloading
// payload chunks. Select the archive format from the manifest, so a failed
// split-index download cannot silently fall back to a different archive.
func LoadPlainRootIndexes(source SnapshotFileSource, kind, id string, stamp int64) (*VerifiedRootIndexes, error) {
	raw, err := source.DownloadFile("index.json.blob")
	if err != nil {
		return nil, fmt.Errorf("download completed snapshot manifest: %w", err)
	}
	plain, err := DecodeBlob(nil, raw)
	if err != nil {
		return nil, fmt.Errorf("decode snapshot manifest: %w", err)
	}
	manifest, err := UnmarshalManifest(plain)
	if err != nil {
		return nil, err
	}
	if manifest.BackupType != kind || manifest.BackupID != id || manifest.BackupTime != stamp {
		return nil, fmt.Errorf("snapshot manifest identity mismatch")
	}
	if manifest.Signature != "" || !plainSnapshotMode(manifest.CryptMode) {
		return nil, fmt.Errorf("encrypted or signed snapshots require encryption key support")
	}
	for _, file := range manifest.Files {
		if !plainSnapshotMode(file.CryptMode) {
			return nil, fmt.Errorf("unsupported snapshot encryption mode %q for %s", file.CryptMode, file.Filename)
		}
	}
	return LoadRootIndexes(source, manifest)
}

// LoadRootIndexes selects and verifies indexes from an already authenticated
// manifest. Callers must first check snapshot identity and, for protected
// snapshots, verify its signature using the selected key. Index files themselves
// are unencrypted; their referenced chunks are decoded separately by the caller.
func LoadRootIndexes(source SnapshotFileSource, manifest *Manifest) (*VerifiedRootIndexes, error) {
	files := make(map[string]string, len(manifest.Files))
	for _, file := range manifest.Files {
		if _, exists := files[file.Filename]; exists {
			return nil, fmt.Errorf("duplicate manifest file %q", file.Filename)
		}
		mode := file.CryptMode
		if mode == "" {
			mode = string(CryptModeNone)
		}
		files[file.Filename] = mode
	}
	load := func(name string) ([]byte, error) {
		data, err := source.DownloadFile(name)
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", name, err)
		}
		if err := VerifySnapshotIndex(manifest, name, data); err != nil {
			return nil, fmt.Errorf("verify %s: %w", name, err)
		}
		return data, nil
	}
	result := &VerifiedRootIndexes{}
	_, metadata := files["root.mpxar.didx"]
	_, payload := files["root.ppxar.didx"]
	_, legacy := files["root.pxar.didx"]
	var err error
	if metadata || payload {
		if !metadata || !payload {
			return nil, fmt.Errorf("incomplete split root archive in snapshot manifest")
		}
		if files["root.mpxar.didx"] != files["root.ppxar.didx"] {
			return nil, fmt.Errorf("mixed split-archive encryption is unsupported")
		}
		if result.Metadata, err = load("root.mpxar.didx"); err != nil {
			return nil, err
		}
		if result.Payload, err = load("root.ppxar.didx"); err != nil {
			return nil, err
		}
	} else if legacy {
		if result.Legacy, err = load("root.pxar.didx"); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("snapshot manifest has no root PXAR archive")
	}
	return result, nil
}

func plainSnapshotMode(mode string) bool { return mode == "" || mode == string(CryptModeNone) }

// VerifySnapshotIndex checks index structure, entry checksums and the completed
// manifest's checksum/size before callers trust chunk references or offsets.
func VerifySnapshotIndex(manifest *Manifest, name string, data []byte) error {
	index, err := ParseDynamicIndex(data)
	if err != nil {
		return err
	}
	defer index.Close()
	sum, size := index.ComputeCsum()
	if index.IndexCsum() != sum {
		return fmt.Errorf("archive index checksum mismatch")
	}
	var previous uint64
	for i := 0; i < index.Count(); i++ {
		end := index.Entry(i).EndOffset
		if end <= previous || end > math.MaxInt64 {
			return fmt.Errorf("invalid archive index chunk offset")
		}
		previous = end
	}
	if size == 0 && name != "root.ppxar.didx" {
		return fmt.Errorf("empty root metadata archive")
	}
	return manifest.VerifyFile(name, hex.EncodeToString(sum[:]), size)
}

// VerifiedChunkSource rejects a misaddressed or corrupted chunk before its bytes reach
// the archive reader.
type VerifiedChunkSource struct{ Inner ChunkSource }

func (source *VerifiedChunkSource) GetChunk(digest [32]byte) ([]byte, error) {
	data, err := source.GetDecodedChunk(digest)
	if err != nil {
		return nil, err
	}
	return EncodeBlob(nil, data)
}

// GetDecodedChunk lets the lazy archive reader reuse verified plaintext instead
// of decompressing a compressed chunk a second time.
func (source *VerifiedChunkSource) GetDecodedChunk(digest [32]byte) ([]byte, error) {
	raw, err := source.Inner.GetChunk(digest)
	if err != nil {
		return nil, err
	}
	plain, err := DecodeBlob(nil, raw)
	if err != nil {
		return nil, fmt.Errorf("decode archive chunk: %w", err)
	}
	if sha256.Sum256(plain) != digest {
		return nil, fmt.Errorf("archive chunk digest mismatch")
	}
	return plain, nil
}
