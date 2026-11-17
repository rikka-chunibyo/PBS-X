package transfer_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-plus/pxar/transfer"
)

type decodedFixtureSource struct {
	data []byte
	err  error
}

func (s decodedFixtureSource) GetChunk([32]byte) ([]byte, error) {
	return nil, errors.New("decoded source must not be re-encoded or decoded again")
}

func (s decodedFixtureSource) GetDecodedChunk([32]byte) ([]byte, error) {
	return s.data, s.err
}

func TestReadSeekerDecodedSourceAndVerificationError(t *testing.T) {
	data := []byte("already decoded and verified chunk")
	writer := datastore.NewDynamicIndexWriter(1)
	writer.Add(uint64(len(data)), sha256.Sum256(data))
	raw, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	index, err := datastore.ParseDynamicIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	source := decodedFixtureSource{data: data}
	got, err := io.ReadAll(transfer.NewReadSeeker(index, source, 1))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("decoded source read = %q, error = %v", got, err)
	}
	want := errors.New("verification failed")
	source.err = want
	got, err = io.ReadAll(transfer.NewReadSeeker(index, source, 1))
	if !errors.Is(err, want) || len(got) != 0 {
		t.Fatalf("verification error lost or returned unverified bytes: %q %v", got, err)
	}
}
