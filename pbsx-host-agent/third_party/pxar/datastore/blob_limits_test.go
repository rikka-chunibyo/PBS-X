package datastore

import (
	"bytes"
	"hash/crc32"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

type zeroBlobReader struct{}

func (zeroBlobReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func compressedBlobFrame(t *testing.T, size int64) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder, err := zstd.NewWriter(&buffer, zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.CopyN(encoder, zeroBlobReader{}, size); err != nil {
		t.Fatal(err)
	}
	if err = encoder.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testCompressedBlob(t *testing.T, compressed []byte, encrypted bool, cc *CryptConfig) []byte {
	t.Helper()
	if encrypted {
		// Encode the compressed frame as plaintext, then label the valid encrypted
		// frame as compressed. The normal encoder refuses oversized plaintext.
		raw, err := EncodeEncryptedBlob(nil, compressed, cc, false)
		if err != nil {
			t.Fatal(err)
		}
		copy(raw[:8], MagicEncrComprBlob[:])
		return raw
	}
	raw := make([]byte, BlobHeaderSize+len(compressed))
	header := BlobHeader{Magic: MagicCompressedBlob, CRC: crc32.ChecksumIEEE(compressed)}
	header.MarshalTo(raw)
	copy(raw[BlobHeaderSize:], compressed)
	return raw
}

func TestBlobDecodersRejectOversizedCompressedOutput(t *testing.T) {
	compressed := compressedBlobFrame(t, MaxBlobSize+1)
	cc, err := NewCryptConfig([32]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	for _, encrypted := range []bool{false, true} {
		raw := testCompressedBlob(t, compressed, encrypted, cc)
		var decoded []byte
		if encrypted {
			decoded, err = DecodeEncryptedBlob(nil, raw, cc)
		} else {
			decoded, err = DecodeBlob(nil, raw)
		}
		if err == nil || decoded != nil {
			t.Errorf("encrypted=%v: accepted oversized decoded blob (%d bytes): %v", encrypted, len(decoded), err)
		}
	}
}

func TestBlobDecoderLimitCoversConcatenatedFrames(t *testing.T) {
	frame := compressedBlobFrame(t, MaxBlobSize/2+1)
	raw := testCompressedBlob(t, append(append([]byte(nil), frame...), frame...), false, nil)
	if decoded, err := DecodeBlob(nil, raw); err == nil || decoded != nil {
		t.Fatalf("accepted oversized concatenated frames (%d bytes): %v", len(decoded), err)
	}
}

func TestBlobDecoderAcceptsExactFormatLimit(t *testing.T) {
	compressed := compressedBlobFrame(t, MaxBlobSize)
	raw := testCompressedBlob(t, compressed, false, nil)
	decoded, err := DecodeBlob(nil, raw)
	if err != nil || len(decoded) != MaxBlobSize {
		t.Fatalf("valid boundary-sized blob refused: size=%d, err=%v", len(decoded), err)
	}
	// Reusing the pooled decoder after large/failed decodes must remain safe.
	data := bytes.Repeat([]byte("small backup"), 100)
	raw, err = EncodeCompressedBlob(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = DecodeBlob(nil, raw)
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatalf("pooled decoder did not recover: %v", err)
	}
}

func TestBlobDecodersRejectOversizedEncodedInput(t *testing.T) {
	raw := make([]byte, MaxBlobSize+EncryptedBlobHeaderSize+1)
	cc, err := NewCryptConfig([32]byte{2})
	if err != nil {
		t.Fatal(err)
	}
	for _, encrypted := range []bool{false, true} {
		if encrypted {
			_, err = DecodeEncryptedBlob(nil, raw, cc)
		} else {
			_, err = DecodeBlob(nil, raw)
		}
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Errorf("encrypted=%v: encoded size was not checked before parsing: %v", encrypted, err)
		}
	}
}

func TestEncryptedBlobRejectsUnknownOrPlainMagic(t *testing.T) {
	cc, err := NewCryptConfig([32]byte{3})
	if err != nil {
		t.Fatal(err)
	}
	for _, magic := range [][8]byte{MagicUncompressedBlob, MagicCompressedBlob, {1, 2, 3, 4, 5, 6, 7, 8}} {
		raw, err := EncodeEncryptedBlob(nil, []byte("backup contents"), cc, false)
		if err != nil {
			t.Fatal(err)
		}
		copy(raw[:8], magic[:])
		if _, err = DecodeEncryptedBlob(nil, raw, cc); err == nil {
			t.Errorf("accepted non-encrypted magic %x", magic)
		}
	}
}
