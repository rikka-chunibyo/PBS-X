package mobilecore

import "testing"

func TestPreviousSnapshotIsCompleteSameGroupAndOlder(t *testing.T) {
	raw := []byte(`{"data":[
{"backup-type":"host","backup-id":"phone","backup-time":10,"size":20,"files":[{"filename":"root.ppxar.didx"}]},
{"backup-type":"host","backup-id":"phone","backup-time":30,"size":null,"files":[{"filename":"root.ppxar.didx"}]},
{"backup-type":"host","backup-id":"other","backup-time":40,"size":20,"files":[{"filename":"root.ppxar.didx"}]},
{"backup-type":"vm","backup-id":"phone","backup-time":41,"size":20,"files":[{"filename":"root.ppxar.didx"}]},
{"backup-type":"host","backup-id":"phone","backup-time":50,"size":20,"files":[{"filename":"root.ppxar.didx"}]},
{"backup-type":"host","backup-id":"phone","backup-time":20,"size":20,"files":[{"filename":"root.ppxar.didx"}]}
]}`)
	p, err := previousSnapshot(raw, "devices/phone", "phone", 50)
	if err != nil || p == nil || p.BackupTime != 20 || p.Namespace != "devices/phone" || p.BackupID != "phone" {
		t.Fatalf("previous = %+v, err = %v", p, err)
	}
	if _, err := previousSnapshot([]byte(`{"data":"invalid"}`), "", "phone", 50); err == nil {
		t.Fatal("malformed previous snapshots were accepted")
	}
	p, err = previousSnapshot([]byte(`{"data":[]}`), "", "phone", 50)
	if err != nil || p != nil {
		t.Fatalf("first backup should have no baseline: %+v, %v", p, err)
	}
}
