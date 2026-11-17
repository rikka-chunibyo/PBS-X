package pxar

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-x/pbsx-host-agent/internal/testutil/pbsfixture"
)

func TestPBSRestoreVerifiesChunksBeforeReplacingDestination(t *testing.T) {
	for _, split := range []bool{false, true} {
		for _, corrupt := range []bool{false, true} {
			t.Run(fmt.Sprintf("split=%t/corrupt=%t", split, corrupt), func(t *testing.T) {
				s := pbsfixture.New(t, split)
				if corrupt {
					changed := false
					for digest, blob := range s.Chunks {
						plain, err := datastore.DecodeBlob(nil, blob)
						if err != nil {
							t.Fatal(err)
						}
						if bytes.Contains(plain, []byte(pbsfixture.Content)) {
							altered := bytes.ReplaceAll(plain, []byte(pbsfixture.Content), []byte("EVIL-PAYLOAD"))
							s.Chunks[digest], err = datastore.EncodeCompressedBlob(nil, altered)
							if err != nil {
								t.Fatal(err)
							}
							changed = true
						}
					}
					if !changed {
						t.Fatal("fixture did not replace content")
					}
				}
				cfg := s.Start(t)
				reader, err := NewPBSArchiveReader(t.Context(), cfg, pbsfixture.HostID, pbsfixture.Stamp)
				if err != nil {
					t.Fatal(err)
				}
				client, reported := NewLocalClient(reader, "fixture")
				defer client.Close()
				dest := t.TempDir()
				file := filepath.Join(dest, "file.txt")
				if err := os.WriteFile(file, []byte("LOCAL-ORIGINAL"), 0600); err != nil {
					t.Fatal(err)
				}
				restoreErr := RestoreWithOptions(t.Context(), client, []string{"/"}, RestoreOptions{Mode: RestoreModeNoAttr, DestDir: dest})
				select {
				case err := <-reported:
					if restoreErr == nil {
						restoreErr = err
					}
				default:
				}
				want := pbsfixture.Content
				if corrupt {
					want = "LOCAL-ORIGINAL"
					if restoreErr == nil || !strings.Contains(restoreErr.Error(), "digest mismatch") {
						t.Fatalf("missing digest error: %v", restoreErr)
					}
				} else if restoreErr != nil {
					t.Fatal(restoreErr)
				}
				got, err := os.ReadFile(file)
				if err != nil || string(got) != want {
					t.Fatalf("destination = %q, want %q: %v", got, want, err)
				}
				if s.Requests("index.json.blob") != 1 {
					t.Fatal("restore did not verify snapshot manifest")
				}
				leftovers, err := filepath.Glob(filepath.Join(dest, ".pxar-restore-*"))
				if err != nil || len(leftovers) != 0 {
					t.Fatalf("temporary restore files leaked: %v %v", leftovers, err)
				}
			})
		}
	}
}

func TestPBSArchiveReaderRejectsCorruptSplitIndexWithoutLegacyFallback(t *testing.T) {
	s := pbsfixture.New(t, true)
	legacy := pbsfixture.New(t, false)
	s.Manifest.Files = append(s.Manifest.Files, legacy.Manifest.Files...)
	s.Files["root.pxar.didx"] = legacy.Files["root.pxar.didx"]
	s.WriteManifest(t)
	data := s.Files["root.mpxar.didx"]
	data[len(data)-1] ^= 1
	cfg := s.Start(t)
	reader, err := NewPBSArchiveReader(t.Context(), cfg, pbsfixture.HostID, pbsfixture.Stamp)
	if reader != nil {
		reader.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("corrupt split archive accepted: %v", err)
	}
	if s.Requests("root.pxar.didx") != 0 {
		t.Fatal("silently fell back to legacy root")
	}
}
