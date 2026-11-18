//go:build unix

package agentfs

import (
	"os"

	"golang.org/x/sys/unix"
)

func nativeReadChange(file *os.File) ([2]int64, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return [2]int64{}, err
	}
	return [2]int64{int64(st.Ctim.Sec), int64(st.Ctim.Nsec)}, nil
}
