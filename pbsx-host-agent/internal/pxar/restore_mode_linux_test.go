//go:build linux

package pxar

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/fxamacker/cbor/v2"
	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs/fswire"
	"golang.org/x/sys/unix"
)

type modeFixture struct {
	*restoreFixture
	rootMode  uint64
	attrs     map[string][]byte
	fileAttrs map[string][]byte
}

func (f *modeFixture) LookupByPath(ctx context.Context, path string) (*pxar.FileInfo, error) {
	info, err := f.restoreFixture.LookupByPath(ctx, path)
	if err == nil {
		info.RawMode = f.rootMode
	}
	return info, err
}
func (f *modeFixture) ListXAttrs(_ context.Context, start, _ uint64) (map[string][]byte, error) {
	attrs := make(map[string][]byte, len(f.attrs))
	for k, v := range f.attrs {
		attrs[k] = append([]byte(nil), v...)
	}
	if start == 100 {
		for k, v := range f.fileAttrs {
			attrs[k] = append([]byte(nil), v...)
		}
	}
	return attrs, nil
}

func TestRestorePrivilegedMetadataOrdering(t *testing.T) {
	if os.Getenv("PBSX_LINUX_PRIVILEGED_TEST") != "1" {
		t.Skip("set PBSX_LINUX_PRIVILEGED_TEST=1 on a disposable root VM")
	}
	if os.Geteuid() != 0 {
		t.Fatal("opted-in metadata test requires root")
	}
	acl := []fswire.PosixACL{{Tag: "user_obj", ID: -1, Perms: 7}, {Tag: "user", ID: 65533, Perms: 4}, {Tag: "group_obj", ID: -1, Perms: 5}, {Tag: "mask", ID: -1, Perms: 5}, {Tag: "other", ID: -1, Perms: 0}}
	encoded, err := cbor.Marshal(acl)
	if err != nil {
		t.Fatal(err)
	}
	caps := make([]byte, 20)
	binary.LittleEndian.PutUint32(caps, 0x02000001)
	binary.LittleEndian.PutUint32(caps[4:], 1<<10) // CAP_NET_BIND_SERVICE, effective
	fixture := &modeFixture{restoreFixture: fileFixture("privileged metadata"), rootMode: 02770,
		attrs:     map[string][]byte{XAttrOwner: []byte("65534"), XAttrGroup: []byte("65534")},
		fileAttrs: map[string][]byte{XAttrACLs: encoded, "security.capability": caps}}
	fixture.entries[0].RawMode = 06750
	fixture.entries[0].EntryRangeStart = 100
	client, errs := NewLocalClient(fixture, "privileged-permissions")
	defer client.Close()
	dest := t.TempDir()
	if err := RestoreWithOptions(t.Context(), client, []string{"/"}, RestoreOptions{Mode: RestoreModeNormal, DestDir: dest}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
	for path, want := range map[string]uint32{filepath.Join(dest, "folder"): 02770, filepath.Join(dest, "folder", "file"): 06750} {
		var stat unix.Stat_t
		if err := unix.Stat(path, &stat); err != nil {
			t.Fatal(err)
		}
		if stat.Mode&07777 != want || stat.Uid != 65534 || stat.Gid != 65534 {
			t.Errorf("metadata lost for %s: mode %04o uid %d gid %d", path, stat.Mode&07777, stat.Uid, stat.Gid)
		}
	}
	path := filepath.Join(dest, "folder", "file")
	for name, want := range map[string][]byte{XATTR_NAME_ACL_ACCESS: packACL(acl), "security.capability": caps} {
		got := make([]byte, 256)
		n, err := unix.Getxattr(path, name, got)
		if err != nil || !bytes.Equal(got[:max(n, 0)], want) {
			t.Errorf("%s lost: %x want %x: %v", name, got[:max(n, 0)], want, err)
		}
	}
}

func TestRestoreSpecialPermissionsAndOwnershipOverrides(t *testing.T) {
	for _, mode := range []uint64{04755, 02755, 06755} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			fixture := &modeFixture{restoreFixture: fileFixture("permissions"), rootMode: 03777,
				attrs: map[string][]byte{XAttrOwner: []byte(strconv.Itoa(os.Getuid())), XAttrGroup: []byte(strconv.Itoa(os.Getgid()))}}
			fixture.entries[0].RawMode = mode
			// Force the legacy ownership-override path without needing root.
			fixture.entries[0].RawUID = uint32(os.Getuid() + 1)
			fixture.entries[0].RawGID = uint32(os.Getgid() + 1)
			client, errs := NewLocalClient(fixture, "permissions")
			defer client.Close()
			dest := t.TempDir()
			if err := RestoreWithOptions(t.Context(), client, []string{"/"}, RestoreOptions{Mode: RestoreModeNormal, DestDir: dest}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-errs:
				t.Fatal(err)
			default:
			}
			for path, want := range map[string]uint64{filepath.Join(dest, "folder"): 03777, filepath.Join(dest, "folder", "file"): mode} {
				var stat unix.Stat_t
				if err := unix.Stat(path, &stat); err != nil {
					t.Fatal(err)
				}
				if got := uint64(stat.Mode & 07777); got != want {
					t.Errorf("%s: mode %04o, want %04o", path, got, want)
				}
				if int(stat.Uid) != os.Getuid() || int(stat.Gid) != os.Getgid() {
					t.Errorf("ownership override lost: %+v", stat)
				}
			}
		})
	}
}

func TestRestoreKeepsCanonicalMtimePrecision(t *testing.T) {
	fixture := &modeFixture{restoreFixture: fileFixture("mtime"), rootMode: 0755, attrs: map[string][]byte{XAttrLastWriteTime: []byte("1")}}
	fixture.entries[0].MtimeNsecs = 123456789
	client, errs := NewLocalClient(fixture, "mtime")
	defer client.Close()
	dest := t.TempDir()
	if err := RestoreWithOptions(t.Context(), client, []string{"/"}, RestoreOptions{Mode: RestoreModeNormal, DestDir: dest}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
	info, err := os.Stat(filepath.Join(dest, "folder", "file"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.ModTime().UnixNano(); got != 1123456789 {
		t.Fatalf("mtime %d, want 1123456789", got)
	}
}
