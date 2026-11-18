package pxar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

func TestSparseWriterPreservesPayloadAcrossWriteBoundaries(t *testing.T) {
	data := make([]byte, 8192)
	copy(data, "outside map")
	copy(data[64:], "new data inside stale hole")
	copy(data[5000:], "second range now contains data")
	holes := []sparseHole{{Offset: 32, Length: 4000}, {Offset: 4900, Length: 3292}}
	for _, chunk := range []int{1, 7, 1024, 65536} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "staged")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			writer := &sparseRestoreWriter{file: file, holes: holes}
			for offset := 0; offset < len(data); offset += chunk {
				part := data[offset:min(offset+chunk, len(data))]
				n, err := writer.Write(part)
				if err != nil || n != len(part) {
					t.Fatalf("write = %d, %v", n, err)
				}
			}
			if err := file.Truncate(int64(len(data))); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(file.Name())
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("sparse writing changed payload or EOF: %v", err)
			}
		})
	}
}

func TestSparseWriterReportsWriteFailure(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	for _, data := range [][]byte{{0}, {1}} {
		writer := &sparseRestoreWriter{file: file, holes: []sparseHole{{Length: 1}}}
		if n, err := writer.Write(data); err == nil || n != 0 {
			t.Fatalf("closed output accepted: %d %v", n, err)
		}
	}
}

func sparseTestAttrs(t *testing.T, size uint64, holes []sparseHole) map[string][]byte {
	t.Helper()
	encoded, err := cbor.Marshal(holes)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	manifest, err := cbor.Marshal(sparseRestoreManifest{Size: size, Digest: digest, Chunks: 1})
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{XAttrSparseManifest: manifest, XAttrSparsePrefix + digest[:16] + ".000000": encoded}
}

type sparseFixture struct {
	*restoreFixture
	attrs map[string][]byte
}

func (f *sparseFixture) ListXAttrs(_ context.Context, start, _ uint64) (map[string][]byte, error) {
	attrs := map[string][]byte{}
	if start == 100 {
		for name, data := range f.attrs {
			attrs[name] = append([]byte(nil), data...)
		}
	}
	return attrs, nil
}

func TestInvalidSparseMapPreservesExistingFile(t *testing.T) {
	fixture := &sparseFixture{restoreFixture: fileFixture("new"), attrs: sparseTestAttrs(t, 4, []sparseHole{{Length: 3}})}
	fixture.entries[0].EntryRangeStart = 100
	client, reported := NewLocalClient(fixture, "invalid-sparse")
	defer client.Close()
	dest := t.TempDir()
	dir := filepath.Join(dest, "folder")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreWithOptions(t.Context(), client, []string{"/"}, RestoreOptions{Mode: RestoreModeNormal, DestDir: dest}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reported:
		if err == nil || !strings.Contains(err.Error(), "sparse manifest size") {
			t.Fatalf("wrong restore error: %v", err)
		}
	default:
		t.Fatal("invalid sparse map was accepted")
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "original" {
		t.Fatalf("failed restore replaced original: %q %v", got, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".pxar-restore-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("failed restore leaked staging files: %v %v", leftovers, err)
	}
}

func TestSparseRestoreKeepsTrailingZeroLength(t *testing.T) {
	data := make([]byte, 1<<20)
	fixture := &sparseFixture{restoreFixture: fileFixture(string(data)), attrs: sparseTestAttrs(t, uint64(len(data)), []sparseHole{{Length: uint64(len(data))}})}
	fixture.entries[0].EntryRangeStart = 100
	client, reported := NewLocalClient(fixture, "trailing-holes")
	defer client.Close()
	dest := t.TempDir()
	if err := RestoreWithOptions(t.Context(), client, []string{"/"}, RestoreOptions{Mode: RestoreModeNormal, DestDir: dest}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reported:
		t.Fatal(err)
	default:
	}
	file, err := os.Open(filepath.Join(dest, "folder", "file"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("trailing holes changed contents or length: %v", err)
	}
}
