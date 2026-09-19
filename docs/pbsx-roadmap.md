# PBS-X roadmap

PBS-X is a fork of Proxmox Backup Server focused on first-class Windows and Linux host backups, while keeping the normal PBS datastore, snapshot, deduplication, verification, pruning, sync, tape, namespace and restore model.

The fork should stay close to upstream internally. Existing crate names, paths, configuration files and APIs should only be renamed when there is a technical reason to do so. New PBS-X components may use `pbsx-*` names. This keeps future upstream rebases manageable.

## Current implementation status

Implemented in the current PBS-X source checkpoint:

- ZFS RAID0/striped pool support in the API and web UI;
- native Host objects, `/host/<id>` ACLs and Host privileges;
- heartbeat/inventory status and the initial API-token control-plane agent;
- top-level Hosts navigation and per-host Summary, Backups, Disks/Volumes, Jobs and Agent views;
- native host backup-job configuration tied to PBS datastores/namespaces;
- snapshot listing that maps the registered host to normal PBS `host/<host-id>` snapshots;
- a server-side backup-client argument builder which enforces `--backup-type host --backup-id <host-id>`;
- an attributed PBS Plus-derived host-agent port containing the agent-side snapshot, ACL, aRPC, mTLS and restore foundations.

Not wired yet:

- PBS-X-native agent enrollment/mTLS certificate issuance;
- the server-side aRPC/FUSE data-plane service;
- scheduled/on-demand host job execution;
- restore and file browsing from the Host UI;
- image backups and bare-metal recovery.

The current environment cannot compile the Rust tree because Rust/Cargo are unavailable, and it cannot fetch rustup due blocked outbound DNS. The PBS Plus-derived agent declares Go 1.27 while the local toolchain is Go 1.23.2, so that port also needs build cleanup in a current toolchain environment. The small API-token control-plane agent still builds and tests locally.

## Product model

A Windows or bare-metal Linux machine is a first-class **Host** in PBS-X. A host is not a second storage system and host backups are not opaque files beside PBS backups.

The stable PBS backup group for a protected host is:

```text
host/<host-id>/<snapshot-time>
```

For example:

```text
host/gaming-pc/2026-09-19T06:00:00Z
host/dev-server/2026-09-19T06:00:00Z
```

Backup jobs belong to a host. The job ID must not become the PBS backup group ID. This is an intentional difference from PBS Plus and is important for the native PBS-X UI: `gaming-pc` owns its snapshot history regardless of whether the snapshot was produced by a nightly job, an on-demand job, or a future image-backup job.

The UI should expose Hosts as a top-level workload type with a detail view containing Summary, Backups, Disks/Volumes, Files, Jobs and Agent tabs. The Backups view should ultimately read the same PBS snapshots visible under Datastore -> Content.

## Initial goals

### ZFS RAID0 / striped pools

PBS currently exposes single-disk, mirror, RAID10 and RAIDZ layouts when creating a ZFS pool. PBS-X also exposes a RAID0 option.

For ZFS this means a striped pool with multiple top-level disks, not a literal `raidz0` vdev. The generated command is equivalent to:

```sh
zpool create ... <pool> /dev/disk1 /dev/disk2 /dev/disk3
```

RAID0 has no redundancy. Failure of any member disk can make the entire pool unavailable. The UI should state this clearly, but PBS-X should not block the configuration.

### Host and agent model

Windows and Linux systems are first-class PBS-X backup sources rather than external scripts that happen to upload archives.

The server-side foundation consists of:

- a host configuration object and `/host/<id>` ACL namespace;
- separate `Host.Audit`, `Host.Modify` and `Host.Agent` privileges;
- persistent agent status that is not mixed into static configuration;
- authenticated agent status/heartbeat;
- OS, architecture, agent version, capabilities, disks and filesystem inventory;
- host backup jobs whose target datastore and namespace are native PBS objects;
- a Hosts view and per-host detail UI;
- an agent control/data plane capable of snapshots, remote filesystem access, backup commands and restores.

The initial API-token heartbeat implementation is useful for the PBS-X ACL boundary. It is not the final data plane.

## PBS Plus reuse strategy

PBS Plus is a substantial implementation and should be used as source material rather than reimplementing the difficult platform-specific work from scratch. It is MIT licensed. Any code copied or substantially derived from it must keep the required copyright and MIT notice.

The most valuable pieces are:

- `internal/agent` service lifecycle and bootstrap concepts;
- `internal/agent/agentfs` remote filesystem enumeration and metadata handling;
- Windows ACL and Unix ACL/xattr handling;
- `internal/agent/snapshots`, especially VSS;
- drive/filesystem discovery;
- `internal/arpc` persistent QUIC control and on-demand TCP data streams;
- mTLS/certificate lifecycle;
- backup and restore command handling;
- agent update/reconnect logic;
- file browsing and restore primitives.

PBS Plus currently uses this data path:

```text
agent snapshot
    -> remote filesystem over aRPC
    -> FUSE mount on the PBS server
    -> proxmox-backup-client
    -> ordinary PBS host snapshot
```

That is a good first-generation PBS-X data path because it gives us normal PBS snapshots quickly and lets us reuse mature Windows/Linux agent work.

PBS-X should not copy the PBS Plus sidecar-product architecture wholesale. PBS-X owns the PBS server, API, ACL model and ExtJS UI directly. The Go data-plane service can remain a separate process where that makes sense, but it should act as an internal PBS-X component rather than exposing a second competing management product.

### Intentional differences from PBS Plus

1. **Stable host backup group**

   PBS Plus uses the backup job ID as the PBS `host/<id>` group. PBS-X uses the Host ID. Jobs target a host; they do not define the host identity.

2. **Native PBS-X API and UI**

   Host configuration, permissions, jobs, status and snapshot links should be exposed through PBS-X itself rather than an injected sidecar UI.

3. **Snapshot provider hardening**

   PBS Plus snapshot code is a starting point, not an unquestioned import. In particular, LVM snapshot sizing, ZFS/Btrfs source assumptions and fallback behavior need review.

4. **Long-term agent-side backup protocol**

   FUSE is transitional. The later target is to run PXAR creation, chunking and PBS backup-protocol upload on the agent so the server does not need to remotely walk every host filesystem.

## Windows backups

The target is more than copying `C:\Users` into an archive. PBS-X should support both file-level backups and image/system backups.

### File backups

The first implementation should use VSS to obtain a consistent filesystem view and expose that snapshot through the agent data plane. The PBS-X server can initially run `proxmox-backup-client` against the mounted remote filesystem and write a normal `host/<host-id>` snapshot.

Windows metadata support should include, where possible:

- NTFS ACLs;
- timestamps;
- reparse points;
- sparse files;
- alternate data streams where the PXAR representation can preserve them;
- exclusions;
- point-in-time VSS consistency.

### Image/system backups

A later mode should stream VSS-backed volume or disk data into fixed-index image archives. Metadata should describe GPT/MBR layout, EFI/MSR/recovery partitions and Windows volumes well enough for bare-metal recovery.

A recovery environment should eventually boot a machine, authenticate to PBS-X, select a host snapshot and reconstruct its disks.

## Linux backups

Linux uses the same Host, Job and datastore model as Windows.

For first-generation file backup, the agent should pick an appropriate consistent snapshot provider before exposing the filesystem to the PBS-X server:

- ZFS snapshot;
- Btrfs snapshot;
- LVM/LVM-thin snapshot;
- safe direct/fallback mode when a native snapshot provider is unavailable.

The PBS Plus implementations are references and should be hardened before being treated as production defaults. LVM snapshot sizing in particular must not use a fixed one-size-fits-all allocation.

Linux system/image backup can later stream snapshot block devices into PBS fixed-index images.

## Architecture generations

### Generation 1: native PBS UI, PBS Plus-derived data plane

```text
Windows/Linux Host
        |
        | PBS-X agent
        | VSS / ZFS / Btrfs / LVM
        v
remote snapshot filesystem
        |
        | aRPC + mTLS
        v
PBS-X host data-plane service
        |
        | FUSE
        v
proxmox-backup-client
        |
        | --backup-type host
        | --backup-id <host-id>
        v
PBS datastore
```

This already produces ordinary PBS snapshots and therefore reuses normal deduplication, chunk storage, verification, pruning, sync, tape and namespace behavior.

### Generation 2: agent-side PXAR/chunking

```text
snapshot
   -> agent filesystem scan
   -> PXAR writer
   -> chunk/hash/dedup negotiation
   -> PBS backup protocol
   -> datastore
```

This removes the remote-FUSE filesystem walk from the hot path while keeping the stored snapshots compatible with PBS.

### Generation 3: image backup and bare-metal recovery

File backup remains available, but hosts can also protect selected volumes/disks as image archives with enough layout metadata for full-machine recovery.

## Development phases

1. **PBS-X baseline**
   - Keep the upstream tree recognizable.
   - Add RAID0/striped ZFS pool creation.

2. **Native host foundation**
   - Host config and ACLs.
   - Agent authentication boundary.
   - Heartbeat, version, OS and inventory.
   - Host detail UI.
   - Native host backup-job configuration.
   - Stable `host/<host-id>` snapshot identity.

3. **PBS Plus-derived data plane**
   - Import/adapt aRPC, mTLS, agent filesystem and snapshot-provider pieces.
   - Preserve PBS Plus MIT attribution.
   - Connect agent identity to PBS-X Host identity.
   - Replace PBS Plus job-ID backup grouping with Host ID grouping.

4. **Linux file backup**
   - Agent snapshot -> remote filesystem -> server-side `proxmox-backup-client`.
   - Restore to the registered host.
   - ZFS/Btrfs/LVM/direct modes.

5. **Windows file backup**
   - Windows service integration.
   - VSS-backed filesystem backup.
   - Windows metadata and restore behavior.

6. **Agent-side PXAR/chunk upload**
   - Move filesystem walk/chunking off the PBS server.
   - Keep snapshot format and datastore behavior native to PBS.

7. **Linux image/system backup**
   - Snapshot block-device backup and restore.

8. **Windows image/system backup**
   - VSS volume imaging.
   - Disk-layout and boot metadata.
   - Multi-volume consistency.

9. **PBS-X recovery environment**
   - Bare-metal Linux and Windows restore.

## Immediate implementation boundary

The current implementation pass should establish:

- RAID0 support;
- Host API/ACL/status model;
- per-host detail navigation;
- host backup-job configuration tied to datastore/namespace;
- PBS Plus-derived agent/data-plane source imported under explicit MIT attribution;
- the interfaces needed for a job to eventually request an agent snapshot and invoke `proxmox-backup-client` with `--backup-type host --backup-id <host-id>`.

It is acceptable for build cleanup to follow once the source is in a proper PBS/Rust/Go build environment. The data model and storage identity should be correct before spending time polishing packaging.

## Later ideas

Possible additions after the core path works include changed-block tracking, application-consistent hooks, SQL/AD-aware backup helpers, remote agent upgrades, bandwidth limits, Wake-on-LAN, pre/post backup hooks, job grouping and richer central scheduling.
