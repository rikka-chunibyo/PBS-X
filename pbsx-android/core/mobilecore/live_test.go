//go:build integration

package mobilecore

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestLivePBS consumes only a disposable enrollment file supplied by the caller.
// Run it on the test PBS VM so device credentials never leave that machine.
// Its caller owns creation and cleanup of the dedicated host/job/namespace.
func TestLivePBS(t *testing.T) {
	enrollment := os.Getenv("PBSX_ANDROID_TEST_ENROLLMENT")
	namespace := os.Getenv("PBSX_ANDROID_TEST_NAMESPACE")
	if enrollment == "" || namespace == "" {
		t.Skip("requires disposable enrollment and namespace")
	}
	code, err := os.ReadFile(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	config, err := Enroll(string(code), "Android core integration fixture")
	if err != nil {
		t.Fatal(err)
	}
	var cfg configuration
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	details := &detailSpy{}
	if err := client.SetProgressDetails(details); err != nil {
		t.Fatal(err)
	}
	source := &fsDocuments{root: t.TempDir()}
	files := map[string][]byte{"Photos/猫.jpg": bytes.Repeat([]byte("photo bytes "), 200000), "Documents/empty": {}, "Documents/keep.txt": []byte("unchanged"), "Documents/change.txt": []byte("version one"), "Documents/delete.txt": []byte("delete me")}
	for name, data := range files {
		if err := os.MkdirAll(filepath.Dir(source.full(name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source.full(name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	progress := &uploadSpy{}
	if jobID := os.Getenv("PBSX_ANDROID_TEST_DISABLED_JOB"); jobID != "" {
		_, err := client.Backup("backup", namespace, source, nil)
		if err == nil || !strings.Contains(err.Error(), "Backup destination needs setup") || !strings.Contains(err.Error(), "enrollment is already valid") {
			t.Fatalf("missing enabled job should explain setup without re-enrollment: %v", err)
		}
		output, err := exec.Command("proxmox-backup-debug", "api", "set", "/config/host-backup/"+jobID, "--enable", "true").CombinedOutput()
		if err != nil {
			t.Fatalf("enable disposable host target: %v: %s", err, output)
		}
		t.Log("Missing enabled job rejected with setup guidance; enabled target using the same enrolled key")
	}
	if _, err := client.Backup("backup", namespace, source, progress); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	atomic.StoreInt64(&progress.bytes, 0)
	if _, err := client.Backup("backup", namespace, source, progress); err != nil {
		t.Fatal(err)
	}
	if uploaded := atomic.LoadInt64(&progress.bytes); uploaded != 0 {
		t.Fatalf("unchanged snapshot uploaded %d new chunk bytes", uploaded)
	}
	var logical int64
	for _, value := range files {
		logical += int64(len(value))
	}
	liveProgress := details.latest()
	if liveProgress.Bytes != logical || liveProgress.Files != int64(len(files)) || liveProgress.UploadedBytes != 0 {
		t.Fatalf("unchanged live PBS backup did not publish logical progress: %+v", liveProgress)
	}
	t.Log("Unchanged snapshot reused every data chunk")
	time.Sleep(1100 * time.Millisecond)
	files["Documents/change.txt"] = []byte("version two")
	files["Documents/new.txt"] = []byte("added file")
	delete(files, "Documents/delete.txt")
	if err := os.Remove(source.full("Documents/delete.txt")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Documents/change.txt", "Documents/new.txt"} {
		if err := os.WriteFile(source.full(name), files[name], 0600); err != nil {
			t.Fatal(err)
		}
	}
	jobResult, err := client.BackupJob("backup", namespace, "fixture-job", "Photo job", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	var named struct {
		Snapshot string `json:"snapshot"`
		Warning  string `json:"warning"`
	}
	if err := json.Unmarshal([]byte(jobResult), &named); err != nil || named.Snapshot == "" || named.Warning != "" {
		t.Fatalf("named backup did not record its note: %s (%v)", jobResult, err)
	}
	raw, err := client.ListSnapshots("backup", namespace, "")
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data []struct {
			Time    int64  `json:"backup-time"`
			Comment string `json:"comment"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 3 {
		t.Fatalf("got %d snapshots, expected 3", len(response.Data))
	}
	sort.Slice(response.Data, func(i, j int) bool { return response.Data[i].Time < response.Data[j].Time })
	if !strings.Contains(response.Data[2].Comment, "Photo job") {
		t.Fatalf("snapshot list did not expose job name: %q", response.Data[2].Comment)
	}
	catalogueJSON, err := client.ListAccessibleBackups()
	if err != nil {
		t.Fatal(err)
	}
	var catalogue backupCatalogue
	if err := json.Unmarshal([]byte(catalogueJSON), &catalogue); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range catalogue.Groups {
		if group.Store != "backup" || group.Namespace != namespace || group.Type != "host" || group.ID != cfg.HostID {
			t.Fatalf("automatic key discovered an unauthorized group: %+v", group)
		}
		if group.Count != 3 {
			t.Fatalf("catalogue snapshot count: %d", group.Count)
		}
		found = true
	}
	if !found || len(catalogue.Warnings) != 0 {
		t.Fatalf("namespace-only key did not discover its group: %s", catalogueJSON)
	}
	// A destination identity does not adopt the source ID. The authorized key and
	// explicit source group control reads, just as they do for normal PBS clients.
	sourceID := cfg.HostID
	cfg.HostID = "android-restore-destination"
	targetConfig, _ := json.Marshal(cfg)
	target, err := NewClient(string(targetConfig))
	if err != nil {
		t.Fatal(err)
	}
	destination := &fsDocuments{root: t.TempDir()}
	if err := target.Restore("backup", namespace, sourceID, response.Data[2].Time, destination, nil); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(destination.full(name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("restore mismatch for %s (%v)", name, err)
		}
	}
	if _, err := os.Stat(destination.full("Documents/delete.txt")); !os.IsNotExist(err) {
		t.Fatal("deleted file appeared in latest restore")
	}
	original := &fsDocuments{root: t.TempDir()}
	if err := target.Restore("backup", namespace, sourceID, response.Data[0].Time, original, nil); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(original.full("Documents/change.txt"))
	if err != nil || string(old) != "version one" {
		t.Fatal("old snapshot did not retain original content")
	}
	if _, err := os.Stat(original.full("Documents/delete.txt")); err != nil {
		t.Fatal("old snapshot lost a later-deleted file")
	}
	page, err := target.ListArchive("backup", namespace, sourceID, response.Data[2].Time, "Documents", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Total   int           `json:"total"`
		Entries []archiveItem `json:"entries"`
	}
	if err := json.Unmarshal([]byte(page), &listing); err != nil || listing.Total != 4 || len(listing.Entries) != 2 {
		t.Fatalf("unexpected archive page: %s (%v)", page, err)
	}
	for _, mode := range []string{"skip", "replace", "rename"} {
		dest := &fsDocuments{root: t.TempDir()}
		if err := os.MkdirAll(dest.full("Documents"), 0700); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"Documents/change.txt": "other bytes", "Documents/unrelated.txt": "keep this"} {
			if err := os.WriteFile(dest.full(name), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := target.RestoreSelected("backup", namespace, sourceID, response.Data[2].Time, `["Documents/change.txt","Documents/new.txt"]`, mode, dest, nil); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(dest.full("Documents/change.txt"))
		want := "other bytes"
		if mode == "replace" {
			want = "version two"
		}
		if err != nil || string(got) != want {
			t.Fatalf("%s conflict: %q (%v)", mode, got, err)
		}
		if mode == "rename" {
			got, err = os.ReadFile(dest.full("Documents/change (restored 1).txt"))
			if err != nil || string(got) != "version two" {
				t.Fatalf("rename: %q (%v)", got, err)
			}
		}
		got, err = os.ReadFile(dest.full("Documents/new.txt"))
		if err != nil || string(got) != "added file" {
			t.Fatalf("%s missing: %q (%v)", mode, got, err)
		}
		got, err = os.ReadFile(dest.full("Documents/unrelated.txt"))
		if err != nil || string(got) != "keep this" {
			t.Fatal("unrelated file changed")
		}
		if _, err := os.Stat(dest.full("Documents/keep.txt")); !os.IsNotExist(err) {
			t.Fatal("unselected file appeared")
		}
	}
	t.Log("Automatic enrollment, named jobs, three snapshots, incremental transfer, historical restore, directory browsing, selective restore in all conflict modes, and separate destination identity passed")
}

type uploadSpy struct{ bytes int64 }

func (s *uploadSpy) Update(stage string, files, bytes int64) {
	for {
		previous := atomic.LoadInt64(&s.bytes)
		if bytes <= previous || atomic.CompareAndSwapInt64(&s.bytes, previous, bytes) {
			return
		}
	}
}
