//go:build linux

package pxar

import (
	"errors"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

func restoreFileTimes(fd int, atime, mtime time.Time) error {
	a, err := unix.TimeToTimespec(atime)
	if err != nil {
		return err
	}
	m, err := unix.TimeToTimespec(mtime)
	if err != nil {
		return err
	}
	ts := []unix.Timespec{a, m}
	err = unix.UtimesNanoAt(fd, "", ts, unix.AT_EMPTY_PATH)
	if !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOSYS) {
		return err
	}
	// Older kernels lack AT_EMPTY_PATH for utimensat. This procfs link points
	// to our still-open, confined descriptor, not an untrusted destination path.
	return unix.UtimesNano("/proc/self/fd/"+strconv.Itoa(fd), ts)
}
