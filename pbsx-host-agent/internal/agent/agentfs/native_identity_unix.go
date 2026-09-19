//go:build unix

package agentfs

import (
	"fmt"

	"github.com/pbs-plus/pxar/backupproxy"
	"golang.org/x/sys/unix"
)

func nativeFileIdentity(path string) (backupproxy.FileIdentity, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(unix.AT_FDCWD, path, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return backupproxy.FileIdentity{}, err
	}
	return backupproxy.FileIdentity{
		Key:   fmt.Sprintf("%x:%x", uint64(st.Dev), uint64(st.Ino)),
		Links: uint64(st.Nlink),
	}, nil
}
