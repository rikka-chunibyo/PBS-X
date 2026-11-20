package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/pbs-plus/pxar/datastore"
)

func TestEncryptedJobNotesNeverLeaveDevice(t *testing.T) {
	var requests atomic.Int32
	c := catalogueClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte(`{"data":null}`))
	})
	var err error
	c.crypt, err = datastore.NewCryptConfig([32]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	// A selected key is the effective archive encryption policy. This must not
	// depend on a separate configuration flag or on whether the job resumed.
	if err = c.saveJobNotes(context.Background(), "backup", "phone", "opaque-backup-id", 123, "private-job-id", "Private medical documents"); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("encrypted job metadata was sent to the server")
	}
}

func TestPlainJobNotesPreserveScopeAndStorageCredential(t *testing.T) {
	for _, namespace := range []string{"", "devices/phone"} {
		t.Run("namespace="+namespace, func(t *testing.T) {
			var requests atomic.Int32
			c := catalogueClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != "PUT" || r.URL.Path != "/api2/json/admin/datastore/backup/notes" {
					t.Error("unexpected notes request")
				}
				if r.Header.Get("Authorization") != "PBSAPIToken=reader@pbs!shared:storage-secret" {
					t.Error("notes used the control credential")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["backup-type"] != "host" || body["backup-id"] != "photos" || body["backup-time"] != float64(123) || body["notes"] != "PBS-X Android: Photos 猫\nJob ID: job-id" {
					t.Error("job identity or label changed")
				}
				if value, present := body["ns"]; (namespace == "" && present) || (namespace != "" && value != namespace) {
					t.Error("notes namespace changed")
				}
				w.Write([]byte(`{"data":null}`))
			})
			if err := c.saveJobNotes(context.Background(), "backup", namespace, "photos", 123, "job-id", "Photos 猫"); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 1 {
				t.Fatal("missing or duplicate notes request")
			}
		})
	}
}

func TestJobNotesWithoutJobDoNotSendRequest(t *testing.T) {
	c := catalogueClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("backup without a job sent snapshot notes")
	})
	if err := c.saveJobNotes(context.Background(), "backup", "", "photos", 123, "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestJobNotesReportPlainBackupPermissionFailure(t *testing.T) {
	c := catalogueClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	err := c.saveJobNotes(context.Background(), "backup", "", "photos", 123, "job-id", "Photos")
	var response *responseError
	if !errors.As(err, &response) || response.status != http.StatusForbidden {
		t.Fatalf("notes failure was hidden: %v", err)
	}
}
