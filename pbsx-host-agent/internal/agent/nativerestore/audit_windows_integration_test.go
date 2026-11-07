//go:build windows

package nativerestore

import (
	"bytes"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Only a caller-provisioned disposable namespace is ever written. Credentials
// stay in the enrolled test VM's existing protected registry.
func TestRestoreAuditCustomBackupIDsLive(t *testing.T) {
	if os.Getenv("PBSX_RESTORE_AUDIT_INTEGRATION") != "1" {
		t.Skip("requires disposable PBS-X test VM/namespace")
	}
	ns, store := os.Getenv("PBSX_WINDOWS_TEST_NAMESPACE"), os.Getenv("PBSX_WINDOWS_TEST_DATASTORE")
	if !strings.HasPrefix(ns, "restore-audit-") || store == "" {
		t.Fatal("explicit disposable namespace/datastore required")
	}
	host, err := agent.AgentHostID()
	if err != nil {
		t.Fatal(err)
	}
	base, token, tlsConfig, err := agent.PBSConnectionInfo()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("RIGHT")
	source := t.TempDir()
	if err = os.WriteFile(filepath.Join(source, "payload.txt"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "_photos", strings.Repeat("a", 100)} {
		t.Run(id, func(t *testing.T) {
			accessor := agentfs.NewNativeAccessor("restore-audit", "standard", snapshots.Snapshot{Path: source, SourcePath: source, Direct: true})
			defer accessor.Close()
			stamp := time.Now().Unix()
			cfg := backupproxy.PBSConfig{BaseURL: base, Datastore: store, Namespace: ns, AuthToken: token, TLSConfig: tlsConfig}
			_, err := backupproxy.NewServer(backupproxy.NewLocalClient(accessor), backupproxy.NewPBSStore(cfg, buzhash.DefaultConfig(), true)).RunBackupWithMode(t.Context(), ".", backupproxy.BackupConfig{BackupType: datastore.BackupHost, BackupID: id, Namespace: ns, BackupTime: stamp, ChunkConfig: buzhash.DefaultConfig(), DetectionMode: backupproxy.DetectionData, Compress: true, StrictMetadata: true})
			if err != nil {
				t.Fatal(err)
			}
			dest := t.TempDir()
			if err = os.WriteFile(filepath.Join(dest, "payload.txt"), []byte("WRONG"), 0600); err != nil {
				t.Fatal(err)
			}
			job := Job{ID: "restore-audit", HostID: host, BackupID: id, Datastore: store, Namespace: ns, BackupTime: stamp, Destination: dest, Mode: "noattr", Sources: []string{"/"}}
			if err = Run(t.Context(), job); err != nil {
				t.Fatal("native custom-ID restore", err)
			}
			got, err := os.ReadFile(filepath.Join(dest, "payload.txt"))
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatal("same-sized destination was not restored", err)
			}
			job.Mode = "normal"
			if err = Run(t.Context(), job); err != nil {
				t.Fatal("normal metadata restore", err)
			}
		})
	}
}
