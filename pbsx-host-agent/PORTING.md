# PBS-X host agent port

This tree is the first PBS-X data-plane port and is derived from the PBS Plus agent implementation.

PBS Plus is MIT licensed. `LICENSE-PBS-PLUS` contains the required upstream notice and license. Keep that notice with any substantially derived source.

## Why this exists

The earlier `pbsx-agent/` program proves the PBS-X Host ACL, API-token authentication, heartbeat and inventory model. It is deliberately small. It does not try to recreate years of Windows/Linux backup-agent work.

This tree brings in the transitive source packages used by the PBS Plus agent so PBS-X can adapt the mature pieces instead:

- persistent agent lifecycle;
- Windows service support;
- agent filesystem RPC;
- Windows ACL and Unix ACL/xattr handling;
- VSS, ZFS, Btrfs and LVM snapshot providers;
- aRPC QUIC/TCP transport;
- mTLS lifecycle;
- restore and verification helpers;
- drive/volume status;
- reconnect and updater logic.

Only packages reachable from `cmd/agent` were imported. PBS Plus server UI, database backup features, tape migration, Kubernetes operator and other unrelated components are not part of this porting tree.

## Current port state

The internal Go import path is `github.com/pbs-x/pbsx-host-agent`, configuration and service state use PBS-X paths, and PBS-X Host ID is the canonical agent and backup-group identity. The native server issues one-time enrollment codes and narrowly scoped upload credentials, while the agent polls PBS-X for durable backup and restore commands.

Bulk backup data no longer follows the imported PBS Plus FUSE design. The agent snapshots and scans locally, builds PXAR/chunk streams, and uploads normal PBS manifests and indexes directly into `host/<host-id>/<snapshot-time>`. Restore reads those archives directly from PBS and writes them on the registered host. Legacy aRPC remains optional plumbing for future control features rather than the backup data path.

The Linux path has been exercised end to end against a PBS-X datastore, including enrollment, inventory, job dispatch, backup commit, restore, symlink fidelity and sparse-file preservation. Go 1.27 builds and tests the control and agent packages, the Windows agent and full module suite run natively on Windows, and the VSS provider has passed a live point-in-time consistency and cleanup test. Remaining integration work includes end-to-end Windows backup/restore validation against PBS-X, agent packaging/upgrades, Host UI file browsing, and later image/bare-metal support.
