package agentfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/pbs-plus/pxar/format"
)

const (
	nativeADSChunkSize     = 60 << 10
	nativeADSMaxStreamSize = 32 << 20
	nativeADSMaxFileTotal  = 64 << 20
)

type nativeADSManifestEntry struct {
	Name   string `cbor:"name"`
	Size   int64  `cbor:"size"`
	Digest []byte `cbor:"digest"`
	Chunks int    `cbor:"chunks"`
}

type nativeAlternateDataStream struct {
	Name string
	Data []byte
}

// encodeNativeADS is shared by the real Windows enumerator and the Linux
// Windows-pipeline harness. Keeping the packing here makes the mock exercise
// the production digest, chunk-key, manifest, and safety-limit behavior.
func encodeNativeADS(streams []nativeAlternateDataStream) ([]format.XAttr, error) {
	if len(streams) == 0 {
		return nil, nil
	}

	attrs := make([]format.XAttr, 0, len(streams)+4)
	manifest := make([]nativeADSManifestEntry, 0, len(streams))
	totalADSBytes := 0
	for streamIndex, stream := range streams {
		if stream.Name == "" {
			return nil, fmt.Errorf("alternate data stream name is empty")
		}
		if len(stream.Data) > nativeADSMaxStreamSize {
			return nil, fmt.Errorf("alternate data stream %q is %d bytes; streams larger than %d bytes require the future ADS payload archive", stream.Name, len(stream.Data), nativeADSMaxStreamSize)
		}
		totalADSBytes += len(stream.Data)
		if totalADSBytes > nativeADSMaxFileTotal {
			return nil, fmt.Errorf("alternate data streams exceed the %d byte metadata safety limit", nativeADSMaxFileTotal)
		}

		digest := sha256.Sum256(stream.Data)
		chunks := 0
		if len(stream.Data) > 0 {
			chunks = (len(stream.Data) + nativeADSChunkSize - 1) / nativeADSChunkSize
		}
		manifest = append(manifest, nativeADSManifestEntry{
			Name: stream.Name, Size: int64(len(stream.Data)),
			Digest: append([]byte(nil), digest[:]...), Chunks: chunks,
		})
		digestTag := hex.EncodeToString(digest[:8])
		for chunkIndex := 0; chunkIndex < chunks; chunkIndex++ {
			start := chunkIndex * nativeADSChunkSize
			end := min(start+nativeADSChunkSize, len(stream.Data))
			key := fmt.Sprintf("%s%04x.%s.%06x", nativeXAttrADSPrefix, streamIndex, digestTag, chunkIndex)
			attrs = append(attrs, format.NewXAttr([]byte(key), stream.Data[start:end]))
		}
	}

	encoded, err := cbor.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("encode ADS manifest: %w", err)
	}
	attrs = append(attrs, format.NewXAttr([]byte(nativeXAttrADSManifest), encoded))
	return attrs, nil
}
