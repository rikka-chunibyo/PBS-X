package nativebackup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-x/pbsx-host-agent/internal/testutil/pbsfixture"
)

func TestSnapshotCandidatesExcludeUnfinishedAndUnreadableManifests(t *testing.T) {
	job := Job{ID: "fixture-job", HostID: pbsfixture.HostID, Datastore: "fixture", Namespace: "device/photos"}
	item := func(stamp int64, size any, filenames ...string) map[string]any {
		files := make([]map[string]string, 0, len(filenames))
		for _, name := range filenames {
			files = append(files, map[string]string{"filename": name})
		}
		return map[string]any{"backup-type": "host", "backup-id": job.HostID, "backup-time": stamp, "size": size, "files": files}
	}
	foreignHost := item(50, 100, "index.json.blob")
	foreignHost["backup-id"] = "different-host"
	foreignType := item(60, 100, "index.json.blob")
	foreignType["backup-type"] = "vm"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ns") != job.Namespace || r.URL.Query().Get("backup-id") != job.HostID || r.URL.Query().Get("backup-type") != "host" {
			t.Errorf("incorrect snapshot filter: %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
			item(10, 100, "index.json.blob", "root.pxar.didx"),
			item(20, nil, "root.pxar.didx"),                    // Interrupted, no manifest.
			item(30, nil, "index.json.blob", "root.pxar.didx"), // Manifest exists but is unreadable.
			item(40, 100, "root.pxar.didx"),
			foreignHost, foreignType, item(0, 100, "index.json.blob"),
		}})
	}))
	defer server.Close()
	tls := server.Client().Transport.(*http.Transport).TLSClientConfig
	times, err := listSnapshotTimes(t.Context(), server.URL+"/api2/json", job, "fixture:dummy", tls)
	if err != nil || !reflect.DeepEqual(times, []int64{10}) {
		t.Fatalf("completed candidates = %v, error = %v", times, err)
	}
	previous, err := findPreviousFromTimes(job, times, nil, 100)
	if err != nil || previous == nil || previous.BackupTime != 10 {
		t.Fatalf("previous snapshot = %+v, error = %v", previous, err)
	}
	// An unfinished run's timestamp cannot trigger the completed-run shortcut.
	result, err := recoverCompletedBackup(t.Context(), backupproxy.PBSConfig{}, job, times, 20)
	if err != nil || result != nil {
		t.Fatalf("unfinished backup recovered as complete: %+v %v", result, err)
	}
}

func TestRecoverCompletedBackupVerifiesSnapshot(t *testing.T) {
	for _, split := range []bool{false, true} {
		s := pbsfixture.New(t, split)
		cfg := s.Start(t)
		job := Job{ID: "fixture-job", HostID: pbsfixture.HostID, Datastore: cfg.Datastore}
		result, err := recoverCompletedBackup(t.Context(), cfg, job, []int64{pbsfixture.Stamp}, pbsfixture.Stamp)
		if err != nil || result == nil || result.BackupTime != pbsfixture.Stamp {
			t.Fatalf("completed backup recovery: %+v %v", result, err)
		}
		if s.Requests("index.json.blob") != 1 {
			t.Fatal("recovery did not read completed manifest")
		}
	}
}

func TestRecoveryRejectsIncompleteOrDamagedSnapshot(t *testing.T) {
	for _, damage := range []string{"missing manifest", "wrong identity", "missing index", "corrupt index"} {
		t.Run(damage, func(t *testing.T) {
			s := pbsfixture.New(t, true)
			switch damage {
			case "missing manifest":
				delete(s.Files, "index.json.blob")
			case "wrong identity":
				s.Manifest.BackupID = "different-host"
				s.WriteManifest(t)
			case "missing index":
				delete(s.Files, "root.ppxar.didx")
			case "corrupt index":
				index := s.Files["root.ppxar.didx"]
				index[len(index)-1] ^= 1
			}
			cfg := s.Start(t)
			job := Job{ID: "fixture-job", HostID: pbsfixture.HostID, Datastore: cfg.Datastore}
			// Force a candidate even when its manifest disappeared after listing.
			result, err := recoverCompletedBackup(t.Context(), cfg, job, []int64{pbsfixture.Stamp}, pbsfixture.Stamp)
			if result != nil || err == nil || !strings.Contains(err.Error(), "verify recovered backup") {
				t.Fatalf("damaged snapshot acknowledged: %+v %v", result, err)
			}
		})
	}
}
