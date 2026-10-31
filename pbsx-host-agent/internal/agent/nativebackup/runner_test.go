package nativebackup

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pbs-plus/pxar/backupproxy"
)

func TestPlanSourcesNormalizesExcludes(t *testing.T) {
	source := t.TempDir()
	outside := t.TempDir()
	plans, err := planSources(
		[]string{"  " + source + "  "},
		[]string{
			filepath.Join(source, "cache"),
			"logs",
			"logs",
			filepath.Join(outside, "unrelated"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1", len(plans))
	}
	if plans[0].Source != source {
		t.Fatalf("source = %q, want %q", plans[0].Source, source)
	}
	if want := []string{"cache", "logs"}; !reflect.DeepEqual(plans[0].Excludes, want) {
		t.Fatalf("excludes = %#v, want %#v", plans[0].Excludes, want)
	}
}

func TestPlanSourcesRejectsUnsafeRelativeExclude(t *testing.T) {
	_, err := planSources([]string{t.TempDir()}, []string{"../escape"})
	if err == nil {
		t.Fatal("parent traversal exclusion was accepted")
	}
}

func TestPlanSourcesCanExcludeOneOfSeveralRoots(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	plans, err := planSources([]string{first, second}, []string{first})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Source != second {
		t.Fatalf("plans = %#v, want only %q", plans, second)
	}
}

func TestPlanSourcesRejectsAllExcludedRoots(t *testing.T) {
	source := t.TempDir()
	_, err := planSources([]string{source}, []string{source})
	if err == nil {
		t.Fatal("all-excluded source set was accepted")
	}
}

func TestPBSAPIBase(t *testing.T) {
	tests := []struct {
		name   string
		server string
		want   string
	}{
		{name: "default port", server: "pbs.example", want: "https://pbs.example:8007/api2/json"},
		{name: "explicit port", server: "https://pbs.example:8443/ignored?q=1", want: "https://pbs.example:8443/api2/json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := pbsAPIBase(test.server)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("pbsAPIBase(%q) = %q, want %q", test.server, got, test.want)
			}
		})
	}
	if _, err := pbsAPIBase("http://pbs.example"); err == nil {
		t.Fatal("plain HTTP server URL was accepted")
	}
}

func TestFindPreviousFromTimes(t *testing.T) {
	job := Job{HostID: "host-1", Namespace: "ns"}
	previous, err := findPreviousFromTimes(job, []int64{30, 10, 20, 40}, nil, 35)
	if err != nil {
		t.Fatal(err)
	}
	if previous == nil || previous.BackupTime != 30 || previous.BackupID != job.HostID || previous.Namespace != job.Namespace {
		t.Fatalf("unexpected previous backup: %#v", previous)
	}

	wantErr := errors.New("snapshot listing failed")
	if _, err := findPreviousFromTimes(job, nil, wantErr, 35); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestDetectionModeFallsBackWithoutPrevious(t *testing.T) {
	tests := []struct {
		mode        string
		hasPrevious bool
		want        backupproxy.DetectionMode
	}{
		{mode: "legacy", want: backupproxy.DetectionLegacy},
		{mode: "data", hasPrevious: true, want: backupproxy.DetectionData},
		{mode: "metadata", want: backupproxy.DetectionData},
		{mode: "metadata", hasPrevious: true, want: backupproxy.DetectionMetadata},
		{mode: "unknown", hasPrevious: true, want: backupproxy.DetectionMetadata},
	}
	for _, test := range tests {
		if got := detectionMode(test.mode, test.hasPrevious); got != test.want {
			t.Errorf("detectionMode(%q, %t) = %v, want %v", test.mode, test.hasPrevious, got, test.want)
		}
	}
}
