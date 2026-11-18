package mobilecore

import (
	"encoding/hex"
	"math"
	"strings"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
)

func TestManifestModesAndDuplicateFilesRequireValidation(t *testing.T) {
	key, err := parseBackupKey(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{keys: map[string]*datastore.CryptConfig{datastore.FormatFingerprint(key.Fingerprint()): key}}
	for _, test := range []struct {
		name, mode, fileMode   string
		sign, duplicate, valid bool
	}{
		{name: "legacy plain", valid: true},
		{name: "explicit plain", mode: "none", fileMode: "none", valid: true},
		{name: "signed", mode: "sign-only", fileMode: "sign-only", sign: true, valid: true},
		{name: "encrypted", mode: "encrypt", fileMode: "encrypt", sign: true, valid: true},
		{name: "missing signed signature", mode: "sign-only", fileMode: "sign-only"},
		{name: "missing per-file signature", fileMode: "sign-only"},
		{name: "unknown manifest mode", mode: "unknown"},
		{name: "unknown file mode", fileMode: "unknown"},
		{name: "signed unknown file mode", fileMode: "unknown", sign: true},
		{name: "duplicate plain", duplicate: true},
		{name: "duplicate signed", fileMode: "encrypt", duplicate: true, sign: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := &datastore.Manifest{BackupType: "host", BackupID: "fixture", BackupTime: 123, CryptMode: test.mode,
				Files: []datastore.BackupFileInfo{{Filename: "root.pxar.didx", CryptMode: test.fileMode}}}
			if test.duplicate {
				m.Files = append(m.Files, m.Files[0])
			}
			if test.sign {
				if err := datastore.SignManifest(m, key); err != nil {
					t.Fatal(err)
				}
			}
			plain, err := m.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			blob, err := datastore.EncodeBlob(nil, plain)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = c.snapshotManifest(blob, "host", "fixture", 123)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t: %v", test.valid, err)
			}
		})
	}
}

func TestAndroidIndexRejectsInvalidOffsetsWithValidChecksums(t *testing.T) {
	for _, offsets := range [][]uint64{nil, {0}, {20, 10}, {10, 10}, {math.MaxInt64 + 1}} {
		writer := datastore.NewDynamicIndexWriter(123)
		for _, offset := range offsets {
			writer.Add(offset, [32]byte{})
		}
		data, err := writer.Finish()
		if err != nil {
			t.Fatal(err)
		}
		sum := writer.Csum()
		var size uint64
		if len(offsets) != 0 {
			size = offsets[len(offsets)-1]
		}
		m := &datastore.Manifest{Files: []datastore.BackupFileInfo{{Filename: "root.mpxar.didx", CSum: hex.EncodeToString(sum[:]), Size: size}}}
		if err := datastore.VerifySnapshotIndex(m, "root.mpxar.didx", data); err == nil {
			t.Fatalf("invalid offsets with matching checksums accepted: %v", offsets)
		}
	}
}
