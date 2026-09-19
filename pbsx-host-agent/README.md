# PBS-X host agent

This directory is the in-progress Windows/Linux data-plane agent for PBS-X.

It is being ported from the MIT-licensed PBS Plus agent because that project already contains mature implementations for VSS and Linux snapshot providers, filesystem metadata/ACL handling, aRPC transport, mTLS, restore logic and service lifecycle. See `LICENSE-PBS-PLUS` and `PORTING.md`.

The small `../pbsx-agent` program remains the currently testable control-plane prototype. It talks directly to the PBS-X Host API and proves host-scoped API-token authentication, heartbeat and inventory reporting. The code in this directory is intended to supersede that prototype once its bootstrap and aRPC endpoints are connected to the native PBS-X server.

The target first-generation data path is:

```text
PBS-X Agent
  -> VSS/ZFS/Btrfs/LVM snapshot
  -> agentfs/aRPC
  -> PBS-X host data-plane mount
  -> proxmox-backup-client
  -> host/<host-id>/<snapshot>
```

The backup job ID is never used as the PBS backup group ID.
