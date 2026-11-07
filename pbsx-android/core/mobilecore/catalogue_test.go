package mobilecore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func catalogueClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	pin := sha256.Sum256(server.Certificate().Raw)
	config, _ := json.Marshal(configuration{Server: server.URL, HostID: "this-phone", Fingerprint: hex.EncodeToString(pin[:]), AuthID: "control@pbs!device", Secret: "control-secret", StorageAuthID: "reader@pbs!shared", StorageSecret: "storage-secret"})
	c, err := NewClient(string(config))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCatalogueDiscoversOtherHostsAndNamespacesWithStorageKey(t *testing.T) {
	calls := map[string]int{}
	var callsMu sync.Mutex
	c := catalogueClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PBSAPIToken=reader@pbs!shared:storage-secret" {
			t.Error("discovery used the control credential")
			http.Error(w, "wrong credential", 401)
			return
		}
		callsMu.Lock()
		calls[r.URL.RequestURI()]++
		callsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api2/json/admin/datastore":
			fmt.Fprint(w, `{"data":[{"store":"backup"},{"store":"family"},{"store":"backup"}]}`)
		case "/api2/json/admin/datastore/backup/namespace":
			fmt.Fprint(w, `{"data":[{"ns":"phones"},{"ns":""},{"ns":"phones"},{"ns":"phones/kids"}]}`)
		case "/api2/json/admin/datastore/family/namespace":
			fmt.Fprint(w, `{"data":[{"ns":""}]}`)
		case "/api2/json/admin/datastore/backup/groups":
			switch r.URL.Query().Get("ns") {
			case "":
				fmt.Fprint(w, `{"data":[{"backup-type":"host","backup-id":"other-laptop","last-backup":20,"backup-count":3},{"backup-type":"vm","backup-id":"100","last-backup":10,"backup-count":2}]}`)
			case "phones":
				fmt.Fprint(w, `{"data":[{"backup-type":"host","backup-id":"this-phone","last-backup":30,"backup-count":4}]}`)
			case "phones/kids":
				fmt.Fprint(w, `{"data":[{"backup-type":"host","backup-id":"this-phone","last-backup":40,"backup-count":1}]}`)
			default:
				t.Error("unexpected namespace")
				http.NotFound(w, r)
			}
		case "/api2/json/admin/datastore/family/groups":
			fmt.Fprint(w, `{"data":[{"backup-type":"ct","backup-id":"101","last-backup":50,"backup-count":1}]}`)
		default:
			t.Error("unexpected endpoint: " + r.URL.Path)
			http.NotFound(w, r)
		}
	})
	raw, err := c.ListAccessibleBackups()
	if err != nil {
		t.Fatal(err)
	}
	var catalogue backupCatalogue
	if err := json.Unmarshal([]byte(raw), &catalogue); err != nil {
		t.Fatal(err)
	}
	if len(catalogue.Groups) != 5 || len(catalogue.Warnings) != 0 {
		t.Fatalf("unexpected catalogue: %s", raw)
	}
	if catalogue.Groups[0].Type != "ct" || catalogue.Groups[0].Store != "family" || catalogue.Groups[1].Namespace != "phones/kids" {
		t.Fatal("catalogue is not sorted newest first across locations")
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls["/api2/json/admin/datastore/backup/groups"] != 1 || calls["/api2/json/admin/datastore/backup/groups?ns=phones"] != 1 {
		t.Fatal("locations were listed more than once")
	}
	for path := range calls {
		if strings.Contains(path, "backup-id=") {
			t.Fatal("catalogue filtered discovery by local host identity")
		}
	}
}

func TestCatalogueHandlesNamespaceOnlyAccessAndPartialFailures(t *testing.T) {
	c := catalogueClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/admin/datastore":
			fmt.Fprint(w, `{"data":[{"store":"backup"},{"store":"offline"}]}`)
		case "/api2/json/admin/datastore/backup/namespace":
			fmt.Fprint(w, `{"data":[{"ns":"phones"}]}`)
		case "/api2/json/admin/datastore/backup/groups":
			if r.URL.Query().Get("ns") == "phones" {
				fmt.Fprint(w, `{"data":[{"backup-type":"host","backup-id":"phone","last-backup":10}]}`)
			} else {
				http.Error(w, "denied", 403)
			}
		default:
			http.Error(w, "offline", 503)
		}
	})
	raw, err := c.ListAccessibleBackups()
	if err != nil {
		t.Fatal(err)
	}
	var result backupCatalogue
	json.Unmarshal([]byte(raw), &result)
	if len(result.Groups) != 1 || result.Groups[0].Namespace != "phones" || len(result.Warnings) == 0 {
		t.Fatal(raw)
	}
}

func TestCatalogueRejectsBadMetadataAndExpiredCredentials(t *testing.T) {
	for _, item := range []struct {
		name, body string
		status     int
	}{
		{"expired", "", 401}, {"missing envelope", `{}`, 200}, {"wrong envelope", `{"data":{}}`, 200}, {"unsafe datastore", `{"data":[{"store":"../escape"}]}`, 200},
	} {
		t.Run(item.name, func(t *testing.T) {
			c := catalogueClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(item.status); fmt.Fprint(w, item.body) })
			if _, err := c.ListAccessibleBackups(); err == nil {
				t.Fatal("bad catalogue accepted")
			}
		})
	}
}

func TestCatalogueCancellationStopsDiscovery(t *testing.T) {
	started := make(chan struct{})
	c := catalogueClient(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() })
	result := make(chan error, 1)
	go func() { _, err := c.ListAccessibleBackups(); result <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	c.Cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled catalogue succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("catalogue did not cancel")
	}
}

func TestGroupSnapshotRequestsPreserveBackupType(t *testing.T) {
	c := catalogueClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("backup-type") != "ct" || r.URL.Query().Get("backup-id") != "101" || r.URL.Query().Get("ns") != "phones" {
			t.Error("group identity lost in snapshot request")
		}
		fmt.Fprint(w, `{"data":[]}`)
	})
	if _, err := c.ListGroupSnapshots("backup", "phones", "ct", "101"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListGroupSnapshots("backup", "", "host", "../escape"); err == nil {
		t.Fatal("invalid group accepted")
	}
}

func TestBackupGroupIDsUsePBSRules(t *testing.T) {
	for _, id := range []string{"a", "_photos", strings.Repeat("a", 100)} {
		if err := validateGroup("host", id); err != nil {
			t.Fatal(id, err)
		}
	}
	for _, id := range []string{"", ".", "..", "a/b", "a\\b", "has space", "a\nheader", "é"} {
		if validateGroup("host", id) == nil {
			t.Fatalf("invalid backup ID accepted: %q", id)
		}
	}
}
