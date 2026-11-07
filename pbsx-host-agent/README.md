# PBS-X host agent

This directory is the in-progress Windows/Linux data-plane agent for PBS-X.

It is being ported from the MIT-licensed PBS Plus agent because that project already contains mature implementations for VSS and Linux snapshot providers, filesystem metadata/ACL handling, aRPC transport, mTLS, restore logic and service lifecycle. See `LICENSE-PBS-PLUS` and `PORTING.md`.

The small `../pbsx-agent` program remains the original control-plane prototype. It talks directly to the PBS-X Host API and proves host-scoped API-token authentication, heartbeat and inventory reporting. The agent in this directory now supersedes it for native enrollment, job polling, backup upload and restore.

The target first-generation data path is:

```text
PBS-X Agent
  -> VSS/ZFS/Btrfs/LVM snapshot
  -> local agentfs/PXAR scan
  -> native PBS chunk and manifest upload
  -> host/<host-id>/<snapshot>
```

The backup job ID is never used as the PBS backup group ID.

Automatic device API keys remain the default. Setup can also use an existing
PBS storage API key, and restores can target another enrolled host without
changing host IDs. See [storage credentials and restore permissions](../docs/host-storage-credentials.md).

The Linux path has completed an end-to-end backup/restore round trip against PBS-X, including symlinks and sparse files. The Windows agent builds and its full Go module suite runs natively on Windows. Windows Server 2025 has completed a VSS-backed PBS-X backup/restore round trip with matching file hashes, Unicode names, exclusions, alternate data streams, sparse allocation ranges, and a protected DACL. See [the validation report](../docs/windows-native-validation.md) for scope and limitations.

Desktop restores confine filesystem operations to the selected destination with
directory handles, including Windows metadata and alternate data streams.
Both normal and content-only modes write archived file contents; size and
mtime alone cannot establish that an existing file matches. Temporary-file
cleanup removes only files allocated by that restore. Crash leftovers are
retained rather than deleting arbitrary `.pxar-restore-*` names. See
[restore audit fixes](../docs/restore-audit-fixes.md) for regression coverage.

## Windows VSS integration test

The snapshot package includes an opt-in test that creates a real VSS shadow
copy, verifies that its contents remain point-in-time consistent after the live
file changes, and removes the shadow copy. Run it from an elevated PowerShell
session on a disposable Windows test host:

```powershell
$env:PBSX_WINDOWS_VSS_INTEGRATION = "1"
go test -v -count=1 -run '^TestNTFSSnapshotLive$' ./internal/agent/snapshots
```

The test is skipped during ordinary unit-test runs because VSS requires an
elevated Windows host and temporarily creates system snapshot state.
When running multiple packages with live VSS tests enabled, pass `-p 1` to
`go test`: shadow-copy creation can be vetoed when separate test processes
request snapshots of the same volume concurrently.

For broader live backup/restore coverage on a disposable enrolled Windows host,
explicitly select the test datastore and opt in:

```powershell
$env:PBSX_WINDOWS_PBS_INTEGRATION = "1"
$env:PBSX_WINDOWS_TEST_DATASTORE = "backup"
$env:PBSX_WINDOWS_TEST_NAMESPACE = "disposable-test-namespace"
go test -v -count=1 -timeout=15m -run 'TestWindowsPBS' ./internal/agent/nativebackup
```

These tests use the selected storage credentials and write real PBS snapshots
inside the optional namespace above (create it and grant access first).
Temporary source/restore files are cleaned up; committed snapshots remain for
inspection. The suite covers multiple sources, hardlinks, symlinks, long paths,
ADS, attributes, locked files, metadata-mode changes, selected-file restore, and
explicit oversized-ADS rejection. Both environment flags are required.
