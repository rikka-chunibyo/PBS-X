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

The Linux path has completed an end-to-end backup/restore round trip against PBS-X, including symlinks and sparse files. Windows builds and Windows-specific packages cross-compile, but the current native Windows path still needs a live end-to-end validation pass.
