//go:build linux

package pxar

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func applySparseLayout(ctx context.Context, st *restoreState, file *os.File, xattrs map[string][]byte, fileSize uint64) {
	holes, present, err := decodeSparseHoles(xattrs, fileSize)
	if err != nil {
		st.reportErr(ctx, "decode sparse layout", file.Name(), err)
		return
	}
	if !present || len(holes) == 0 {
		return
	}
	fd := int(file.Fd())
	for _, hole := range holes {
		err := unix.Fallocate(fd, unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, int64(hole.Offset), int64(hole.Length))
		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EINVAL) {
			st.reportErr(ctx, "restore sparse layout", file.Name(), err)
			return
		}
		if err != nil {
			st.reportErr(ctx, "punch sparse hole", file.Name(), err)
		}
	}
}
