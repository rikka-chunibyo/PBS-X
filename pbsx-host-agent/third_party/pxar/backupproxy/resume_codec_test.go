package backupproxy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestResumeStateIsAlwaysEncryptedAndBoundToRunAndSequence(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	want := documentResumeEnvelope{Version: 1, Sequence: 7, Binding: strings.Repeat("a", 64), Streams: []StreamCheckpoint{{Name: "root.mpxar.didx"}, {Name: "root.ppxar.didx"}}, Walk: json.RawMessage(`{"private_path":"Download/private-file.txt"}`), UploadedBytes: 12345}
	sealed, err := sealResumeEnvelope(key, want)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("private-file")) {
		t.Fatal("plaintext source metadata reached encrypted state")
	}
	got, err := openResumeEnvelope(key, want.Binding, want.Sequence, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Walk, want.Walk) || got.UploadedBytes != want.UploadedBytes {
		t.Fatal("lost continuation metadata")
	}
	again, err := sealResumeEnvelope(key, want)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sealed, again) {
		t.Fatal("resume encryption reused a nonce")
	}
	for _, test := range []struct {
		key      []byte
		binding  string
		sequence uint64
	}{
		{bytes.Repeat([]byte{2}, 32), want.Binding, want.Sequence},
		{key, strings.Repeat("b", 64), want.Sequence},
		{key, want.Binding, want.Sequence + 1},
	} {
		if _, err := openResumeEnvelope(test.key, test.binding, test.sequence, sealed); err == nil {
			t.Fatal("accepted wrong resume key, run or sequence")
		}
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := openResumeEnvelope(key, want.Binding, want.Sequence, sealed); err == nil {
		t.Fatal("accepted tampered resume state")
	}
}
