//go:build unix

package pxar

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fxamacker/cbor/v2"
	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/format"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs/fswire"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
	"golang.org/x/sys/unix"
)

const (
	ACL_EA_VERSION         = 0x0002
	XATTR_NAME_ACL_ACCESS  = "system.posix_acl_access"
	XATTR_NAME_ACL_DEFAULT = "system.posix_acl_default"
)

// reported immediately via reportErr; the function always returns nil since
// metadata failures are non-fatal (content is already in place).
func applyMeta(ctx context.Context, st *restoreState, file *os.File, e pxar.FileInfo) error {
	defer func() {
		if err := file.Close(); err != nil {
			log.Error(err, "")
		}
	}()

	fd := int(file.Fd())
	path := file.Name()

	uid, gid := int(e.RawUID), int(e.RawGID)

	atime := format.StatxTimestamp{Secs: e.MtimeSecs, Nanos: e.MtimeNsecs}.Time()
	mtime := atime

	xattrs, lerr := st.client.ListXAttrs(ctx, e.EntryRangeStart, e.EntryRangeEnd)
	if lerr != nil {
		st.reportErr(ctx, "list xattrs", path, lerr)
	}

	if lerr == nil && len(xattrs) > 0 {
		if d, ok := xattrs[XAttrOwner]; ok {
			if id, perr := strconv.Atoi(string(d)); perr == nil {
				uid = id
			}
			delete(xattrs, XAttrOwner)
		}
		if d, ok := xattrs[XAttrGroup]; ok {
			if id, perr := strconv.Atoi(string(d)); perr == nil {
				gid = id
			}
			delete(xattrs, XAttrGroup)
		}

		if d, ok := xattrs[XAttrLastAccessTime]; ok {
			if ts, ok := ParseXattrUnixSecs(d); ok {
				atime = time.Unix(ts, 0)
			}
			delete(xattrs, XAttrLastAccessTime)
		}
		if d, ok := xattrs[XAttrLastWriteTime]; ok {
			if ts, ok := ParseXattrUnixSecs(d); ok && ts != mtime.Unix() {
				mtime = time.Unix(ts, 0)
			}
			delete(xattrs, XAttrLastWriteTime)
		}
	}

	// Chown can clear special mode bits and file capabilities. Resolve the
	// platform ownership overrides before applying ownership, ACLs, the final
	// mode and finally capability xattrs.
	if st.fsCap.supportsChown {
		st.reportErr(ctx, "chown", path, unix.Fchown(fd, uid, gid))
	}

	if lerr == nil && len(xattrs) > 0 {
		if st.fsCap.supportsACLs {
			if d, ok := xattrs[XAttrACLs]; ok {
				if detectACLFlavor(d) == aclPosix {
					var entries []fswire.PosixACL
					if uerr := cbor.Unmarshal(d, &entries); uerr != nil {
						st.reportErr(ctx, "decode acls", path, uerr)
					} else {
						applyUnixACLsFd(ctx, st, fd, path, entries)
					}
				}
				delete(xattrs, XAttrACLs)
			}
		} else {
			delete(xattrs, XAttrACLs)
		}
	}

	st.reportErr(ctx, "chmod", path, unix.Fchmod(fd, uint32(e.RawMode&07777)))

	if lerr == nil {
		if st.fsCap.supportsXAttrs {
			for name, val := range xattrs {
				if IsCanonicalXAttr(name) {
					continue
				}
				st.reportErr(ctx, "setxattr "+name, path, unix.Fsetxattr(fd, name, val, 0))
			}
		}
	}

	st.reportErr(ctx, "utimes", path, restoreFileTimes(fd, atime, mtime))

	return nil
}

func applyUnixACLsFd(ctx context.Context, st *restoreState, fd int, path string, entries []fswire.PosixACL) {
	knownTags := map[string]struct{}{
		"user_obj": {}, "user": {}, "group_obj": {},
		"group": {}, "mask": {}, "other": {},
	}
	var acc, def []fswire.PosixACL
	for _, ent := range entries {
		if _, ok := knownTags[ent.Tag]; !ok {
			continue
		}
		if ent.IsDefault {
			def = append(def, ent)
		} else {
			acc = append(acc, ent)
		}
	}
	if len(acc) > 0 {
		st.reportErr(ctx, "set acl access", path, unix.Fsetxattr(fd, XATTR_NAME_ACL_ACCESS, packACL(acc), 0))
	}
	if len(def) > 0 {
		st.reportErr(ctx, "set acl default", path, unix.Fsetxattr(fd, XATTR_NAME_ACL_DEFAULT, packACL(def), 0))
	}
}

func packACL(entries []fswire.PosixACL) []byte {
	buf := make([]byte, 4+(8*len(entries)))
	binary.LittleEndian.PutUint32(buf[0:4], uint32(ACL_EA_VERSION))
	tags := map[string]uint16{
		"user_obj": 0x01, "user": 0x02, "group_obj": 0x04,
		"group": 0x08, "mask": 0x10, "other": 0x20,
	}
	for i, ent := range entries {
		off := 4 + (i * 8)
		binary.LittleEndian.PutUint16(buf[off:off+2], tags[ent.Tag])
		binary.LittleEndian.PutUint16(buf[off+2:off+4], uint16(ent.Perms))
		binary.LittleEndian.PutUint32(buf[off+4:off+8], uint32(ent.ID))
	}
	return buf
}

// applyMetaSymlink applies owner and xattrs to a symlink. Each error is
// reported immediately via reportErr; the function always returns nil.
func applyMetaSymlink(ctx context.Context, st *restoreState, path string, e pxar.FileInfo) error {
	uid, gid := int(e.RawUID), int(e.RawGID)
	xattrs, lerr := st.client.ListXAttrs(ctx, e.EntryRangeStart, e.EntryRangeEnd)
	if lerr != nil {
		st.reportErr(ctx, "list xattrs", path, lerr)
	}

	if lerr == nil && len(xattrs) > 0 {
		if d, ok := xattrs[XAttrOwner]; ok {
			if id, perr := strconv.Atoi(string(d)); perr == nil {
				uid = id
			}
		}
		if d, ok := xattrs[XAttrGroup]; ok {
			if id, perr := strconv.Atoi(string(d)); perr == nil {
				gid = id
			}
		}
	}

	if st.fsCap.supportsChown {
		st.reportErr(ctx, "lchown", path, restoreSymlinkChown(st, path, uid, gid))
	}

	if lerr == nil && st.fsCap.supportsXAttrs {
		for name, val := range xattrs {
			if IsCanonicalXAttr(name) {
				continue
			}
			st.reportErr(ctx, "lsetxattr "+name, path, restoreSymlinkXattr(st, path, name, val))
		}
	}
	return nil
}

// applyTempMode sets a freshly-written temp file to 0666 (subject to umask)
func applyTempMode(st *restoreState, path string, rawMode uint64) error {
	mode := os.FileMode(0o666)
	if rawMode != 0 {
		mode = os.FileMode(rawMode & 0o777)
	}
	target, closeTarget, err := st.destinationFS(path)
	if err != nil {
		return err
	}
	defer closeTarget()
	file, err := target.open(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Chmod(mode)
}

func restoreDir(ctx context.Context, st *restoreState, job restoreJob) error {
	dst := job.dest
	dirEntry := job.info
	targetFS, closeTarget, err := st.destinationFS(dst)
	if err != nil {
		return err
	}
	defer closeTarget()
	if err := targetFS.mkdir(dst); err != nil {
		return err
	}

	entries, err := st.client.ReadDir(ctx, dirEntry.EntryRangeEnd)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if err := validRestoreName(e.Name()); err != nil {
			return err
		}
		target := filepath.Join(dst, e.Name())
		childSrc := path.Join(job.srcPath, e.Name())

		switch e.FileType {
		case pxar.FileTypeDirectory, pxar.FileTypeFile, pxar.FileTypeSymlink, pxar.FileTypeHardlink:
			st.wg.Add(1)
			go func(t, s string, info pxar.FileInfo) {
				select {
				case st.jobs <- restoreJob{dest: t, srcPath: s, info: info}:
				case <-ctx.Done():
					st.wg.Done()
				}
			}(target, childSrc, e)

		case pxar.FileTypeFifo, pxar.FileTypeSocket:
			if err := restoreSpecial(ctx, st, restoreJob{dest: target, srcPath: childSrc, info: e}); err != nil {
				return err
			}

		}
	}

	if st.noAttr {
		return nil
	}

	deferDirMeta(st, dst, dirEntry)
	return nil
}

func deferDirMeta(st *restoreState, dest string, info pxar.FileInfo) {
	st.pendingMu.Lock()
	st.pendingDirMeta = append(st.pendingDirMeta, dirMeta{dest: dest, info: info})
	st.pendingMu.Unlock()
}

func applyDeferredDirMeta(ctx context.Context, st *restoreState) error {
	st.pendingMu.Lock()
	dirs := st.pendingDirMeta
	st.pendingDirMeta = nil
	st.pendingMu.Unlock()

	var errs []error
	for _, d := range dirs {
		if err := restoreMetadata(ctx, st, d.dest, d.info, true); err != nil {
			errs = append(errs, fmt.Errorf("dir meta %q: %w", d.dest, err))
		}
	}
	return errors.Join(errs...)
}

func restoreMetadata(ctx context.Context, st *restoreState, p string, e pxar.FileInfo, directory bool) error {
	target, closeTarget, err := st.destinationFS(p)
	if err != nil {
		return err
	}
	defer closeTarget()
	flags := os.O_RDWR | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if directory {
		flags = os.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK
	}
	file, err := target.open(p, flags, 0)
	if err != nil {
		return err
	}
	return applyMeta(ctx, st, file, e)
}
func restoreSymlinkChown(st *restoreState, p string, uid, gid int) error {
	target, closeTarget, err := st.destinationFS(p)
	if err != nil {
		return err
	}
	defer closeTarget()
	rel, err := target.relative(p)
	if err != nil {
		return err
	}
	return target.root.Lchown(rel, uid, gid)
}
