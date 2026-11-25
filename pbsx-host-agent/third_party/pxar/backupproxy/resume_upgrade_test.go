package backupproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
)

func TestResumeUpgradeKeepsTokenOutOfURL(t *testing.T) {
	token := strings.Repeat("a1", 32)
	type request struct{ query, header, flag string }
	requests := make(chan request, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- request{r.URL.RawQuery, r.Header.Get("PBSX-Resume-Token"), r.URL.Query().Get("pbsx-resume")}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	cfg := BackupConfig{BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1, ResumeToken: token}
	if conn, err := dialPBSH2(context.Background(), server.URL, "fixture", "test:secret", cfg, nil, true, nil); err == nil {
		conn.close()
		t.Fatal("fixture unexpectedly upgraded")
	}
	got := <-requests
	if got.header != token || got.flag != "1" || strings.Contains(got.query, token) {
		t.Fatal("resume secret was absent from its header or included in the URL")
	}
}

func TestResumeUpgradeRejectsInvalidTokensBeforeDial(t *testing.T) {
	for _, token := range []string{"short", strings.Repeat("g", 64), strings.Repeat("A", 64), strings.Repeat("a", 62) + "\r\n"} {
		_, err := dialPBSH2(context.Background(), "https://unused.invalid", "fixture", "test:secret", BackupConfig{ResumeToken: token}, nil, true, nil)
		if err == nil || err.Error() != "invalid resume token" {
			t.Fatalf("invalid token reached dial: %v", err)
		}
	}
}

func TestDiscardRefusesTruncatedAbsenceResponse(t *testing.T) {
	const body = "incomplete checkpoint does not exist"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	store := NewPBSStore(PBSConfig{
		BaseURL: server.URL, Datastore: "fixture", AuthToken: "audit@pbs!fixture:dummy", SkipTLSVerify: true,
	}, buzhash.DefaultConfig(), false)
	absent, err := store.RequestDocumentBackupDiscard(t.Context(), BackupConfig{
		BackupType: datastore.BackupHost, BackupID: "fixture", BackupTime: 1, ResumeToken: strings.Repeat("a1", 32),
	})
	if absent || err == nil {
		t.Fatalf("truncated reply was accepted as confirmation: absent=%t, error=%v", absent, err)
	}
}
