package agentfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/pbs-plus/pxar/format"
)

const sparseMetadataChunkSize = 60 << 10

type sparseRange struct {
	Offset uint64 `cbor:"o"`
	Length uint64 `cbor:"l"`
}

type sparseManifest struct {
	Size   uint64 `cbor:"size"`
	Digest string `cbor:"digest"`
	Chunks uint32 `cbor:"chunks"`
}

func nativeSparseXAttrs(path string) ([]format.XAttr, error) {
	size, holes, err := nativeSparseHoles(path)
	if err != nil || len(holes) == 0 {
		return nil, err
	}
	return encodeSparseXAttrs(size, holes)
}

// encodeSparseXAttrs is deliberately platform-neutral. Windows mock-pipeline
// tests can therefore validate the exact metadata stored by the production
// native accessor without requiring NTFS sparse-file APIs.
func encodeSparseXAttrs(size uint64, holes []sparseRange) ([]format.XAttr, error) {
	if len(holes) == 0 {
		return nil, nil
	}

	encoded, err := cbor.Marshal(holes)
	if err != nil {
		return nil, fmt.Errorf("encode sparse map: %w", err)
	}
	digest := sha256.Sum256(encoded)
	digestHex := hex.EncodeToString(digest[:])
	chunks := (len(encoded) + sparseMetadataChunkSize - 1) / sparseMetadataChunkSize
	manifestData, err := cbor.Marshal(sparseManifest{
		Size: size, Digest: digestHex, Chunks: uint32(chunks),
	})
	if err != nil {
		return nil, fmt.Errorf("encode sparse manifest: %w", err)
	}

	attrs := make([]format.XAttr, 0, chunks+1)
	attrs = append(attrs, format.NewXAttr([]byte(nativeXAttrSparseManifest), manifestData))
	digestTag := digestHex[:16]
	for i := 0; i < chunks; i++ {
		start := i * sparseMetadataChunkSize
		end := min(start+sparseMetadataChunkSize, len(encoded))
		name := fmt.Sprintf("%s%s.%06x", nativeXAttrSparsePrefix, digestTag, i)
		attrs = append(attrs, format.NewXAttr([]byte(name), encoded[start:end]))
	}
	return attrs, nil
}
