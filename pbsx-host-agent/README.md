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

The Linux path has completed an end-to-end backup/restore round trip against PBS-X, including symlinks and sparse files. The Windows agent builds and its full Go module suite runs natively on Windows. Windows Server 2025 has completed a VSS-backed PBS-X backup/restore round trip with matching file hashes, Unicode names, exclusions, alternate data streams, sparse allocation ranges, and a protected DACL. See [the validation report](../docs/windows-native-validation.md) for scope and limitations.

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
