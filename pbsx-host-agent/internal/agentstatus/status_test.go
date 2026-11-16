package agentstatus

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStatusFreshnessAndStoppedState(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, tc := range []struct {
		phase   string
		updated int64
		want    bool
	}{
		{"ready", 999, true}, {"ready", 980, false}, {"ready", 1010, false}, {"stopped", 1000, false}, {"", 0, false},
	} {
		if got := (Snapshot{Phase: tc.phase, UpdatedAt: tc.updated}).Running(now); got != tc.want {
			t.Fatalf("%+v: got %v", tc, got)
		}
	}
}

func TestPublishedStatusContainsOnlyPublicFields(t *testing.T) {
	mu.Lock()
	state = Snapshot{Version: "0.7.0-preview", Phase: "ready"}
	mu.Unlock()
	Progress(Operation{Kind: "backup", Stage: "streaming", Files: 12, ProcessedBytes: 1024})
	data, err := json.Marshal(current())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"token", "secret", "password", "server_url", "source_path"} {
		if strings.Contains(string(data), name) {
			t.Fatalf("unexpected sensitive field %s", name)
		}
	}
	Finished(false)
	value := current()
	if value.Operation != nil || value.LastSuccess == nil || *value.LastSuccess {
		t.Fatalf("completion not recorded: %+v", value)
	}
}
