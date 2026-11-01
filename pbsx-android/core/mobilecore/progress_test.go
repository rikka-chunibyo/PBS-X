package mobilecore

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

type detailSpy struct {
	mu      sync.Mutex
	states  []transferState
	updates chan transferState
}

func (s *detailSpy) UpdateJSON(raw string) {
	var state transferState
	if json.Unmarshal([]byte(raw), &state) != nil {
		panic("invalid progress JSON")
	}
	s.mu.Lock()
	s.states = append(s.states, state)
	s.mu.Unlock()
	if s.updates != nil {
		select {
		case s.updates <- state:
		default:
		}
	}
}
func (s *detailSpy) latest() transferState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.states[len(s.states)-1]
}

func TestReadProgressDoesNotDependOnUploadedChunks(t *testing.T) {
	spy := &detailSpy{}
	report := newTransferReporter(context.Background(), spy)
	defer report.close()
	docs := &fsDocuments{root: t.TempDir()}
	writeFixture(t, docs, "Photos/a.txt", "abcdefgh")
	fs := &documentFS{ctx: context.Background(), docs: docs, report: report}
	file, _, err := fs.OpenFile("Photos/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 3)
	if _, err := io.ReadFull(file, buffer); err != nil {
		t.Fatal(err)
	}
	report.send()
	first := spy.latest()
	if first.Stage != "Backing up" || first.Path != "Photos/a.txt" || first.FileBytes != 3 || first.FileTotal != 8 || first.Bytes != 3 || first.UploadedBytes != 0 {
		t.Fatalf("bad live read progress: %+v", first)
	}
	if _, err := io.Copy(io.Discard, file); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	report.send()
	last := spy.latest()
	if last.Bytes != 8 || last.Files != 1 || last.UploadedBytes != 0 {
		t.Fatalf("unchanged/deduplicated reads lost progress: %+v", last)
	}
	report.uploaded(5)
	report.uploaded(2)
	report.send()
	if spy.latest().UploadedBytes != 5 {
		t.Fatal("out-of-order upload callbacks moved progress backwards")
	}
}

func TestReporterPublishesWhileProviderIsBlockedAndStopsOnClose(t *testing.T) {
	spy := &detailSpy{updates: make(chan transferState, 20)}
	report := newTransferReporter(context.Background(), spy)
	report.phase("Scanning folders", "Photos/2026", -1)
	select {
	case <-spy.updates: // Initial connection snapshot.
	case <-time.After(time.Second):
		t.Fatal("no initial progress")
	}
	select {
	case value := <-spy.updates:
		if value.Stage != "Scanning folders" || value.Path != "Photos/2026" {
			t.Fatal(value)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked work did not publish its current phase")
	}
	report.close()
	spy.mu.Lock()
	count := len(spy.states)
	spy.mu.Unlock()
	time.Sleep(250 * time.Millisecond)
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.states) != count {
		t.Fatal("progress callback arrived after operation closed")
	}
}

func TestRestoreReportsFileAndLogicalBytes(t *testing.T) {
	fs := archiveFixture(t, map[string]string{"Photos/a.txt": "restore bytes"})
	spy := &detailSpy{}
	report := newTransferReporter(context.Background(), spy)
	defer report.close()
	dest := &fsDocuments{root: t.TempDir()}
	stats, err := restoreSelectedReporting(context.Background(), fs, []string{"Photos/a.txt"}, "fail", dest, nil, report)
	if err != nil {
		t.Fatal(err)
	}
	report.send()
	last := spy.latest()
	if last.Stage != "Restoring" || last.Path != "Photos/a.txt" || last.Files != stats.Files || last.Bytes != stats.Bytes || last.FileBytes != last.FileTotal {
		t.Fatalf("bad restore progress: %+v", last)
	}
	if _, err := os.Stat(dest.full("Photos/a.txt")); err != nil {
		t.Fatal(err)
	}
}
