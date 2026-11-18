package pbsverify

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-plus/pxar/transfer"
	"github.com/pbs-x/pbsx-host-agent/internal/testutil/pbsfixture"
)

func TestLoadRootIndexes(t *testing.T) {
	for _, split := range []bool{false, true} {
		s := pbsfixture.New(t, split)
		indexes, err := LoadRootIndexes(s, "host", pbsfixture.HostID, pbsfixture.Stamp)
		if err != nil {
			t.Fatal(err)
		}
		if split != (indexes.Metadata != nil && indexes.Payload != nil) || (!split && indexes.Legacy == nil) {
			t.Fatalf("wrong archive format: %+v", indexes)
		}
	}
}

func TestLoadRootIndexesRejectsInvalidSnapshot(t *testing.T) {
	tests := []struct {
		name string
		edit func(*testing.T, *pbsfixture.Snapshot)
	}{
		{"wrong type", func(t *testing.T, s *pbsfixture.Snapshot) { s.Manifest.BackupType = "vm"; s.WriteManifest(t) }},
		{"wrong host", func(t *testing.T, s *pbsfixture.Snapshot) { s.Manifest.BackupID = "different"; s.WriteManifest(t) }},
		{"wrong timestamp", func(t *testing.T, s *pbsfixture.Snapshot) { s.Manifest.BackupTime++; s.WriteManifest(t) }},
		{"missing manifest", func(t *testing.T, s *pbsfixture.Snapshot) { delete(s.Files, "index.json.blob") }},
		{"invalid manifest blob", func(t *testing.T, s *pbsfixture.Snapshot) { s.Files["index.json.blob"] = []byte("incomplete") }},
		{"no root", func(t *testing.T, s *pbsfixture.Snapshot) { s.Manifest.Files = nil; s.WriteManifest(t) }},
		{"missing root index", func(t *testing.T, s *pbsfixture.Snapshot) { delete(s.Files, "root.pxar.didx") }},
		{"duplicate filename", func(t *testing.T, s *pbsfixture.Snapshot) {
			s.Manifest.Files = append(s.Manifest.Files, s.Manifest.Files[0])
			s.WriteManifest(t)
		}},
		{"encrypted", func(t *testing.T, s *pbsfixture.Snapshot) {
			s.Manifest.Files[0].CryptMode = "encrypt"
			s.WriteManifest(t)
		}},
		{"signed", func(t *testing.T, s *pbsfixture.Snapshot) {
			s.Manifest.Signature = "signature-requires-key"
			s.WriteManifest(t)
		}},
		{"unknown crypt mode", func(t *testing.T, s *pbsfixture.Snapshot) { s.Manifest.CryptMode = "unknown"; s.WriteManifest(t) }},
		{"wrong manifest checksum", func(t *testing.T, s *pbsfixture.Snapshot) {
			s.Manifest.Files[0].CSum = strings.Repeat("0", 64)
			s.WriteManifest(t)
		}},
		{"wrong manifest size", func(t *testing.T, s *pbsfixture.Snapshot) { s.Manifest.Files[0].Size++; s.WriteManifest(t) }},
		{"corrupt index entries", func(t *testing.T, s *pbsfixture.Snapshot) {
			s.Files["root.pxar.didx"][datastore.IndexHeaderSize+8] ^= 1
		}},
		{"truncated index", func(t *testing.T, s *pbsfixture.Snapshot) {
			s.Files["root.pxar.didx"] = s.Files["root.pxar.didx"][:datastore.IndexHeaderSize+1]
		}},
		{"incomplete split does not fall back", func(t *testing.T, s *pbsfixture.Snapshot) {
			f := s.Manifest.Files[0]
			f.Filename = "root.mpxar.didx"
			s.Manifest.Files = append(s.Manifest.Files, f)
			s.WriteManifest(t)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := pbsfixture.New(t, false)
			test.edit(t, s)
			if _, err := LoadRootIndexes(s, "host", pbsfixture.HostID, pbsfixture.Stamp); err == nil {
				t.Fatal("accepted invalid snapshot")
			}
		})
	}
}

func TestLoadRootIndexesRejectsNonIncreasingOffsets(t *testing.T) {
	s := pbsfixture.New(t, false)
	writer := datastore.NewDynamicIndexWriter(pbsfixture.Stamp)
	writer.Add(20, [32]byte{})
	writer.Add(10, [32]byte{})
	data, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if err := datastore.VerifySnapshotIndex(&s.Manifest, "root.pxar.didx", data); err == nil || !strings.Contains(err.Error(), "offset") {
		t.Fatalf("invalid offsets: %v", err)
	}
}

type chunkFunc func([32]byte) ([]byte, error)

func (f chunkFunc) GetChunk(digest [32]byte) ([]byte, error) { return f(digest) }

func TestChunkSourceChecksPlaintextDigest(t *testing.T) {
	data := bytes.Repeat([]byte("compressible-content"), 100)
	digest := sha256.Sum256(data)
	for _, encode := range []func([]byte, []byte) ([]byte, error){datastore.EncodeBlob, datastore.EncodeCompressedBlob} {
		blob, err := encode(nil, data)
		if err != nil {
			t.Fatal(err)
		}
		source := &ChunkSource{Inner: chunkFunc(func([32]byte) ([]byte, error) { return blob, nil })}
		decoded, err := source.GetDecodedChunk(digest)
		if err != nil || !bytes.Equal(decoded, data) {
			t.Fatalf("valid chunk rejected: %v", err)
		}
		if _, err := source.GetChunk([32]byte{}); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
			t.Fatalf("accepted misaddressed chunk with valid CRC: %v", err)
		}
		encoded, err := source.GetChunk(digest)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err = datastore.DecodeBlob(nil, encoded)
		if err != nil || !bytes.Equal(decoded, data) {
			t.Fatalf("chunk source contract broken: %v", err)
		}
		blob[len(blob)-1] ^= 1
		if _, err := source.GetDecodedChunk(digest); err == nil {
			t.Fatal("accepted corrupt CRC")
		}
	}
}

func TestChunkSourceReportsDownloadError(t *testing.T) {
	want := errors.New("fixture download failed")
	source := &ChunkSource{Inner: chunkFunc(func([32]byte) ([]byte, error) { return nil, want })}
	if _, err := source.GetDecodedChunk([32]byte{}); !errors.Is(err, want) {
		t.Fatalf("lost download error: %v", err)
	}
}

func TestLazyReaderUsesVerifiedDecodedChunk(t *testing.T) {
	data := bytes.Repeat([]byte("compressed"), 100)
	digest := sha256.Sum256(data)
	blob, err := datastore.EncodeCompressedBlob(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	writer := datastore.NewDynamicIndexWriter(pbsfixture.Stamp)
	writer.Add(uint64(len(data)), digest)
	raw, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	index, err := datastore.ParseDynamicIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	source := &ChunkSource{Inner: chunkFunc(func([32]byte) ([]byte, error) { return blob, nil })}
	reader := transfer.NewReadSeeker(index, source, 1)
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("verified lazy read: %v", err)
	}
}
