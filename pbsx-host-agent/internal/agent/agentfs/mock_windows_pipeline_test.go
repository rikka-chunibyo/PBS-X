package agentfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	pxarlib "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/accessor"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-plus/pxar/format"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs/fswire"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
)

type mockWindowsNode struct {
	stat       format.Stat
	data       []byte
	linkTarget string
	xattrs     []format.XAttr
	identity   backupproxy.FileIdentity
}

type mockWindowsAccessor struct {
	nodes  map[string]mockWindowsNode
	closed bool
}

func newMockWindowsAccessor(nodes map[string]mockWindowsNode) *mockWindowsAccessor {
	normalized := make(map[string]mockWindowsNode, len(nodes))
	for name, node := range nodes {
		normalized[mockWindowsPath(name)] = node
	}
	return &mockWindowsAccessor{nodes: normalized}
}

func mockWindowsPath(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = strings.TrimPrefix(name, "./")
	name = strings.Trim(name, "/")
	if name == "" || name == "." {
		return "."
	}
	return path.Clean(name)
}

func (a *mockWindowsAccessor) node(name string) (mockWindowsNode, error) {
	node, ok := a.nodes[mockWindowsPath(name)]
	if !ok {
		return mockWindowsNode{}, os.ErrNotExist
	}
	return node, nil
}

func (a *mockWindowsAccessor) Close() { a.closed = true }

func (a *mockWindowsAccessor) Stat(name string) (format.Stat, error) {
	node, err := a.node(name)
	return node.stat, err
}

func (a *mockWindowsAccessor) ReadDir(name string) ([]backupproxy.DirEntry, error) {
	parent := mockWindowsPath(name)
	node, err := a.node(parent)
	if err != nil {
		return nil, err
	}
	if !node.stat.IsDir() {
		return nil, errors.New("not a directory")
	}
	prefix := ""
	if parent != "." {
		prefix = parent + "/"
	}
	entries := make([]backupproxy.DirEntry, 0)
	for childPath, child := range a.nodes {
		if childPath == "." || !strings.HasPrefix(childPath, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(childPath, prefix)
		if remainder == "" || strings.Contains(remainder, "/") {
			continue
		}
		entries = append(entries, backupproxy.DirEntry{
			Name: remainder, Stat: child.stat, Size: uint64(len(child.data)),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func (a *mockWindowsAccessor) OpenFile(name string) (io.ReadCloser, uint64, error) {
	node, err := a.node(name)
	if err != nil {
		return nil, 0, err
	}
	if !node.stat.IsRegularFile() {
		return nil, 0, errors.New("not a regular file")
	}
	data := append([]byte(nil), node.data...)
	return io.NopCloser(bytes.NewReader(data)), uint64(len(data)), nil
}

func (a *mockWindowsAccessor) ReadLink(name string) (string, error) {
	node, err := a.node(name)
	if err != nil {
		return "", err
	}
	if !node.stat.IsSymlink() {
		return "", errors.New("not a symlink")
	}
	return node.linkTarget, nil
}

func (a *mockWindowsAccessor) GetXAttrs(name string) ([]format.XAttr, error) {
	node, err := a.node(name)
	if err != nil {
		return nil, err
	}
	return append([]format.XAttr(nil), node.xattrs...), nil
}

func (*mockWindowsAccessor) GetACL(string) (pxarlib.ACL, error) { return pxarlib.ACL{}, nil }
func (*mockWindowsAccessor) GetFCaps(string) ([]byte, error)    { return nil, nil }

func (a *mockWindowsAccessor) FileIdentity(name string) (backupproxy.FileIdentity, error) {
	node, err := a.node(name)
	return node.identity, err
}

// TestMockWindowsPipelineOnLinux exercises the production virtual-source,
// metadata-packing, split-PXAR, chunk-store, and restore pipeline without
// requiring a Windows runner. Only the NTFS/VSS syscalls themselves are
// replaced by this deterministic in-memory filesystem.
func TestMockWindowsPipelineOnLinux(t *testing.T) {
	t.Parallel()

	daclProtected := true
	metadata := fswire.AgentFileInfo{
		Owner: "S-1-5-21-1000", Group: "S-1-5-32-545",
		CreationTime: 1710000001, LastAccessTime: 1710000002, LastWriteTime: 1710000003,
		FileAttributes: map[string]bool{"archive": true, "hidden": true},
		WinACLs:        []fswire.WinACL{{SID: "S-1-5-21-1000", AccessMask: 0x1f01ff, Type: 0, Flags: 3}},
		DACLProtected:  &daclProtected,
	}
	resumeAttrs := nativeMetadataXAttrs(metadata)
	adsAttrs, err := encodeNativeADS([]nativeAlternateDataStream{
		{Name: "Zone.Identifier", Data: []byte("[ZoneTransfer]\r\nZoneId=3\r\n")},
	})
	if err != nil {
		t.Fatalf("encode mock ADS: %v", err)
	}
	sparseAttrs, err := encodeSparseXAttrs(1<<20, []sparseRange{{Offset: 4096, Length: 512 << 10}})
	if err != nil {
		t.Fatalf("encode mock sparse map: %v", err)
	}
	for _, attrs := range [][]format.XAttr{adsAttrs, sparseAttrs} {
		for _, attr := range attrs {
			resumeAttrs = upsertNativeXAttr(resumeAttrs, string(attr.Name()), attr.Value())
		}
	}

	dirStat := format.Stat{Mode: format.ModeIFDIR | 0o755}
	fileStat := format.Stat{Mode: format.ModeIFREG | 0o640, Mtime: format.NewStatxTimestampFromTime(time.Unix(1710000003, 0))}
	linkStat := format.Stat{Mode: format.ModeIFLNK | 0o777}
	cSource := newMockWindowsAccessor(map[string]mockWindowsNode{
		".":                  {stat: dirStat},
		"Resume.txt":         {stat: fileStat, data: []byte("Windows profile data\n"), xattrs: resumeAttrs, identity: backupproxy.FileIdentity{Key: "c:42", Links: 1}},
		"Resume-current.txt": {stat: linkStat, linkTarget: "Resume.txt", identity: backupproxy.FileIdentity{Key: "c:43", Links: 1}},
		"Cache":              {stat: dirStat},
		"Cache/browser.tmp":  {stat: fileStat, data: []byte("must not be archived")},
	})
	dSource := newMockWindowsAccessor(map[string]mockWindowsNode{
		".":           {stat: dirStat},
		"project.txt": {stat: fileStat, data: []byte("D drive project\n"), identity: backupproxy.FileIdentity{Key: "d:42", Links: 1}},
	})

	bySnapshotPath := map[string]*mockWindowsAccessor{"mock-c": cSource, "mock-d": dSource}
	vfs, err := newVirtualAccessorWithFactory("mock-windows", "standard", []snapshots.Snapshot{
		{Path: "mock-c", SourcePath: `C:\Users\Nyx\Documents`, Excludes: []string{"cache"}},
		{Path: "mock-d", SourcePath: `D:\Projects`},
	}, func(_, _ string, snapshot snapshots.Snapshot) virtualSourceAccessor {
		return bySnapshotPath[snapshot.Path]
	})
	if err != nil {
		t.Fatalf("create virtual Windows filesystem: %v", err)
	}
	defer vfs.Close()

	storeDir := t.TempDir()
	chunkConfig, err := buzhash.NewConfig(4 << 10)
	if err != nil {
		t.Fatalf("create chunk config: %v", err)
	}
	store, err := backupproxy.NewLocalStore(storeDir, chunkConfig, true)
	if err != nil {
		t.Fatalf("create local store: %v", err)
	}
	result, err := backupproxy.NewServer(backupproxy.NewLocalClient(vfs), store).RunBackupWithMode(
		context.Background(), ".", backupproxy.BackupConfig{
			BackupType: datastore.BackupHost, BackupID: "windows-mock", BackupTime: 1710000000,
			ChunkConfig: chunkConfig, DetectionMode: backupproxy.DetectionData,
			Compress: true, StrictMetadata: true,
		},
	)
	if err != nil {
		t.Fatalf("run mock Windows split backup: %v", err)
	}
	if result.FileCount != 2 {
		t.Fatalf("backed up %d files, want 2 (excluded cache file must not be walked)", result.FileCount)
	}

	archive := restoreMockWindowsSplitArchive(t, storeDir)
	assertMockWindowsFile(t, archive, "C-Users-Nyx-Documents/Resume.txt", "Windows profile data\n")
	assertMockWindowsFile(t, archive, "D-Projects/project.txt", "D drive project\n")
	link, err := archive.Lookup("C-Users-Nyx-Documents/Resume-current.txt")
	if err != nil || !link.IsSymlink() || link.LinkTarget != "Resume.txt" {
		t.Fatalf("restored Windows symlink = %#v, err=%v", link, err)
	}
	if _, err := archive.Lookup("C-Users-Nyx-Documents/Cache/browser.tmp"); err == nil {
		t.Fatal("case-insensitive Windows exclusion was archived")
	}

	resume, err := archive.Lookup("C-Users-Nyx-Documents/Resume.txt")
	if err != nil {
		t.Fatalf("lookup restored metadata: %v", err)
	}
	resume, err = archive.ReadEntryAt(int64(resume.FileOffset))
	if err != nil {
		t.Fatalf("read restored metadata entry: %v", err)
	}
	xattrs := pxarlib.EntryXAttrs(resume)
	for _, name := range []string{
		nativeXAttrOwner, nativeXAttrGroup, nativeXAttrACLs, nativeXAttrDACLProtected, nativeXAttrFileAttributes,
		nativeXAttrCreationTime, nativeXAttrLastAccessTime, nativeXAttrLastWriteTime,
		nativeXAttrADSManifest, nativeXAttrSparseManifest,
	} {
		if len(xattrs[name]) == 0 {
			t.Errorf("restored archive is missing %q", name)
		}
	}
	if got := string(xattrs[nativeXAttrOwner]); got != metadata.Owner {
		t.Errorf("owner = %q, want %q", got, metadata.Owner)
	}
	if got := string(xattrs[nativeXAttrDACLProtected]); got != "1" {
		t.Errorf("DACL protection = %q, want 1", got)
	}
	var restoredACLs []fswire.WinACL
	if err := cbor.Unmarshal(xattrs[nativeXAttrACLs], &restoredACLs); err != nil {
		t.Fatalf("decode restored Windows ACLs: %v", err)
	}
	if len(restoredACLs) != 1 || restoredACLs[0] != metadata.WinACLs[0] {
		t.Errorf("Windows ACLs = %#v, want %#v", restoredACLs, metadata.WinACLs)
	}
	if !hasMockWindowsXAttrPrefix(xattrs, nativeXAttrADSPrefix) {
		t.Error("restored archive is missing ADS data chunks")
	}
	if !hasMockWindowsXAttrPrefix(xattrs, nativeXAttrSparsePrefix) {
		t.Error("restored archive is missing sparse-map chunks")
	}
}

func hasMockWindowsXAttrPrefix(attrs map[string][]byte, prefix string) bool {
	for name := range attrs {
		if strings.HasPrefix(name, prefix) && name != prefix+"manifest" {
			return true
		}
	}
	return false
}

func restoreMockWindowsSplitArchive(t *testing.T, dir string) *accessor.Accessor {
	t.Helper()
	readStream := func(indexName string) []byte {
		raw, err := os.ReadFile(path.Join(dir, indexName))
		if err != nil {
			t.Fatalf("read %s: %v", indexName, err)
		}
		index, err := datastore.ParseDynamicIndex(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", indexName, err)
		}
		chunkStore, err := datastore.NewChunkStore(dir)
		if err != nil {
			t.Fatalf("open chunk store: %v", err)
		}
		var stream bytes.Buffer
		for i := 0; i < index.Count(); i++ {
			info, ok := index.ChunkInfo(i)
			if !ok {
				t.Fatalf("missing chunk %d in %s", i, indexName)
			}
			blob, err := chunkStore.LoadChunk(info.Digest)
			if err != nil {
				t.Fatalf("load chunk %d from %s: %v", i, indexName, err)
			}
			decoded, err := datastore.DecodeBlob(nil, blob)
			if err != nil {
				t.Fatalf("decode chunk %d from %s: %v", i, indexName, err)
			}
			stream.Write(decoded)
		}
		return stream.Bytes()
	}
	metadata := readStream("root.mpxar.didx")
	payload := readStream("root.ppxar.didx")
	return accessor.NewAccessor(bytes.NewReader(metadata), bytes.NewReader(payload))
}

func assertMockWindowsFile(t *testing.T, archive *accessor.Accessor, name, want string) {
	t.Helper()
	entry, err := archive.Lookup(name)
	if err != nil {
		t.Fatalf("lookup %q: %v", name, err)
	}
	reader, err := archive.ReadFileContentReader(entry)
	if err != nil {
		t.Fatalf("open %q: %v", name, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read %q: %v", name, err)
	}
	if string(data) != want {
		t.Errorf("%q content = %q, want %q", name, data, want)
	}
}
