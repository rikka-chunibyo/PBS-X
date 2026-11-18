//go:build windows

package pxar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/fxamacker/cbor/v2"
	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs/fswire"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
	"golang.org/x/sys/windows"
)

var (
	modAdvapi32          = syscall.NewLazyDLL("advapi32.dll")
	modKernel32          = syscall.NewLazyDLL("kernel32.dll")
	procSetEntriesInAclW = modAdvapi32.NewProc("SetEntriesInAclW")
	procSetSecurityInfo  = modAdvapi32.NewProc("SetSecurityInfo")
	procLocalFree        = modKernel32.NewProc("LocalFree")
)

const restoreSecurityAccess = windows.READ_CONTROL | windows.WRITE_DAC | windows.WRITE_OWNER

type FILE_BASIC_INFO struct {
	_              [0]uint64 // FILETIME fields alone only enforce 4-byte Go alignment.
	CreationTime   windows.Filetime
	LastAccessTime windows.Filetime
	LastWriteTime  windows.Filetime
	ChangeTime     windows.Filetime
	FileAttributes uint32
	_              uint32 // Native FILE_BASIC_INFO has 8-byte alignment and size 40.
}

func applyMeta(ctx context.Context, st *restoreState, file *os.File, e pxar.FileInfo) error {
	defer file.Close()

	h := windows.Handle(file.Fd())
	path := file.Name()

	log.Debug("applyMeta", "path", path, "mtime_secs", e.MtimeSecs, "entry_start", e.EntryRangeStart, "entry_end", e.EntryRangeEnd)

	xattrs, lerr := st.client.ListXAttrs(ctx, e.EntryRangeStart, e.EntryRangeEnd)
	if lerr != nil {
		st.reportErr(ctx, "list xattrs", path, lerr)
	}

	baseFt := unixToFiletime(e.MtimeSecs)
	var c windows.Filetime
	a := baseFt
	w := baseFt
	var hasCreation bool

	if xattrs != nil {
		if d, ok := xattrs[XAttrCreationTime]; ok {
			if ts, ok := ParseXattrUnixSecs(d); ok {
				c = unixToFiletime(ts)
				hasCreation = true
			}
		}
		if d, ok := xattrs[XAttrLastWriteTime]; ok {
			if ts, ok := ParseXattrUnixSecs(d); ok {
				w = unixToFiletime(ts)
			}
		}
		if d, ok := xattrs[XAttrLastAccessTime]; ok {
			if ts, ok := ParseXattrUnixSecs(d); ok {
				a = unixToFiletime(ts)
			}
		}
	}

	var attrs uint32
	var hasAttrs bool
	if xattrs != nil && st.fsCap.supportsXAttrs {
		if d, ok := xattrs[XAttrFileAttributes]; ok {
			var fa map[string]bool
			if cbor.Unmarshal(d, &fa) == nil {
				if attr := buildFileAttributes(fa); attr != 0 {
					attrs = attr
					hasAttrs = true
				}
			}
		}
	}

	if xattrs != nil {
		// WRITE_DAC/WRITE_OWNER, which normally causes SetSecurityInfo to
		if st.fsCap.supportsPersistentACLs {
			restoreWindowsACLsFromPath(ctx, st, path, xattrs)
		}
		// Restore non-canonical xattrs as NTFS alternate data streams
		// (canonical keys consumed above are skipped).
		if st.fsCap.supportsXAttrs {
			writeAlternateDataStreams(ctx, st, path, xattrs)
		}
	}
	// Restore streams before READONLY and timestamps: creating an ADS needs
	// write access and can update the file's last-write time.
	if hasAttrs {
		info := FILE_BASIC_INFO{CreationTime: c, LastAccessTime: a, LastWriteTime: w, FileAttributes: attrs}
		st.reportErr(ctx, "set basic info", path, setBasicInfo(h, &info))
	} else {
		var cp *windows.Filetime
		if hasCreation {
			cp = &c
		}
		st.reportErr(ctx, "set file time", path, windows.SetFileTime(h, cp, &a, &w))
	}

	return nil
}

func setBasicInfo(h windows.Handle, info *FILE_BASIC_INFO) error {
	return windows.SetFileInformationByHandle(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(info)), uint32(unsafe.Sizeof(*info)))
}

// restoreWindowsACLsFromPath opens the file with WRITE_DAC|WRITE_OWNER and
// FILE_FLAG_BACKUP_SEMANTICS, then delegates to restoreWindowsACLsFromHandle.
// SIDs are intentionally leaked — see restoreWindowsACLsFromHandle.
func restoreWindowsACLsFromPath(ctx context.Context, st *restoreState, path string, xattrs map[string][]byte) {
	file, err := openWindowsRestoreFile(st, path, "", restoreSecurityAccess, false, false)
	if err != nil {
		st.reportErr(ctx, "open for acls", path, err)
		return
	}
	defer file.Close()
	h := windows.Handle(file.Fd())

	restoreWindowsACLsFromHandle(ctx, st, h, path, xattrs)
}

// restoreWindowsACLsFromHandle applies owner, group, and DACL via SetSecurityInfo.
// SIDs allocated by StringToSid are intentionally NOT freed: on Windows build
// 10.0.26200 LocalFree on a ConvertStringSidToSidW SID causes heap corruption
// (0xC0000374). The leak is bounded (freed at process exit).
func restoreWindowsACLsFromHandle(ctx context.Context, st *restoreState, h windows.Handle, path string, xattrs map[string][]byte) {
	var ownerSID, groupSID *windows.SID

	if d, ok := xattrs["user.owner"]; ok {
		if sid, err := windows.StringToSid(string(d)); err == nil {
			ownerSID = sid
		}
	}
	if d, ok := xattrs["user.group"]; ok {
		if sid, err := windows.StringToSid(string(d)); err == nil {
			groupSID = sid
		}
	}

	var dacl *windows.ACL
	if d, ok := xattrs["user.acls"]; ok {
		if detectACLFlavor(d) == aclWindows {
			var winACLs []fswire.WinACL
			if uerr := cbor.Unmarshal(d, &winACLs); uerr != nil {
				st.reportErr(ctx, "decode acls", path, uerr)
			} else {
				acl, _, berr := buildDACLFromACEs(winACLs)
				if berr != nil {
					st.reportErr(ctx, "build dacl", path, berr)
				} else if acl != nil {
					dacl = acl
				}
			}
		}
	}

	if h == 0 || h == windows.InvalidHandle {
		return
	}

	if ownerSID != nil {
		st.reportErr(ctx, "set owner", path, setSecurityInfoPart(h, windows.OWNER_SECURITY_INFORMATION, ownerSID, nil, nil))
	}
	if groupSID != nil {
		st.reportErr(ctx, "set group", path, setSecurityInfoPart(h, windows.GROUP_SECURITY_INFORMATION, nil, groupSID, nil))
	}
	if dacl != nil {
		info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
		switch string(xattrs[XAttrDACLProtected]) {
		case "1":
			info |= windows.PROTECTED_DACL_SECURITY_INFORMATION
		case "0":
			info |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
		}
		st.reportErr(ctx, "set dacl", path, setSecurityInfoPart(h, info, nil, nil, dacl))
	}

	localFreePtr(aclPtr(dacl))
}

func setSecurityInfoPart(h windows.Handle, info windows.SECURITY_INFORMATION, owner, group *windows.SID, dacl *windows.ACL) error {
	ret, _, _ := procSetSecurityInfo.Call(
		uintptr(h), uintptr(windows.SE_FILE_OBJECT), uintptr(info),
		sidPtr(owner), sidPtr(group), aclPtr(dacl), 0,
	)
	if ret != 0 {
		return syscall.Errno(ret)
	}
	return nil
}

func applyMetaSymlink(ctx context.Context, st *restoreState, linkPath string, e pxar.FileInfo) error {
	file, err := openWindowsRestoreFile(st, linkPath, "", windows.FILE_WRITE_ATTRIBUTES|restoreSecurityAccess, false, true)
	if err != nil {
		st.reportErr(ctx, "open symlink for metadata", linkPath, err)
		return nil
	}
	defer file.Close()
	h := windows.Handle(file.Fd())

	xattrs, lerr := st.client.ListXAttrs(ctx, e.EntryRangeStart, e.EntryRangeEnd)
	if lerr != nil {
		st.reportErr(ctx, "list xattrs", linkPath, lerr)
	}
	if xattrs == nil {
		return nil
	}

	// File attributes are NOT set on a reparse point — writing them could
	// clear FILE_ATTRIBUTE_REPARSE_POINT and break the symlink.
	baseFt := unixToFiletime(e.MtimeSecs)
	a := baseFt
	w := baseFt
	var cp *windows.Filetime
	if d, ok := xattrs[XAttrCreationTime]; ok {
		if ts, ok := ParseXattrUnixSecs(d); ok {
			ft := unixToFiletime(ts)
			cp = &ft
		}
	}
	if d, ok := xattrs[XAttrLastWriteTime]; ok {
		if ts, ok := ParseXattrUnixSecs(d); ok {
			w = unixToFiletime(ts)
		}
	}
	if d, ok := xattrs[XAttrLastAccessTime]; ok {
		if ts, ok := ParseXattrUnixSecs(d); ok {
			a = unixToFiletime(ts)
		}
	}
	st.reportErr(ctx, "set file time", linkPath, windows.SetFileTime(h, cp, &a, &w))

	if st.fsCap.supportsPersistentACLs {
		restoreWindowsACLsFromHandle(ctx, st, h, linkPath, xattrs)
	}
	return nil
}

func applyTempMode(_ *restoreState, path string, rawMode uint64) error { return nil }

func openReparsePoint(p string, access uint32) (windows.Handle, error) {
	ptr, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(
		ptr, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0,
	)
}

type adsManifestEntry struct {
	Name   string `cbor:"name"`
	Size   int64  `cbor:"size"`
	Digest []byte `cbor:"digest"`
	Chunks int    `cbor:"chunks"`
}

// writeAlternateDataStreams writes non-canonical user.* xattrs as NTFS
func writeAlternateDataStreams(ctx context.Context, st *restoreState, name string, xattrs map[string][]byte) {
	consumed := make(map[string]struct{})
	if raw, ok := xattrs[XAttrADSManifest]; ok {
		var manifest []adsManifestEntry
		if err := cbor.Unmarshal(raw, &manifest); err != nil {
			st.reportErr(ctx, "decode ADS manifest", name, err)
		} else {
			for streamIndex, entry := range manifest {
				if entry.Name == "" || strings.ContainsAny(entry.Name, `<>:"/\|?*`) {
					continue
				}
				digestTag := ""
				if len(entry.Digest) >= 8 {
					digestTag = hex.EncodeToString(entry.Digest[:8])
				}
				var data []byte
				missing := false
				for chunkIndex := 0; chunkIndex < entry.Chunks; chunkIndex++ {
					key := fmt.Sprintf("%s%04x.%s.%06x", XAttrADSPrefix, streamIndex, digestTag, chunkIndex)
					chunk, ok := xattrs[key]
					if !ok {
						st.reportErr(ctx, "restore ADS", name, fmt.Errorf("missing metadata chunk %q", key))
						missing = true
						break
					}
					consumed[key] = struct{}{}
					data = append(data, chunk...)
				}
				if missing {
					continue
				}
				if int64(len(data)) != entry.Size {
					st.reportErr(ctx, "restore ADS", name, fmt.Errorf("stream %q size mismatch: got %d, want %d", entry.Name, len(data), entry.Size))
					continue
				}
				if len(entry.Digest) == sha256.Size {
					digest := sha256.Sum256(data)
					if !strings.EqualFold(hex.EncodeToString(digest[:]), hex.EncodeToString(entry.Digest)) {
						st.reportErr(ctx, "restore ADS", name, fmt.Errorf("stream %q digest mismatch", entry.Name))
						continue
					}
				}
				st.reportErr(ctx, "write ads "+entry.Name, name, writeRestoreADS(st, name, entry.Name, data))
			}
		}
	}

	// Backward compatibility with PBS Plus/PBS-X snapshots which represented
	// small ADS directly as non-canonical user.<stream> xattrs.
	for k, v := range xattrs {
		if !strings.HasPrefix(k, "user.") || strings.HasPrefix(k, XAttrADSPrefix) {
			continue
		}
		if IsCanonicalXAttr(k) {
			continue
		}
		if _, ok := consumed[k]; ok {
			continue
		}
		stream := strings.TrimPrefix(k, "user.")
		if stream == "" || strings.ContainsAny(stream, `<>:"/\|?*`) {
			continue
		}
		st.reportErr(ctx, "write ads "+k, name, writeRestoreADS(st, name, stream, v))
	}
}

func unixToFiletime(unixTime int64) windows.Filetime {
	const ticksPerSecond = 10000000
	const epochDifference = 116444736000000000

	ticks := unixTime*ticksPerSecond + epochDifference
	return windows.Filetime{
		LowDateTime:  uint32(ticks & 0xFFFFFFFF),
		HighDateTime: uint32(ticks >> 32),
	}
}

func buildFileAttributes(fa map[string]bool) uint32 {
	var r uint32
	m := map[string]uint32{
		"readOnly":                windows.FILE_ATTRIBUTE_READONLY,
		"hidden":                  windows.FILE_ATTRIBUTE_HIDDEN,
		"system":                  windows.FILE_ATTRIBUTE_SYSTEM,
		"archive":                 windows.FILE_ATTRIBUTE_ARCHIVE,
		"temporary":               windows.FILE_ATTRIBUTE_TEMPORARY,
		"notContentIndexed":       windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED,
		"FILE_ATTRIBUTE_READONLY": windows.FILE_ATTRIBUTE_READONLY,
		"FILE_ATTRIBUTE_HIDDEN":   windows.FILE_ATTRIBUTE_HIDDEN,
		"FILE_ATTRIBUTE_SYSTEM":   windows.FILE_ATTRIBUTE_SYSTEM,
		"FILE_ATTRIBUTE_ARCHIVE":  windows.FILE_ATTRIBUTE_ARCHIVE,
	}
	for k, v := range m {
		if fa[k] {
			r |= v
		}
	}
	return r
}

// buildDACLFromACEs builds a DACL from WinACL entries, returning allocated SIDs
// so the caller can free them after SetSecurityInfo consumes the ACL.
func buildDACLFromACEs(winACLs []fswire.WinACL) (acl *windows.ACL, sids []*windows.SID, err error) {
	if len(winACLs) == 0 {
		return nil, nil, nil
	}

	entries := make([]windows.EXPLICIT_ACCESS, 0, len(winACLs))

	for _, acl := range winACLs {
		sid, serr := windows.StringToSid(acl.SID)
		if serr != nil {
			continue
		}
		sids = append(sids, sid)
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.ACCESS_MASK(acl.AccessMask),
			AccessMode:        windows.ACCESS_MODE(acl.Type),
			Inheritance:       uint32(acl.Flags),
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_UNKNOWN,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}

	if len(entries) == 0 {
		return nil, sids, nil
	}

	var newACL *windows.ACL
	ret, _, _ := procSetEntriesInAclW.Call(
		uintptr(len(entries)),
		uintptr(unsafe.Pointer(&entries[0])),
		0,
		uintptr(unsafe.Pointer(&newACL)),
	)
	if ret != 0 {
		return nil, sids, fmt.Errorf("SetEntriesInAcl failed: %w", syscall.Errno(ret))
	}
	return newACL, sids, nil
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

		st.wg.Add(1)
		go func(t, s string, info pxar.FileInfo) {
			select {
			case st.jobs <- restoreJob{dest: t, srcPath: s, info: info}:
			case <-ctx.Done():
				st.wg.Done()
			}
		}(target, childSrc, e)
	}

	if st.noAttr {
		return nil
	}

	deferDirMeta(st, dst, dirEntry)
	return nil
}

func sidPtr(s *windows.SID) uintptr {
	if s == nil {
		return 0
	}
	return uintptr(unsafe.Pointer(s))
}

func aclPtr(a *windows.ACL) uintptr {
	if a == nil {
		return 0
	}
	return uintptr(unsafe.Pointer(a))
}

func localFreePtr(p uintptr) {
	if p == 0 {
		return
	}
	_, _, _ = procLocalFree.Call(p)
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
	access := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
	if directory {
		access = windows.FILE_READ_ATTRIBUTES | windows.FILE_WRITE_ATTRIBUTES
	}
	file, err := openWindowsRestoreFile(st, p, "", access, false, false)
	if err != nil {
		return err
	}
	return applyMeta(ctx, st, file, e)
}
