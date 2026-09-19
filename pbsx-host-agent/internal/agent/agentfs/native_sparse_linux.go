//go:build linux

package agentfs

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func nativeSparseHoles(path string) (uint64, []sparseRange, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, nil, err
	}
	if st.Size() <= 0 || !st.Mode().IsRegular() {
		return uint64(max(st.Size(), 0)), nil, nil
	}
	size := uint64(st.Size())
	fd := int(f.Fd())
	var holes []sparseRange
	var pos uint64
	for pos < size {
		data, err := unix.Seek(fd, int64(pos), unix.SEEK_DATA)
		if err != nil {
			if errors.Is(err, unix.ENXIO) {
				holes = append(holes, sparseRange{Offset: pos, Length: size - pos})
				break
			}
			if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
				return size, nil, nil
			}
			return 0, nil, err
		}
		if uint64(data) > pos {
			holes = append(holes, sparseRange{Offset: pos, Length: uint64(data) - pos})
		}
		hole, err := unix.Seek(fd, data, unix.SEEK_HOLE)
		if err != nil {
			if errors.Is(err, unix.ENXIO) {
				break
			}
			if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
				return size, nil, nil
			}
			return 0, nil, err
		}
		if hole <= data {
			break
		}
		pos = uint64(hole)
	}
	return size, holes, nil
}
