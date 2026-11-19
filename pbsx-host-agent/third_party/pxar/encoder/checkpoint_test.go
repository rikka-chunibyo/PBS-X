package encoder

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/pbs-plus/pxar/format"
)

func checkpointFixture(t *testing.T, split, resume bool) ([]byte, []byte) {
	t.Helper()
	var meta, payload bytes.Buffer
	var payloadOut io.Writer
	if split {
		payloadOut = &payload
	}
	e := NewEncoder(&meta, payloadOut, dirMetadata(0755), nil)
	var fw *FileWriter
	check := func() {
		t.Helper()
		if !resume {
			return
		}
		raw, err := e.Checkpoint()
		if err != nil {
			t.Fatal(err)
		}
		if e.MetadataPosition() != uint64(meta.Len()) || (split && e.PayloadPosition() != uint64(payload.Len())) {
			t.Fatal("saved positions differ from output")
		}
		// Deliberately discard the original encoder and its writer, retaining only
		// serialized state plus the acknowledged output. No source replay occurs.
		e, fw, err = ResumeEncoder(&meta, payloadOut, raw)
		if err != nil {
			t.Fatal(err)
		}
	}
	check()
	if _, err := e.AddFile(fileMetadata(0644, 1, 1), "first", []byte("first file")); err != nil {
		t.Fatal(err)
	}
	check()
	if err := e.CreateDirectory("nested", dirMetadata(0755)); err != nil {
		t.Fatal(err)
	}
	if err := e.CreateDirectory("deeper", dirMetadata(0700)); err != nil {
		t.Fatal(err)
	}
	check()
	var err error
	fw, err = e.CreateFile(fileMetadata(0600, 1, 1), "large", 15)
	if err != nil {
		t.Fatal(err)
	}
	check()
	for _, block := range []string{"12345", "67890", "abcde"} {
		if err := fw.WriteAll([]byte(block)); err != nil {
			t.Fatal(err)
		}
		progress := fw.FileProgress()
		check()
		if fw.FileProgress() != progress {
			t.Fatal("lost current-file offset")
		}
	}
	offset := fw.FileOffset()
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}
	check()
	if err := e.AddHardlink("linked", "large", offset); err != nil {
		t.Fatal(err)
	}
	if err := e.Finish(); err != nil {
		t.Fatal(err)
	}
	check()
	if _, err := e.AddFile(fileMetadata(0644, 1, 1), "empty", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Finish(); err != nil {
		t.Fatal(err)
	}
	check()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	return meta.Bytes(), payload.Bytes()
}

func TestCheckpointResumeIsByteIdenticalIncludingMidFileAndDirectoryStack(t *testing.T) {
	for _, split := range []bool{false, true} {
		wantMeta, wantPayload := checkpointFixture(t, split, false)
		gotMeta, gotPayload := checkpointFixture(t, split, true)
		if !bytes.Equal(wantMeta, gotMeta) || !bytes.Equal(wantPayload, gotPayload) {
			t.Fatalf("split=%v: resumed archive differs", split)
		}
	}
}

func TestCheckpointRejectsInvalidStateBeforeWriting(t *testing.T) {
	var meta, payload bytes.Buffer
	e := NewEncoder(&meta, &payload, dirMetadata(0755), nil)
	raw, err := e.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*encoderCheckpoint){
		func(c *encoderCheckpoint) { c.Version = 2 },
		func(c *encoderCheckpoint) { c.Directories = nil },
		func(c *encoderCheckpoint) { c.Directories[0].Parent = 0 },
		func(c *encoderCheckpoint) { c.Directories[0].Position = 0 },
		func(c *encoderCheckpoint) { c.Directories[0].Items = []format.GoodbyeItem{{Offset: 999999}} },
		func(c *encoderCheckpoint) { c.File = &fileCheckpoint{Size: 2, Remaining: 3} },
		func(c *encoderCheckpoint) { c.Directories = append(c.Directories, c.Directories[0]) },
	} {
		var c encoderCheckpoint
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		mutate(&c)
		bad, _ := json.Marshal(c)
		var out, po bytes.Buffer
		if _, _, err := ResumeEncoder(&out, &po, bad); err == nil {
			t.Fatalf("accepted invalid checkpoint: %s", bad)
		}
		if out.Len() != 0 || po.Len() != 0 {
			t.Fatal("wrote output while validating")
		}
	}
	for _, bad := range [][]byte{append(append([]byte(nil), raw...), []byte(" {}")...), bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"unknown":true`), 1)} {
		if _, _, err := ResumeEncoder(io.Discard, io.Discard, bad); err == nil {
			t.Fatal("accepted trailing or unknown fields")
		}
	}
	if _, _, err := ResumeEncoder(io.Discard, nil, raw); err == nil {
		t.Fatal("accepted mismatched streams")
	}
}

type failingCheckpointWriter struct{}

func (failingCheckpointWriter) Write(p []byte) (int, error) {
	return len(p) / 2, errors.New("output failed")
}

type shortCheckpointWriter struct{}

func (shortCheckpointWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestCheckpointRejectsShortPayloadWrites(t *testing.T) {
	var meta, payload bytes.Buffer
	e := NewEncoder(&meta, shortCheckpointWriter{}, dirMetadata(0755), nil)
	if _, err := e.Checkpoint(); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("payload header short write hidden: %v", err)
	}
	e = NewEncoder(&meta, &payload, dirMetadata(0755), nil)
	e.payloadOut = shortCheckpointWriter{}
	if _, err := e.CreateFile(fileMetadata(0600, 1, 1), "bad", 4); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("file header short write hidden: %v", err)
	}
	if _, err := e.Checkpoint(); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("checkpoint accepted a short output: %v", err)
	}
}

func TestCheckpointCannotHideFailedWritesOrOpenFile(t *testing.T) {
	var meta, payload bytes.Buffer
	e := NewEncoder(&meta, &payload, dirMetadata(0755), nil)
	fw, err := e.CreateFile(fileMetadata(0600, 1, 1), "file", 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.AddFile(fileMetadata(0600, 1, 1), "other", nil); err == nil {
		t.Fatal("mutated encoder with open file")
	}
	if err := e.Finish(); err == nil {
		t.Fatal("finished directory with open file")
	}
	if err := e.Close(); err == nil {
		t.Fatal("closed archive with open file")
	}
	e.payloadOut = failingCheckpointWriter{}
	if _, err := fw.Write([]byte("test")); err == nil {
		t.Fatal("lost write failure")
	}
	if _, err := e.Checkpoint(); err == nil {
		t.Fatal("checkpoint accepted a failed output prefix")
	}
	if err := fw.Close(); err == nil {
		t.Fatal("closed failed file")
	}
}
