//go:build windows

package pxar

import (
	"context"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type fileZeroDataInformation struct {
	FileOffset      int64
	BeyondFinalZero int64
}

func applySparseLayout(ctx context.Context, st *restoreState, file *os.File, xattrs map[string][]byte, fileSize uint64) {
	holes, present, err := decodeSparseHoles(xattrs, fileSize)
	if err != nil {
		st.reportErr(ctx, "decode sparse layout", file.Name(), err)
		return
	}
	if !present || len(holes) == 0 {
		return
	}
	h := windows.Handle(file.Fd())
	var returned uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_SET_SPARSE, nil, 0, nil, 0, &returned, nil); err != nil {
		st.reportErr(ctx, "mark file sparse", file.Name(), err)
		return
	}
	for _, hole := range holes {
		info := fileZeroDataInformation{
			FileOffset:      int64(hole.Offset),
			BeyondFinalZero: int64(hole.Offset + hole.Length),
		}
		if err := windows.DeviceIoControl(
			h,
			windows.FSCTL_SET_ZERO_DATA,
			(*byte)(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
			nil,
			0,
			&returned,
			nil,
		); err != nil {
			st.reportErr(ctx, "punch sparse hole", file.Name(), err)
		}
	}
}
