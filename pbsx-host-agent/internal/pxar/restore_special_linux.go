//go:build linux

package pxar

import (
	"context"
	"fmt"
	"github.com/fxamacker/cbor/v2"
	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs/fswire"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strconv"
)

// Anchor native *at operations and symlink xattrs to a parent resolved by Root.
func restoreParent(st *restoreState, p string) (*os.File, string, func(), error) {
	target, closeTarget, err := st.destinationFS(p)
	if err != nil {
		return nil, "", nil, err
	}
	rel, err := target.relative(filepath.Dir(p))
	if err != nil {
		closeTarget()
		return nil, "", nil, err
	}
	parent, err := target.root.OpenRoot(rel)
	if err != nil {
		closeTarget()
		return nil, "", nil, err
	}
	file, err := parent.Open(".")
	if err != nil {
		parent.Close()
		closeTarget()
		return nil, "", nil, err
	}
	closeAll := func() { file.Close(); parent.Close(); closeTarget() }
	return file, filepath.Base(p), closeAll, nil
}
func restoreSymlinkXattr(st *restoreState, p, name string, value []byte) error {
	parent, base, closeParent, err := restoreParent(st, p)
	if err != nil {
		return err
	}
	defer closeParent()
	return unix.Lsetxattr("/proc/self/fd/"+strconv.Itoa(int(parent.Fd()))+"/"+base, name, value, 0)
}
func restoreSpecial(ctx context.Context, st *restoreState, job restoreJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, base, closeParent, err := restoreParent(st, job.dest)
	if err != nil {
		return err
	}
	defer closeParent()
	fd := int(parent.Fd())
	mode := uint32(job.info.RawMode & 0777)
	expected := os.ModeNamedPipe
	if job.info.FileType == pxar.FileTypeFifo {
		err = unix.Mkfifoat(fd, base, mode)
	} else {
		expected = os.ModeSocket
		err = unix.Mknodat(fd, base, unix.S_IFSOCK|mode, 0)
	}
	if err != nil && !os.IsExist(err) {
		return err
	}
	var stat unix.Stat_t
	if err = unix.Fstatat(fd, base, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if (expected == os.ModeNamedPipe && stat.Mode&unix.S_IFMT != unix.S_IFIFO) || (expected == os.ModeSocket && stat.Mode&unix.S_IFMT != unix.S_IFSOCK) {
		return fmt.Errorf("destination special-file type conflict: %s", job.dest)
	}
	if st.noAttr {
		return nil
	}
	if expected == os.ModeSocket {
		return restoreSocketMetadata(ctx, st, fd, base, job)
	}

	opened, err := unix.Openat(fd, base, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(opened), job.dest)
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		file.Close()
		return fmt.Errorf("FIFO changed during restore: %s", job.dest)
	}
	return applyMeta(ctx, st, file, job.info)
}

// Sockets cannot be opened like regular files. Metadata operations remain
// relative to the pinned parent and never follow the final component.
func restoreSocketMetadata(ctx context.Context, st *restoreState, fd int, base string, job restoreJob) error {
	attrs, err := st.client.ListXAttrs(ctx, job.info.EntryRangeStart, job.info.EntryRangeEnd)
	if err != nil {
		st.reportErr(ctx, "list socket xattrs", job.dest, err)
	}
	uid, gid := int(job.info.RawUID), int(job.info.RawGID)
	if v, ok := attrs[XAttrOwner]; ok {
		if parsed, err := strconv.Atoi(string(v)); err == nil {
			uid = parsed
		}
	}
	if v, ok := attrs[XAttrGroup]; ok {
		if parsed, err := strconv.Atoi(string(v)); err == nil {
			gid = parsed
		}
	}
	if st.fsCap.supportsChown {
		st.reportErr(ctx, "socket chown", job.dest, unix.Fchownat(fd, base, uid, gid, unix.AT_SYMLINK_NOFOLLOW))
	}
	if st.fsCap.supportsACLs {
		if d := attrs[XAttrACLs]; detectACLFlavor(d) == aclPosix {
			var entries []fswire.PosixACL
			if err := cbor.Unmarshal(d, &entries); err != nil {
				st.reportErr(ctx, "decode socket ACL", job.dest, err)
			} else {
				var access []fswire.PosixACL
				for _, entry := range entries {
					if !entry.IsDefault {
						switch entry.Tag {
						case "user_obj", "user", "group_obj", "group", "mask", "other":
							access = append(access, entry)
						}
					}
				}
				if len(access) > 0 {
					st.reportErr(ctx, "socket ACL", job.dest, restoreSymlinkXattr(st, job.dest, XATTR_NAME_ACL_ACCESS, packACL(access)))
				}
			}
		}
	}
	st.reportErr(ctx, "socket chmod", job.dest, unix.Fchmodat(fd, base, uint32(job.info.RawMode&07777), unix.AT_SYMLINK_NOFOLLOW))
	if st.fsCap.supportsXAttrs {
		for name, val := range attrs {
			if !IsCanonicalXAttr(name) {
				st.reportErr(ctx, "socket xattr "+name, job.dest, restoreSymlinkXattr(st, job.dest, name, val))
			}
		}
	}
	atime, mtime := unix.Timespec{Sec: job.info.MtimeSecs, Nsec: int64(job.info.MtimeNsecs)}, unix.Timespec{Sec: job.info.MtimeSecs, Nsec: int64(job.info.MtimeNsecs)}
	if value, ok := ParseXattrUnixSecs(attrs[XAttrLastAccessTime]); ok {
		atime = unix.Timespec{Sec: value}
	}
	if value, ok := ParseXattrUnixSecs(attrs[XAttrLastWriteTime]); ok && value != mtime.Sec {
		mtime = unix.Timespec{Sec: value}
	}
	st.reportErr(ctx, "socket timestamps", job.dest, unix.UtimesNanoAt(fd, base, []unix.Timespec{atime, mtime}, unix.AT_SYMLINK_NOFOLLOW))
	return nil
}
