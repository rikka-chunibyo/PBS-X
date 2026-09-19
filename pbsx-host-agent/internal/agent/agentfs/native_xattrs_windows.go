//go:build windows

package agentfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"github.com/fxamacker/cbor/v2"
	"github.com/pbs-plus/pxar/format"
	"golang.org/x/sys/windows"
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

type win32FindStreamData struct {
	StreamSize int64
	StreamName [296]uint16 // MAX_PATH + 36
}

var (
	kernel32Streams      = windows.NewLazySystemDLL("kernel32.dll")
	procFindFirstStreamW = kernel32Streams.NewProc("FindFirstStreamW")
	procFindNextStreamW  = kernel32Streams.NewProc("FindNextStreamW")
	procFindCloseStreams = kernel32Streams.NewProc("FindClose")
)

// nativePlatformXAttrs captures NTFS alternate data streams in PXAR metadata.
// Stream content is split across bounded xattrs. The content digest is part of
// every chunk key because PBS metadata reuse compares xattr identity; changing
// the ADS therefore changes the metadata key set even when size/chunk count is
// unchanged.
func nativePlatformXAttrs(path string) ([]format.XAttr, error) {
	streams, err := enumerateAlternateDataStreams(path)
	if err != nil {
		// Filesystems other than NTFS and some reparse objects may not support
		// stream enumeration. Treat that as no ADS rather than failing backup.
		if err == syscall.ERROR_INVALID_FUNCTION || err == syscall.ERROR_NOT_SUPPORTED || err == syscall.ERROR_FILE_NOT_FOUND {
			return nil, nil
		}
		return nil, err
	}
	if len(streams) == 0 {
		return nil, nil
	}

	attrs := make([]format.XAttr, 0, len(streams)+4)
	manifest := make([]nativeADSManifestEntry, 0, len(streams))
	totalADSBytes := 0
	for streamIndex, stream := range streams {
		data, err := readAlternateDataStream(path, stream)
		if err != nil {
			return nil, fmt.Errorf("read alternate data stream %q: %w", stream, err)
		}
		if len(data) > nativeADSMaxStreamSize {
			return nil, fmt.Errorf("alternate data stream %q is %d bytes; streams larger than %d bytes require the future ADS payload archive", stream, len(data), nativeADSMaxStreamSize)
		}
		totalADSBytes += len(data)
		if totalADSBytes > nativeADSMaxFileTotal {
			return nil, fmt.Errorf("alternate data streams on %q exceed the %d byte metadata safety limit", path, nativeADSMaxFileTotal)
		}
		digest := sha256.Sum256(data)
		chunks := 0
		if len(data) > 0 {
			chunks = (len(data) + nativeADSChunkSize - 1) / nativeADSChunkSize
		}
		manifest = append(manifest, nativeADSManifestEntry{
			Name:   stream,
			Size:   int64(len(data)),
			Digest: append([]byte(nil), digest[:]...),
			Chunks: chunks,
		})
		digestTag := hex.EncodeToString(digest[:8])
		for chunkIndex := 0; chunkIndex < chunks; chunkIndex++ {
			start := chunkIndex * nativeADSChunkSize
			end := min(start+nativeADSChunkSize, len(data))
			key := fmt.Sprintf("%s%04x.%s.%06x", nativeXAttrADSPrefix, streamIndex, digestTag, chunkIndex)
			attrs = append(attrs, format.NewXAttr([]byte(key), data[start:end]))
		}
	}

	encoded, err := cbor.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("encode ADS manifest: %w", err)
	}
	attrs = append(attrs, format.NewXAttr([]byte(nativeXAttrADSManifest), encoded))
	return attrs, nil
}

func nativePlatformFCaps(string) ([]byte, error) { return nil, nil }

func enumerateAlternateDataStreams(path string) ([]string, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var data win32FindStreamData
	handle, _, callErr := procFindFirstStreamW.Call(
		uintptr(unsafe.Pointer(ptr)),
		0, // FindStreamInfoStandard
		uintptr(unsafe.Pointer(&data)),
		0,
	)
	if handle == uintptr(windows.InvalidHandle) {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return nil, errno
		}
		return nil, os.ErrNotExist
	}
	defer procFindCloseStreams.Call(handle)

	out := make([]string, 0, 2)
	for {
		raw := windows.UTF16ToString(data.StreamName[:])
		if name, ok := parseAlternateStreamName(raw); ok {
			out = append(out, name)
		}
		data = win32FindStreamData{}
		ok, _, nextErr := procFindNextStreamW.Call(handle, uintptr(unsafe.Pointer(&data)))
		if ok != 0 {
			continue
		}
		if errno, isErrno := nextErr.(syscall.Errno); isErrno && errno == syscall.ERROR_HANDLE_EOF {
			break
		}
		if errno, isErrno := nextErr.(syscall.Errno); isErrno && errno != 0 {
			return nil, errno
		}
		break
	}
	return out, nil
}

func parseAlternateStreamName(raw string) (string, bool) {
	// FindFirstStreamW returns names as ::$DATA (unnamed/default stream) or
	// :stream-name:$DATA. Only preserve named $DATA streams.
	if raw == "::$DATA" || !strings.HasPrefix(raw, ":") || !strings.HasSuffix(raw, ":$DATA") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(raw, ":"), ":$DATA")
	if name == "" {
		return "", false
	}
	return name, true
}

func readAlternateDataStream(path, stream string) ([]byte, error) {
	f, err := os.Open(path + ":" + stream)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
