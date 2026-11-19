package backupproxy

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
)

type checkpointProtocol struct {
	*mockPBSProtocol
	sizes      map[string]uint64
	appends    map[uint64][]datastore.DynamicEntry
	uploads    int
	failAppend bool
}

func newCheckpointProtocol() *checkpointProtocol {
	return &checkpointProtocol{mockPBSProtocol: newMockPBSProtocol(), sizes: map[string]uint64{}, appends: map[uint64][]datastore.DynamicEntry{}}
}
func (p *checkpointProtocol) dynamicChunkUpload(wid uint64, digest string, size, encoded int, data []byte) error {
	p.uploads++
	p.sizes[digest] = uint64(size)
	return p.mockPBSProtocol.dynamicChunkUpload(wid, digest, size, encoded, data)
}
func (p *checkpointProtocol) dynamicIndexAppend(wid uint64, digests []string, offsets []uint64) error {
	if p.failAppend {
		return errors.New("connection lost before index acknowledgment")
	}
	for i, name := range digests {
		entries := p.appends[wid]
		offset := uint64(0)
		if len(entries) > 0 {
			offset = entries[len(entries)-1].EndOffset
		}
		size, ok := p.sizes[name]
		if !ok || offsets[i] != offset {
			return errors.New("unregistered chunk or invalid offset")
		}
		var digest [32]byte
		decoded, err := hex.DecodeString(name)
		if err != nil {
			return err
		}
		copy(digest[:], decoded)
		p.appends[wid] = append(entries, datastore.DynamicEntry{EndOffset: offset + size, Digest: digest})
	}
	return nil
}
func (p *checkpointProtocol) index(t *testing.T, wid uint64) []byte {
	t.Helper()
	index := datastore.NewDynamicIndexWriter(1)
	for _, entry := range p.appends[wid] {
		index.Add(entry.EndOffset, entry.Digest)
	}
	raw, err := index.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCheckpointStreamRestoresAcknowledgedPrefixWithoutUploadingItAgain(t *testing.T) {
	s, _ := newTestPBSSession(t)
	protocol := newCheckpointProtocol()
	s.proto = protocol
	w, err := newCheckpointStream(t.Context(), s, "root.ppxar.didx", 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	prefix := bytes.Repeat([]byte("prefix content "), 1000)
	if _, err = w.Write(prefix); err != nil {
		t.Fatal(err)
	}
	state, err := w.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if state.Size != uint64(len(prefix)) || state.Count != len(protocol.appends[w.wid]) {
		t.Fatal("checkpoint returned before all references were appended")
	}
	raw := protocol.index(t, w.wid)
	uploads := protocol.uploads
	// A fresh authenticated server connection reopens the index with these
	// exact prefix entries and registers their chunks; writer IDs may change.
	newWID, _ := protocol.dynamicIndexCreate("root.ppxar.didx")
	protocol.appends[newWID] = append([]datastore.DynamicEntry(nil), protocol.appends[w.wid]...)
	s2, _ := newTestPBSSession(t)
	s2.proto = protocol
	resumed, err := newCheckpointStream(t.Context(), s2, "root.ppxar.didx", newWID, raw, &state)
	if err != nil {
		t.Fatal(err)
	}
	if protocol.uploads != uploads {
		t.Fatal("uploaded prefix while reopening continuation")
	}
	if _, err = resumed.Write([]byte("suffix")); err != nil {
		t.Fatal(err)
	}
	result, err := resumed.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != uint64(len(prefix)+6) {
		t.Fatal("lost prefix length")
	}
	idx, err := datastore.ParseDynamicIndex(protocol.index(t, newWID))
	if err != nil {
		t.Fatal(err)
	}
	csum, size := idx.ComputeCsum()
	if csum != result.Digest || size != result.Size {
		t.Fatal("resumed checksum does not include prefix")
	}
	if len(s2.files) != 1 {
		t.Fatal("finalized stream was not recorded for manifest")
	}
}

func TestCheckpointStreamNeverAcknowledgesFailedIndexAppend(t *testing.T) {
	s, _ := newTestPBSSession(t)
	protocol := newCheckpointProtocol()
	s.proto = protocol
	w, err := newCheckpointStream(t.Context(), s, "root.mpxar.didx", 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("less than one chunk")); err != nil {
		t.Fatal(err)
	}
	protocol.failAppend = true
	if _, err = w.Checkpoint(); err == nil {
		t.Fatal("acknowledged a failed index append")
	}
	if w.state.Size != 0 || w.state.Count != 0 || len(s.files) != 0 {
		t.Fatal("failed stream advanced committed state")
	}
	if _, err = w.Finish(); err == nil {
		t.Fatal("finalized failed stream")
	}
}

func TestCheckpointStreamRejectsMismatchedOrCorruptServerPrefix(t *testing.T) {
	s, _ := newTestPBSSession(t)
	protocol := newCheckpointProtocol()
	s.proto = protocol
	w, err := newCheckpointStream(t.Context(), s, "root.mpxar.didx", 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("prefix"))
	state, err := w.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	raw := protocol.index(t, w.wid)
	bad := state
	bad.Size++
	if _, err = newCheckpointStream(t.Context(), s, state.Name, w.wid, raw, &bad); err == nil {
		t.Fatal("accepted mismatched saved prefix")
	}
	raw[len(raw)-1] ^= 1
	if _, err = newCheckpointStream(t.Context(), s, state.Name, w.wid, raw, &state); err == nil {
		t.Fatal("accepted corrupt server index")
	}
}
