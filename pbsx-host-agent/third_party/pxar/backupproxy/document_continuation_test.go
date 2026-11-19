package backupproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/pbs-plus/pxar/format"
)

type continuationFixture struct {
	files  map[string][]byte
	opens  map[string]int
	lists  map[string]int
	client *LocalClient
}

func newContinuationFixture() *continuationFixture {
	f := &continuationFixture{files: map[string][]byte{"first": []byte("already finished"), "nested/empty": nil, "nested/large": bytes.Repeat([]byte("file content"), 20000), "nested/last": []byte("last")}, opens: map[string]int{}, lists: map[string]int{}}
	file := format.Stat{Mode: format.ModeIFREG | 0600}
	dir := format.Stat{Mode: format.ModeIFDIR | 0700}
	entry := func(name, full string) DirEntry {
		return DirEntry{Name: name, Stat: file, Size: uint64(len(f.files[full]))}
	}
	f.client = NewLocalClient(&mockFS{
		statFn: func(name string) (format.Stat, error) {
			if name == "." || name == "nested" {
				return dir, nil
			}
			if _, ok := f.files[name]; !ok {
				return format.Stat{}, errors.New("missing fixture source")
			}
			return file, nil
		},
		readDirFn: func(name string) ([]DirEntry, error) {
			f.lists[name]++
			if name == "." {
				return []DirEntry{entry("first", "first"), {Name: "nested", Stat: dir}}, nil
			}
			if name == "nested" {
				return []DirEntry{entry("empty", "nested/empty"), entry("large", "nested/large"), entry("last", "nested/last")}, nil
			}
			return nil, errors.New("unexpected fixture directory")
		},
		openFileFn: func(name string) (io.ReadCloser, uint64, error) {
			data, ok := f.files[name]
			if !ok {
				return nil, 0, errors.New("missing fixture file")
			}
			f.opens[name]++
			return io.NopCloser(bytes.NewReader(data)), uint64(len(data)), nil
		},
	})
	return f
}

func finishContinuation(t *testing.T, w *DocumentContinuation) {
	t.Helper()
	for i := 0; i < 100; i++ {
		done, err := w.Step(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if done {
			return
		}
	}
	t.Fatal("continuation did not finish")
}

func interruptedContinuation(t *testing.T) (*continuationFixture, *DocumentContinuation, *bytes.Buffer, *bytes.Buffer, []byte) {
	t.Helper()
	f := newContinuationFixture()
	meta, payload := new(bytes.Buffer), new(bytes.Buffer)
	w, err := NewDocumentContinuation(t.Context(), f.client, meta, payload)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if _, err = w.Step(t.Context()); err != nil {
			t.Fatal(err)
		}
		if w.state.File != nil && w.state.File.Path == "nested/large" && w.state.File.Offset == 64<<10 {
			raw, err := w.Checkpoint()
			if err != nil {
				t.Fatal(err)
			}
			return f, w, meta, payload, raw
		}
	}
	t.Fatal("did not reach interruption point")
	return nil, nil, nil, nil, nil
}

func TestDocumentContinuationResumesMidFileWithoutReopeningCompletedFiles(t *testing.T) {
	f, w, meta, payload, raw := interruptedContinuation(t)
	metaSize, payloadSize := meta.Len(), payload.Len()
	// Simulate bytes produced after the last durable server checkpoint, then a
	// lost process. Recovery appends only after that checkpoint's prefixes.
	if _, err := w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.Close()
	w = nil
	meta.Truncate(metaSize)
	payload.Truncate(payloadSize)
	var err error
	w, err = ResumeDocumentContinuation(f.client, meta, payload, raw)
	if err != nil {
		t.Fatal(err)
	}
	finishContinuation(t, w)
	if f.opens["first"] != 1 || f.opens["nested/empty"] != 1 || f.opens["nested/large"] != 2 {
		t.Fatalf("completed sources were replayed: %v", f.opens)
	}
	if f.lists["."] != 1 || f.lists["nested"] != 1 {
		t.Fatalf("directories were relisted: %v", f.lists)
	}
	base := newContinuationFixture()
	var wantMeta, wantPayload bytes.Buffer
	want, err := NewDocumentContinuation(t.Context(), base.client, &wantMeta, &wantPayload)
	if err != nil {
		t.Fatal(err)
	}
	finishContinuation(t, want)
	if !bytes.Equal(meta.Bytes(), wantMeta.Bytes()) || !bytes.Equal(payload.Bytes(), wantPayload.Bytes()) {
		t.Fatal("resumed document archive differs from uninterrupted backup")
	}
	if w.state.Bytes != want.state.Bytes || w.state.Files != want.state.Files {
		t.Fatal("resume lost cumulative progress")
	}
}

func TestDocumentContinuationRejectsChangedInterruptedSourceBeforeAppending(t *testing.T) {
	f, w, meta, payload, raw := interruptedContinuation(t)
	w.Close()
	f.files["nested/large"][0] ^= 1 // Same size/mtime: prefix verification must catch this.
	resumed, err := ResumeDocumentContinuation(f.client, meta, payload, raw)
	if err != nil {
		t.Fatal(err)
	}
	m, p := meta.Len(), payload.Len()
	if _, err = resumed.Step(t.Context()); err == nil {
		t.Fatal("accepted changed interrupted source")
	}
	if meta.Len() != m || payload.Len() != p {
		t.Fatal("appended data from a changed source")
	}
	if _, err = resumed.Checkpoint(); err == nil {
		t.Fatal("saved a checkpoint after a failed step")
	}
}

func TestDocumentContinuationRejectsInvalidTraversalAndEncoderMismatch(t *testing.T) {
	f, w, _, _, raw := interruptedContinuation(t)
	w.Close()
	for _, mutate := range []func(*documentWalkCheckpoint){
		func(c *documentWalkCheckpoint) { c.Frames[1].Path = "../outside" },
		func(c *documentWalkCheckpoint) { c.Frames[0].Next = 0 },
		func(c *documentWalkCheckpoint) { c.File.Size++ },
		func(c *documentWalkCheckpoint) { c.File.Offset++ },
		func(c *documentWalkCheckpoint) { c.Frames[1].Entries[0].Name = "../other" },
		func(c *documentWalkCheckpoint) { c.Frames = c.Frames[:1] },
	} {
		var c documentWalkCheckpoint
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		mutate(&c)
		bad, _ := json.Marshal(c)
		var meta, payload bytes.Buffer
		if _, err := ResumeDocumentContinuation(f.client, &meta, &payload, bad); err == nil {
			t.Fatal("accepted inconsistent document checkpoint")
		}
		if meta.Len() != 0 || payload.Len() != 0 {
			t.Fatal("wrote before checkpoint validation")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var meta, payload bytes.Buffer
	w, err := ResumeDocumentContinuation(f.client, &meta, &payload, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Step(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}
