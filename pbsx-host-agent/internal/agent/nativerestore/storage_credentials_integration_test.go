//go:build windows

package nativerestore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
)

// Requires an already enrolled disposable host and a protected JSON credential
// file containing a temporary ordinary PBS token with backup/read access.
func TestCrossHostStorageCredentialsLive(t *testing.T) {
	if os.Getenv("PBSX_STORAGE_CREDENTIAL_INTEGRATION") != "1" {
		t.Skip("requires opted-in test host")
	}
	file := os.Getenv("PBSX_TEST_STORAGE_CREDENTIAL_FILE")
	storeName := os.Getenv("PBSX_WINDOWS_TEST_DATASTORE")
	if file == "" || storeName == "" {
		t.Fatal("explicit credential file and test datastore required")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var credentials agent.StorageCredentials
	if json.Unmarshal(raw, &credentials) != nil || credentials.Validate() != nil {
		t.Fatal("invalid test credential file")
	}
	previous := agent.StorageCredentials{}
	mode, _ := registry.GetEntry(registry.CONFIG, "StorageCredentialMode", false)
	if mode != nil && mode.Value == "custom" {
		entry, err := registry.GetEntry(registry.AUTH, "StorageCredentials", true)
		if err != nil || entry == nil || json.Unmarshal([]byte(entry.Value), &previous) != nil {
			t.Fatal("cannot save previous storage credentials")
		}
	}
	t.Cleanup(func() {
		if err := agent.SetStorageCredentials(previous); err != nil {
			t.Errorf("restore previous credentials: %v", err)
		}
	})
	host, err := agent.AgentHostID()
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.SetStorageCredentials(credentials); err != nil {
		t.Fatal(err)
	}
	// This API requires Host.Agent; the datastore-only key must not be used here.
	if err := agent.PBSAPIRequest(t.Context(), "GET", "/admin/host/"+host, nil, nil); err != nil {
		t.Fatalf("management credential changed: %v", err)
	}
	baseURL, token, tlsConfig, err := agent.PBSConnectionInfo()
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	payload := []byte("restore across different host identities using normal PBS credentials\n")
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	accessor := agentfs.NewNativeAccessor("credential-fixture", "standard", snapshots.Snapshot{Path: source, SourcePath: source, Direct: true})
	defer accessor.Close()
	foreign := "pbsx-storage-credential-fixture"
	backupTime := time.Now().Unix()
	store := backupproxy.NewPBSStore(backupproxy.PBSConfig{BaseURL: baseURL, Datastore: storeName, AuthToken: token, TLSConfig: tlsConfig}, buzhash.DefaultConfig(), true)
	_, err = backupproxy.NewServer(backupproxy.NewLocalClient(accessor), store).RunBackupWithMode(t.Context(), ".", backupproxy.BackupConfig{
		BackupType: datastore.BackupHost, BackupID: foreign, BackupTime: backupTime, ChunkConfig: buzhash.DefaultConfig(), DetectionMode: backupproxy.DetectionData, Compress: true, StrictMetadata: true,
	})
	if err != nil {
		t.Fatalf("create independent source backup: %v", err)
	}
	t.Logf("foreign source group=%s snapshot=%d destination host=%s", foreign, backupTime, host)
	job := Job{ID: "credential-restore", HostID: host, BackupID: foreign, Datastore: storeName, BackupTime: backupTime, Destination: filepath.Join(t.TempDir(), "restore"), Mode: "noattr", Sources: []string{"/"}}
	if err := agent.SetStorageCredentials(agent.StorageCredentials{}); err != nil {
		t.Fatal(err)
	}
	err = Run(t.Context(), job)
	if err == nil || !strings.Contains(err.Error(), "bound to host") {
		t.Fatalf("automatic key should reject foreign group: %v", err)
	}
	if err := agent.SetStorageCredentials(credentials); err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), job); err != nil {
		t.Fatalf("ordinary PBS key failed cross-host restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(job.Destination, "payload.txt"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("cross-host restored content mismatch")
	}
	credentials.Secret = "deliberately-invalid-test-secret"
	if err := agent.SetStorageCredentials(credentials); err != nil {
		t.Fatal(err)
	}
	job.Destination = filepath.Join(t.TempDir(), "denied")
	if err := Run(t.Context(), job); err == nil {
		t.Fatal("invalid custom key was accepted")
	}
	if err := agent.PBSAPIRequest(t.Context(), "GET", "/admin/host/"+host, nil, nil); err != nil {
		t.Fatal("bad storage key broke host management")
	}
}
