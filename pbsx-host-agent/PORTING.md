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

The internal Go import path has been changed to `github.com/pbs-x/pbsx-host-agent`, and obvious product/service names have been changed to PBS-X. Protocol URLs, certificate bootstrap, configuration storage and several `PBS Plus` protocol identifiers still need to be adapted to PBS-X.

Do not treat this binary as integrated yet. The intended integration order is:

1. make PBS-X Host ID the canonical agent identity;
2. replace the PBS Plus sidecar bootstrap endpoint with a PBS-X native enrollment endpoint;
3. bind issued credentials to `/host/<host-id>` and the `Host.Agent` privilege;
4. connect the aRPC agent manager to the PBS-X internal host service;
5. wire backup jobs to request a snapshot/source from the agent;
6. mount the agent filesystem on the PBS-X server;
7. invoke `proxmox-backup-client` with `--backup-type host --backup-id <host-id>`;
8. wire restore and file browsing into the Host UI.

The current environment has Go 1.23.2 while upstream PBS Plus declares Go 1.27.0, and outbound module/toolchain downloads are blocked. Build cleanup is therefore deferred until the source is tested in a current Go environment.
