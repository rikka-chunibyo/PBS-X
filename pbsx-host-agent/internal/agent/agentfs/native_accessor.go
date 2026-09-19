package agentfs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	pxarlib "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/format"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs/fswire"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
)

// Canonical PBS-X metadata xattrs. These match the existing PBS Plus restore
// implementation so backups made by PBS-X retain Windows-only metadata and can
// also be restored by the agent on another platform.
const (
	nativeXAttrOwner          = "user.owner"
	nativeXAttrGroup          = "user.group"
	nativeXAttrACLs           = "user.acls"
	nativeXAttrFileAttributes = "user.fileattributes"
	nativeXAttrCreationTime   = "user.creationtime"
	nativeXAttrLastAccessTime = "user.lastaccesstime"
	nativeXAttrLastWriteTime  = "user.lastwritetime"
)

// NativeAccessor exposes a local point-in-time snapshot to the Go PXAR
// encoder. It deliberately reuses agentfs' platform-specific privileged file
// access and metadata readers rather than maintaining a second filesystem
// implementation for native PBS uploads.
type NativeAccessor struct {
	fs *Server
}

// NewNativeAccessor returns a filesystem accessor rooted at snapshot.Path.
// readMode is currently retained for compatibility with the existing agentfs
// implementation; native uploads use standard streaming reads by default.
func NewNativeAccessor(jobID, readMode string, snapshot snapshots.Snapshot) *NativeAccessor {
	if readMode == "" {
		readMode = "standard"
	}
	return &NativeAccessor{fs: NewServer(jobID, readMode, snapshot)}
}

func (a *NativeAccessor) Close() { a.fs.Close() }

func (a *NativeAccessor) Stat(path string) (format.Stat, error) {
	abs := a.fs.abs(cleanNativePath(path))
	info, err := a.fs.platformStat(abs)
	if err != nil {
		return format.Stat{}, err
	}
	xinfo, _ := a.fs.platformXstat(abs, false)
	return nativeFormatStat(abs, info, xinfo), nil
}

func (a *NativeAccessor) ReadDir(path string) ([]backupproxy.DirEntry, error) {
	abs := a.fs.abs(cleanNativePath(path))
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}

	result := make([]backupproxy.DirEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		childAbs := filepath.Join(abs, name)
		info, err := a.fs.platformStat(childAbs)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", childAbs, err)
		}
		xinfo, _ := a.fs.platformXstat(childAbs, false)
		result = append(result, backupproxy.DirEntry{
			Name: name,
			Stat: nativeFormatStat(childAbs, info, xinfo),
			Size: uint64(max(info.Size, 0)),
		})
	}
	return result, nil
}

func (a *NativeAccessor) OpenFile(path string) (io.ReadCloser, uint64, error) {
	abs := a.fs.abs(cleanNativePath(path))
	fh, err := a.fs.platformOpen(abs)
	if err != nil {
		return nil, 0, err
	}
	if fh.isDir {
		_ = fh.file.Close()
		return nil, 0, fmt.Errorf("%q is a directory", path)
	}
	if fh.fileSize < 0 {
		_ = fh.file.Close()
		return nil, 0, fmt.Errorf("%q has negative file size", path)
	}
	return fh.file, uint64(fh.fileSize), nil
}

func (a *NativeAccessor) ReadLink(path string) (string, error) {
	abs := a.fs.abs(cleanNativePath(path))
	return os.Readlink(abs)
}

func (a *NativeAccessor) GetXAttrs(path string) ([]format.XAttr, error) {
	abs := a.fs.abs(cleanNativePath(path))

	attrs, err := nativePlatformXAttrs(abs)
	if err != nil {
		return nil, err
	}

	// agentfs has the richer Windows metadata and POSIX ACL representation
	// used by PBS-X's cross-platform restore path. Encode fields which PXAR
	// does not otherwise preserve as ordinary PXAR xattrs.
	xinfo, err := a.fs.platformXstat(abs, false)
	if err != nil {
		return attrs, nil // extended metadata is best-effort, matching PBS client behavior
	}

	upsert := func(name string, value []byte) {
		if len(value) == 0 {
			return
		}
		for i := range attrs {
			if attrs[i].Name() == name {
				attrs[i] = format.NewXAttr([]byte(name), value)
				return
			}
		}
		attrs = append(attrs, format.NewXAttr([]byte(name), value))
	}

	if xinfo.Owner != "" {
		upsert(nativeXAttrOwner, []byte(xinfo.Owner))
	}
	if xinfo.Group != "" {
		upsert(nativeXAttrGroup, []byte(xinfo.Group))
	}
	if xinfo.CreationTime > 0 {
		upsert(nativeXAttrCreationTime, []byte(strconv.FormatInt(xinfo.CreationTime, 10)))
	}
	if xinfo.LastAccessTime > 0 {
		upsert(nativeXAttrLastAccessTime, []byte(strconv.FormatInt(xinfo.LastAccessTime, 10)))
	}
	if xinfo.LastWriteTime > 0 {
		upsert(nativeXAttrLastWriteTime, []byte(strconv.FormatInt(xinfo.LastWriteTime, 10)))
	}
	if len(xinfo.FileAttributes) != 0 {
		if data, err := cbor.Marshal(xinfo.FileAttributes); err == nil {
			upsert(nativeXAttrFileAttributes, data)
		}
	}
	switch {
	case xinfo.WinACLs != nil:
		if data, err := cbor.Marshal(xinfo.WinACLs); err == nil {
			upsert(nativeXAttrACLs, data)
		}
	case xinfo.PosixACLs != nil:
		if data, err := cbor.Marshal(xinfo.PosixACLs); err == nil {
			upsert(nativeXAttrACLs, data)
		}
	}

	return attrs, nil
}

func (a *NativeAccessor) GetACL(path string) (pxarlib.ACL, error) {
	abs := a.fs.abs(cleanNativePath(path))
	xinfo, err := a.fs.platformXstat(abs, true)
	if err != nil || len(xinfo.PosixACLs) == 0 {
		return pxarlib.ACL{}, err
	}
	return posixACLToPXAR(xinfo.PosixACLs), nil
}

func (a *NativeAccessor) GetFCaps(path string) ([]byte, error) {
	return nativePlatformFCaps(a.fs.abs(cleanNativePath(path)))
}

func cleanNativePath(path string) string {
	path = strings.TrimSpace(path)
	switch path {
	case "", ".", "/", `\\`:
		return "."
	}
	return path
}

func nativeFormatStat(abs string, info, xinfo fswire.AgentFileInfo) format.Stat {
	mode := nativePXARMode(os.FileMode(info.Mode), info.IsDir)
	uid, gid := uint32(0), uint32(0)
	if value, err := strconv.ParseUint(xinfo.Owner, 10, 32); err == nil {
		uid = uint32(value)
	}
	if value, err := strconv.ParseUint(xinfo.Group, 10, 32); err == nil {
		gid = uint32(value)
	}

	var mtime time.Time
	if xinfo.LastWriteTime != 0 {
		mtime = time.Unix(xinfo.LastWriteTime, 0)
	} else if stat, err := os.Lstat(abs); err == nil {
		mtime = stat.ModTime()
	} else if info.ModTime != 0 {
		// Unix platformStat reports nanoseconds, while older Windows code
		// reports seconds. The os.Lstat path above handles the common case;
		// this branch is only a fallback.
		if info.ModTime > 1_000_000_000_000 {
			mtime = time.Unix(0, info.ModTime)
		} else {
			mtime = time.Unix(info.ModTime, 0)
		}
	}

	return format.Stat{
		Mode:  mode,
		UID:   uid,
		GID:   gid,
		Mtime: format.NewStatxTimestampFromTime(mtime),
	}
}

func nativePXARMode(mode os.FileMode, isDir bool) uint64 {
	result := uint64(mode.Perm())
	if mode&os.ModeSetuid != 0 {
		result |= format.ModeISUID
	}
	if mode&os.ModeSetgid != 0 {
		result |= format.ModeISGID
	}
	if mode&os.ModeSticky != 0 {
		result |= format.ModeISVTX
	}

	switch {
	case isDir || mode.IsDir():
		result |= format.ModeIFDIR
	case mode&os.ModeSymlink != 0:
		result |= format.ModeIFLNK
	case mode&os.ModeNamedPipe != 0:
		result |= format.ModeIFIFO
	case mode&os.ModeSocket != 0:
		result |= format.ModeIFSOCK
	case mode&os.ModeDevice != 0 && mode&os.ModeCharDevice != 0:
		result |= format.ModeIFCHR
	case mode&os.ModeDevice != 0:
		result |= format.ModeIFBLK
	default:
		result |= format.ModeIFREG
	}
	return result
}

func posixACLToPXAR(entries []fswire.PosixACL) pxarlib.ACL {
	acl := pxarlib.ACL{}
	var defaults = struct {
		userObj, groupObj, other, mask *format.ACLPermissions
	}{}

	ptr := func(v uint8) *format.ACLPermissions {
		p := format.ACLPermissions(v & 0x7)
		return &p
	}

	for _, entry := range entries {
		perms := format.ACLPermissions(entry.Perms & 0x7)
		if entry.IsDefault {
			switch entry.Tag {
			case "user_obj":
				defaults.userObj = ptr(entry.Perms)
			case "group_obj":
				defaults.groupObj = ptr(entry.Perms)
			case "other":
				defaults.other = ptr(entry.Perms)
			case "mask":
				defaults.mask = ptr(entry.Perms)
			case "user":
				if entry.ID >= 0 {
					acl.DefaultUsers = append(acl.DefaultUsers, format.ACLUser{UID: uint64(entry.ID), Permissions: perms})
				}
			case "group":
				if entry.ID >= 0 {
					acl.DefaultGroups = append(acl.DefaultGroups, format.ACLGroup{GID: uint64(entry.ID), Permissions: perms})
				}
			}
			continue
		}

		switch entry.Tag {
		case "user":
			if entry.ID >= 0 {
				acl.Users = append(acl.Users, format.ACLUser{UID: uint64(entry.ID), Permissions: perms})
			}
		case "group":
			if entry.ID >= 0 {
				acl.Groups = append(acl.Groups, format.ACLGroup{GID: uint64(entry.ID), Permissions: perms})
			}
		case "group_obj":
			p := format.ACLGroupObject{Permissions: perms}
			acl.GroupObj = &p
		}
	}

	if defaults.userObj != nil || defaults.groupObj != nil || defaults.other != nil || defaults.mask != nil {
		value := format.ACLDefault{
			UserObjPermissions:  format.ACLNoMask,
			GroupObjPermissions: format.ACLNoMask,
			OtherPermissions:    format.ACLNoMask,
			MaskPermissions:     format.ACLNoMask,
		}
		if defaults.userObj != nil {
			value.UserObjPermissions = *defaults.userObj
		}
		if defaults.groupObj != nil {
			value.GroupObjPermissions = *defaults.groupObj
		}
		if defaults.other != nil {
			value.OtherPermissions = *defaults.other
		}
		if defaults.mask != nil {
			value.MaskPermissions = *defaults.mask
		}
		acl.Default = &value
	}

	return acl
}

// MarshalNativeMetadataJSON is used only for diagnostics and test fixtures.
// It intentionally avoids exposing platform structs to the control plane.
func MarshalNativeMetadataJSON(info fswire.AgentFileInfo) []byte {
	data, _ := json.Marshal(info)
	return data
}
