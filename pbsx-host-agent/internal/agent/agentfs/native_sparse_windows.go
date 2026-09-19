//go:build windows

package agentfs

import (
	"errors"

	"golang.org/x/sys/windows"
)

func nativeSparseHoles(path string) (uint64, []sparseRange, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, nil, err
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return 0, nil, err
	}
	defer windows.CloseHandle(h)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return 0, nil, err
	}
	size := uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow)
	if size == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return size, nil, nil
	}

	var holes []sparseRange
	var pos uint64
	for pos < size {
		data, err := sparseSeekAllocatedRanges(h, int64(pos), SeekData, int64(size))
		if err != nil && !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return 0, nil, err
		}
		if uint64(data) > pos {
			holes = append(holes, sparseRange{Offset: pos, Length: uint64(data) - pos})
		}
		if uint64(data) >= size || errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		hole, err := sparseSeekAllocatedRanges(h, data, SeekHole, int64(size))
		if err != nil {
			return 0, nil, err
		}
		if hole <= data {
			break
		}
		pos = uint64(hole)
	}
	return size, holes, nil
}
