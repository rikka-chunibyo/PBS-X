//go:build windows

package agentfs

import (
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"github.com/pbs-plus/pxar/format"
	"golang.org/x/sys/windows"
)

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
		if err == windows.ERROR_INVALID_FUNCTION || err == windows.ERROR_NOT_SUPPORTED || err == windows.ERROR_FILE_NOT_FOUND {
			return nil, nil
		}
		return nil, err
	}
	if len(streams) == 0 {
		return nil, nil
	}

	encodedStreams := make([]nativeAlternateDataStream, 0, len(streams))
	for _, stream := range streams {
		data, err := readAlternateDataStream(path, stream)
		if err != nil {
			return nil, fmt.Errorf("read alternate data stream %q: %w", stream, err)
		}
		encodedStreams = append(encodedStreams, nativeAlternateDataStream{Name: stream, Data: data})
	}
	return encodeNativeADS(encodedStreams)
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
		if errno, isErrno := nextErr.(syscall.Errno); isErrno && errno == windows.ERROR_HANDLE_EOF {
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
