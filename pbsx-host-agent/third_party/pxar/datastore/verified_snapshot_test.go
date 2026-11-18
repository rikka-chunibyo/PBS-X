package datastore

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

type indexFixture struct {
	files    map[string][]byte
	requests []string
	fail     string
}

func (s *indexFixture) DownloadFile(name string) ([]byte, error) {
	s.requests = append(s.requests, name)
	if name == s.fail || s.files[name] == nil {
		return nil, errors.New("fixture index unavailable")
	}
	return s.files[name], nil
}

func rootIndexFixture(t *testing.T, modes map[string]string) (*indexFixture, *Manifest) {
	t.Helper()
	s := &indexFixture{files: map[string][]byte{}}
	m := &Manifest{}
	for name, mode := range modes {
		writer := NewDynamicIndexWriter(123)
		writer.Add(1, [32]byte{})
		data, err := writer.Finish()
		if err != nil {
			t.Fatal(err)
		}
		s.files[name] = data
		sum := writer.Csum()
		m.Files = append(m.Files, BackupFileInfo{Filename: name, CryptMode: mode, CSum: hex.EncodeToString(sum[:]), Size: 1})
	}
	return s, m
}

func TestRootIndexDownloadFailureCannotFallBackToLegacy(t *testing.T) {
	for _, missing := range []string{"root.mpxar.didx", "root.ppxar.didx"} {
		t.Run(missing, func(t *testing.T) {
			s, m := rootIndexFixture(t, map[string]string{"root.mpxar.didx": "encrypt", "root.ppxar.didx": "encrypt", "root.pxar.didx": "none"})
			s.fail = missing
			if _, err := LoadRootIndexes(s, m); err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("missing split index accepted: %v", err)
			}
			for _, name := range s.requests {
				if name == "root.pxar.didx" {
					t.Fatal("download failure selected a different archive")
				}
			}
		})
	}
}

func TestRootIndexSelectionIgnoresUndeclaredSplitFiles(t *testing.T) {
	s, m := rootIndexFixture(t, map[string]string{"root.pxar.didx": "none"})
	s.files["root.mpxar.didx"] = []byte("undeclared file must not be requested")
	indexes, err := LoadRootIndexes(s, m)
	if err != nil || indexes.Legacy == nil || len(s.requests) != 1 || s.requests[0] != "root.pxar.didx" {
		t.Fatalf("manifest did not determine format: %+v %v %v", indexes, err, s.requests)
	}
}

func TestRootIndexPreflightRejectsIncompleteMixedAndDuplicateEntries(t *testing.T) {
	for _, modes := range []map[string]string{
		{"root.mpxar.didx": "none", "root.pxar.didx": "none"},
		{"root.ppxar.didx": "none", "root.pxar.didx": "none"},
		{"root.mpxar.didx": "encrypt", "root.ppxar.didx": "none"},
		{"root.mpxar.didx": "none", "root.ppxar.didx": "sign-only"},
	} {
		s, m := rootIndexFixture(t, modes)
		if _, err := LoadRootIndexes(s, m); err == nil || len(s.requests) != 0 {
			t.Fatalf("invalid manifest reached downloads: %v %v", err, s.requests)
		}
	}
	s, m := rootIndexFixture(t, map[string]string{"root.pxar.didx": "none"})
	m.Files = append(m.Files, m.Files[0])
	if _, err := LoadRootIndexes(s, m); err == nil || len(s.requests) != 0 {
		t.Fatalf("duplicate entries reached downloads: %v %v", err, s.requests)
	}
}

func TestRootIndexesAcceptPlainEncryptedAndSignedModes(t *testing.T) {
	for _, mode := range []string{"", "none", "encrypt", "sign-only"} {
		t.Run(mode, func(t *testing.T) {
			s, m := rootIndexFixture(t, map[string]string{"root.mpxar.didx": mode, "root.ppxar.didx": mode})
			indexes, err := LoadRootIndexes(s, m)
			if err != nil || indexes.Metadata == nil || indexes.Payload == nil {
				t.Fatalf("valid indexes rejected: %+v %v", indexes, err)
			}
		})
	}
	s, m := rootIndexFixture(t, map[string]string{"root.mpxar.didx": "", "root.ppxar.didx": "none"})
	if _, err := LoadRootIndexes(s, m); err != nil {
		t.Fatalf("equivalent default/plain modes rejected: %v", err)
	}
}
