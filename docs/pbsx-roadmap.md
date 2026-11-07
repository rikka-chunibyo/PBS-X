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
- an attributed PBS Plus-derived host-agent port containing the agent-side snapshot, ACL, aRPC, mTLS and restore foundations;
- PBS-X-native one-time enrollment with host-scoped upload credentials;
- scheduled and on-demand agent job dispatch with durable run/progress history;
- native agent-side PXAR/chunk upload into ordinary PBS host snapshots;
- direct agent-side restore from PBS snapshots, including progress and sparse-file handling.

Not wired yet:

- file browsing from the Host UI;
- production packaging and upgrade delivery for the cross-platform host agent;
- image backups and bare-metal recovery.

The Linux native path has been validated end to end in the PBS-X VM environment: one-time enrollment, heartbeat/inventory, on-demand dispatch, PXAR/chunk upload, normal `host/<host-id>` snapshot commit, direct restore, file/symlink fidelity and sparse-file preservation. Windows Server 2025 has also passed real VSS-backed backup/restore and expanded NTFS cases; see [Windows validation](windows-native-validation.md) for coverage and remaining limits.

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

Android 0.3.0 extends this with an explicit, editable **Backup ID**, independent
of both the enrolled device identity and the local scheduling UUID. Local jobs
can select the same datastore/namespace/Backup ID to share a history, or choose
different IDs for separate histories. Job names remain cosmetic snapshot notes;
sharing a history never merges the contents of successive snapshots. New job
forms default to a device namespace. Automatic device tokens follow normal PBS
namespace ACLs and ownership, including manually granted additional namespaces.
Desktop dispatched jobs retain the host-ID default described above.

The UI should expose Hosts as a top-level workload type with a detail view containing Summary, Backups, Disks/Volumes, Files, Jobs and Agent tabs. Host Backups now discovers normal PBS snapshots by device namespace, generated group owner or legacy device ID, while preserving operator namespace/owner checks. Rows and restore commands retain the actual custom Backup ID; enabled server jobs do not define history visibility. Datastore -> Content remains the complete datastore view.

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

PBS Plus currently uses a remote-filesystem/FUSE path:

```text
agent snapshot
    -> remote filesystem over aRPC
    -> FUSE mount on the PBS server
    -> proxmox-backup-client
    -> ordinary PBS host snapshot
```

PBS-X deliberately does **not** adopt that as its primary backup path. There is no need to optimize for a disposable first generation when the project can be designed around the long-term data path now. PBS Plus remains valuable for snapshot providers, filesystem metadata, control transport, restore primitives and certificate handling, but FUSE should only be added later if a concrete compatibility problem makes it useful.

PBS-X should not copy the PBS Plus sidecar-product architecture wholesale. PBS-X owns the PBS server, API, ACL model and ExtJS UI directly. The Go data-plane service can remain a separate process where that makes sense, but it should act as an internal PBS-X component rather than exposing a second competing management product.

### Intentional differences from PBS Plus

1. **Stable host backup group**

   PBS Plus uses the backup job ID as the PBS `host/<id>` group. PBS-X uses the Host ID. Jobs target a host; they do not define the host identity.

2. **Native PBS-X API and UI**

   Host configuration, permissions, jobs, status and snapshot links should be exposed through PBS-X itself rather than an injected sidecar UI.

3. **Snapshot provider hardening**

   PBS Plus snapshot code is a starting point, not an unquestioned import. In particular, LVM snapshot sizing, ZFS/Btrfs source assumptions and fallback behavior need review.

4. **Agent-side native PBS backup protocol from the start**

   PXAR creation, chunking and PBS backup-protocol upload belong on the agent. The server should orchestrate jobs and receive ordinary PBS backup streams rather than remotely walking the host filesystem through FUSE.

## Windows backups

The target is more than copying `C:\Users` into an archive. PBS-X should support both file-level backups and image/system backups.

### File backups

The first implementation should use VSS to obtain a consistent filesystem view, then build the PXAR/chunk stream on the agent and upload it through the native PBS backup protocol into a normal `host/<host-id>` snapshot.

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

For file backup, the agent should pick an appropriate consistent snapshot provider before scanning and uploading the filesystem:

- ZFS snapshot;
- Btrfs snapshot;
- LVM/LVM-thin snapshot;
- safe direct/fallback mode when a native snapshot provider is unavailable.

The PBS Plus implementations are references and should be hardened before being treated as production defaults. LVM snapshot sizing in particular must not use a fixed one-size-fits-all allocation.

Linux system/image backup can later stream snapshot block devices into PBS fixed-index images.

## Backup architecture

PBS-X is designed around the native agent-side data path from the start:

```text
Windows/Linux Host
        |
        | PBS-X agent
        | VSS / ZFS / Btrfs / LVM
        v
consistent filesystem snapshot
        |
        | local scan + metadata capture
        | PXAR writer
        | chunking / hashing / dedup negotiation
        v
PBS backup protocol
        |
        | --backup-type host
        | backup-id = <host-id>
        v
PBS-X datastore
```

The server owns policy, ACLs, schedules, task history, datastore selection and orchestration. The agent owns the filesystem snapshot, scan and data production. This keeps filesystem latency local to the protected machine and avoids building a FUSE transport that would later need to be replaced.

PBS Plus aRPC remains useful for control operations, inventory, restore/file-browsing requests and snapshot-provider plumbing where appropriate. It is not required to sit in the bulk backup data path.

### Image/system backup

Image backup is a second producer using the same host identity and snapshot model:

```text
VSS/raw volume or Linux snapshot block device
        -> block reader
        -> PBS chunking/fixed-index image stream
        -> host/<host-id>/<snapshot>
```

A single host snapshot may eventually contain PXAR file archives, one or more image archives and disk-layout metadata. File-level backup and bare-metal protection therefore stay inside the same PBS datastore model rather than becoming separate products.

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

3. **PBS Plus-derived agent/control foundation**
   - Import/adapt aRPC, mTLS, agent filesystem metadata and snapshot-provider pieces.
   - Preserve PBS Plus MIT attribution.
   - Connect agent identity to PBS-X Host identity.
   - Keep bulk backup data off the FUSE path.

4. **Native agent-side file backup protocol**
   - Reuse/port the PBS client, PXAR writer and chunk-upload logic for the cross-platform agent.
   - Preserve `host/<host-id>` grouping and normal PBS manifests/indexes.
   - Make the implementation usable by both Linux and Windows providers.

5. **Linux file backup**
   - ZFS/Btrfs/LVM/direct snapshot modes.
   - Agent-side filesystem scan and PXAR/chunk upload.
   - Restore to the registered host.

6. **Windows file backup**
   - Windows service integration.
   - VSS-backed filesystem backup.
   - Agent-side PXAR/chunk upload.
   - Windows metadata and restore behavior.

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
- the interfaces needed for a job to request an agent snapshot and drive native agent-side PXAR/chunk upload into `host/<host-id>`.

It is acceptable for build cleanup to follow once the source is in a proper PBS/Rust/Go build environment. The data model and storage identity should be correct before spending time polishing packaging.

## Later ideas

Possible additions after the core path works include changed-block tracking, application-consistent hooks, SQL/AD-aware backup helpers, remote agent upgrades, bandwidth limits, Wake-on-LAN, pre/post backup hooks, job grouping and richer central scheduling.
