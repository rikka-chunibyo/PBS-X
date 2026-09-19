package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseFingerprint(t *testing.T) {
	input := "00:01:02:03:04:05:06:07:08:09:0A:0B:0C:0D:0E:0F:10:11:12:13:14:15:16:17:18:19:1A:1B:1C:1D:1E:1F"
	got, err := parseFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 0 || got[31] != 0x1f {
		t.Fatalf("unexpected fingerprint bytes: %x", got)
	}
}

func TestPercentEncode(t *testing.T) {
	got := percentEncode("abc XYZ!@#")
	want := "abc%20XYZ%21%40%23"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHeartbeatRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/admin/host/test-host/heartbeat" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "PBSAPIToken agent@pbs!test:secret%20value" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("unexpected content type %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["agent-version"] != agentVersion {
			t.Fatalf("unexpected agent version %#v", payload["agent-version"])
		}
		if payload["hostname"] == "" {
			t.Fatal("heartbeat did not contain a hostname")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()

	a := &agent{
		cfg: config{
			server:      server.URL,
			hostID:      "test-host",
			tokenID:     "agent@pbs!test",
			tokenSecret: "secret value",
		},
		client: server.Client(),
	}

	if err := a.heartbeat(); err != nil {
		t.Fatal(err)
	}
}
