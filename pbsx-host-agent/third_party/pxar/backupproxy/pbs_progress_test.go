package backupproxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/pbs-plus/pxar/datastore"
)

func progressPayload(t *testing.T, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	return data
}

func uploadedProgressTotal(mock *mockPBSProtocol) (size, chunks uint64) {
	for _, chunk := range mock.chunks {
		size += uint64(len(chunk))
	}
	return size, uint64(len(mock.chunks))
}

func TestPBSUploadArchiveReportsBeforeEndOfStream(t *testing.T) {
	session, _ := newTestPBSSession(t)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	progress := make(chan UploadProgress, 1)
	session.config.OnUploadProgress = func(value UploadProgress) {
		if value.UploadedBytes > 0 {
			select {
			case progress <- value:
			default:
			}
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := session.UploadArchive(context.Background(), "root.ppxar.didx", reader)
		done <- err
	}()
	// Supply a full chunker read buffer but withhold EOF. The display must
	// advance while the source and upload are still running.
	if _, err := writer.Write(progressPayload(t, 256<<10)); err != nil {
		t.Fatal(err)
	}
	var failure string
	select {
	case value := <-progress:
		if value.ProcessedChunks == 0 || value.UploadedChunks == 0 {
			failure = "upload progress did not report completed chunks"
		}
	case <-time.After(3 * time.Second):
		failure = "no upload progress before end of stream"
	}
	writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if failure != "" {
		t.Fatal(failure)
	}
}

func TestPBSUploadArchiveProgressDedupAndCumulativeTotals(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "encrypted-compressed"}[encrypted], func(t *testing.T) {
			session, mock := newTestPBSSession(t)
			if encrypted {
				session.compress = true
				var key [32]byte
				key[0] = 42
				crypt, err := datastore.NewCryptConfig(key)
				if err != nil {
					t.Fatal(err)
				}
				session.config.CryptConfig = crypt
				session.config.CryptMode = datastore.CryptModeEncrypt
			}
			var updates []UploadProgress
			session.config.OnUploadProgress = func(value UploadProgress) { updates = append(updates, value) }
			data := progressPayload(t, 64<<10)
			if encrypted {
				data = bytes.Repeat([]byte("compressible encrypted backup payload"), 4096)
			}
			if _, err := session.UploadArchive(context.Background(), "root.ppxar.didx", bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
			if len(updates) == 0 {
				t.Fatal("archive upload never reported progress")
			}
			first := updates[len(updates)-1]
			encodedSize, chunks := uploadedProgressTotal(mock)
			if first.ProcessedBytes != uint64(len(data)) || first.UploadedBytes != encodedSize || first.UploadedChunks != chunks {
				t.Fatalf("first progress = %+v, encoded bytes = %d, chunks = %d", first, encodedSize, chunks)
			}
			// Same data in the same session must count as processed but must
			// not increase the network upload counter.
			if _, err := session.UploadArchive(context.Background(), "copy.ppxar.didx", bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
			last := updates[len(updates)-1]
			if last.ProcessedBytes != 2*first.ProcessedBytes || last.ProcessedChunks != 2*first.ProcessedChunks || last.UploadedBytes != first.UploadedBytes || last.UploadedChunks != first.UploadedChunks {
				t.Fatalf("deduplicated progress = %+v, first = %+v", last, first)
			}
		})
	}
}

func TestPBSSplitProgressCombinesConcurrentArchives(t *testing.T) {
	session, mock := newTestPBSSession(t)
	var updates []UploadProgress
	// Deliberately no test-side mutex: the API must serialize callbacks from
	// the two archive goroutines. The race detector checks that contract.
	session.config.OnUploadProgress = func(value UploadProgress) { updates = append(updates, value) }
	metadata, payload := progressPayload(t, 80<<10), progressPayload(t, 160<<10)
	if _, err := session.UploadSplitArchive(context.Background(), "root.mpxar.didx", bytes.NewReader(metadata), "root.ppxar.didx", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if len(updates) < 2 {
		t.Fatal("split upload never reported live progress")
	}
	var previous UploadProgress
	for _, update := range updates {
		if update.ProcessedChunks <= previous.ProcessedChunks || update.ProcessedBytes < previous.ProcessedBytes || update.UploadedBytes < previous.UploadedBytes || update.UploadedChunks < previous.UploadedChunks {
			t.Fatalf("progress moved backwards: %+v -> %+v", previous, update)
		}
		previous = update
	}
	encodedSize, chunks := uploadedProgressTotal(mock)
	if previous.ProcessedBytes != uint64(len(metadata)+len(payload)) || previous.UploadedBytes != encodedSize || previous.UploadedChunks != chunks {
		t.Fatalf("split totals = %+v, encoded bytes = %d, chunks = %d", previous, encodedSize, chunks)
	}
}

type rejectedProgressUpload struct{ *mockPBSProtocol }

func (p *rejectedProgressUpload) dynamicChunkUpload(uint64, string, int, int, []byte) error {
	return errors.New("fixture upload rejected")
}

func TestPBSUploadFailureDoesNotReportSuccessfulBytes(t *testing.T) {
	session, mock := newTestPBSSession(t)
	session.proto = &rejectedProgressUpload{mock}
	var updates []UploadProgress
	session.config.OnUploadProgress = func(value UploadProgress) { updates = append(updates, value) }
	if _, err := session.UploadArchive(context.Background(), "root.ppxar.didx", bytes.NewReader([]byte("payload"))); err == nil {
		t.Fatal("upload failure was ignored")
	}
	for _, update := range updates {
		if update.UploadedBytes != 0 || update.UploadedChunks != 0 {
			t.Fatalf("failed upload reported success: %+v", update)
		}
	}
}

func TestPBSInterleavedAndFollowingArchiveProgressStaysCumulative(t *testing.T) {
	session, mock := newTestPBSSession(t)
	var updates []UploadProgress
	session.config.OnUploadProgress = func(value UploadProgress) { updates = append(updates, value) }
	data := progressPayload(t, 64<<10)
	injections := make(chan InjectChunks)
	close(injections)
	if _, err := session.UploadPayloadInterleaved(context.Background(), "root.ppxar.didx", bytes.NewReader(data), injections, nil); err != nil {
		t.Fatal(err)
	}
	if len(updates) == 0 {
		t.Fatal("interleaved upload did not report progress")
	}
	first := updates[len(updates)-1]
	metadata := []byte("metadata archive")
	if _, err := session.UploadArchive(context.Background(), "root.mpxar.didx", bytes.NewReader(metadata)); err != nil {
		t.Fatal(err)
	}
	last := updates[len(updates)-1]
	encodedSize, _ := uploadedProgressTotal(mock)
	if last.ProcessedBytes != uint64(len(data)+len(metadata)) || last.UploadedBytes != encodedSize || last.UploadedBytes <= first.UploadedBytes {
		t.Fatalf("archive progress reset after payload: %+v -> %+v", first, last)
	}
}
