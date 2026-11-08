//go:build unix && !linux

package pxar

import (
	"time"

	"golang.org/x/sys/unix"
)

func restoreFileTimes(fd int, atime, mtime time.Time) error {
	return unix.Futimes(fd, []unix.Timeval{unix.NsecToTimeval(atime.UnixNano()), unix.NsecToTimeval(mtime.UnixNano())})
}
