# PBS-X production readiness and installation

Audited on 2026-11-07 at `1eace048e` on `pbsx-audit-followup`.
The findings below describe the pre-fix state. Follow-up work is on
`pbsx-production-install`; see [installation and recovery](pbsx-installation.md)
for the package-based workflow. Validation of the R710's actual hardware and
disk layout remains separate from the disposable VM rehearsal.

## Findings

### P1: A normal package upgrade can replace PBS-X with upstream PBS

`debian/changelog:1` still identifies the tree as `4.2.6-1`, using the upstream
package names. VM104 has that package version, with the PBS-X executables and
GUI deployed separately. There are no package holds or APT preference files on
that VM. Its configured repository offers `4.2.9-1`.

On VM104, `apt-get --simulate --only-upgrade install proxmox-backup-server`
selected upstream `proxmox-backup-server` and `proxmox-backup-docs` `4.2.9-1`.
An actual upgrade would replace the fork's packaged files. The simulation did
not install or change anything.

Before production, build identifiable PBS-X Debian packages, establish how fork
updates are selected, and rehearse an upgrade and rollback. Keep Debian and
unrelated security updates available. A permanent freeze of the entire server
is not an update strategy. Review upstream changes after our 4.2.6 baseline
before choosing the first production release.

The inherited package build is in `Makefile` and `debian/rules`; it has not been
validated by this audit as a complete PBS-X release. `README.rst` still gives
Debian 12/Bookworm build instructions, while this source/package baseline and
test server use Debian 13/Trixie. No PBS-X ISO or Debian package artifact was
found in the current checkout. The Go desktop agents need their own artifacts;
the server package install manifest does not include them.

### P1: The installed Linux service cannot perform required privileged work

`pbsx-host-agent/internal/agent/lifecycle/service.go:23` bounds the systemd
service to `CAP_DAC_READ_SEARCH` and `CAP_DAC_OVERRIDE`, with
`NoNewPrivileges=yes`. The Linux entry point actually uses this template.

The native restore path calls `Fchown` in
`pbsx-host-agent/internal/pxar/restore_unix.go:55`, but the service has no
`CAP_CHOWN`. Its snapshot providers also need privileges absent from that
bounding set, such as `CAP_SYS_ADMIN` for LVM snapshot mounts. With automatic
snapshot mode, provider failures fall back to reading the live filesystem;
snapshot-required mode fails the backup.

Reproduced on VM104 using a disposable transient systemd unit with the same
capability and no-new-privileges settings. Changing a new temporary file's
owner to UID/GID 65534 succeeded under ordinary root and failed with `EPERM`
under the service restrictions. The effective/bounding capability mask was
`0000000000000006`. The temporary file and transient service were removed.

Review the capability and filesystem sandbox together: `ProtectSystem=full`
also prevents writes to system configuration/program directories unless an
appropriate restore destination is made writable. Verify actual installed
service backup and metadata restore on disposable Linux hosts after fixing it;
interactive root tests do not establish service correctness.

### P2: Linux backup and restore lose special permission bits

`pbsx-host-agent/internal/agent/agentfs/syscall_unix.go:20` keeps only `0777`
before converting a Unix mode to Go's representation. The native PXAR adapter
supports setuid/setgid/sticky flags, but they have already been discarded.
`pbsx-host-agent/internal/pxar/restore_unix.go:58` independently masks archived
modes to `0777`, including during the ownership-override path.

Two temporary Go probes reproduced this through the production entry points:

- `NativeAccessor.Stat` reported `0755` for a verified `04755` source file.
- Normal `RestoreWithOptions` restored archived `04755` and `02755` files as
  `0755`, returning success.

These masks also discard the sticky bit. This affects recovery fidelity for
Linux system files and shared directories, even when contents match. Fix both
capture and restore, including operation ordering around ownership, ACLs and
capability xattrs. Add persistent regressions for files and directories.
The temporary failing audit probes were removed after reproduction.

## Recovery validation still required

PBS-X needs more than the backed-up files and a copy of `/etc/proxmox-backup`.
`src/server/host.rs` stores generated credential history under
`/var/lib/proxmox-backup/pbsx-agent-credentials`. Its current and historical token
IDs participate in ownership checks when a device appends snapshots after
re-enrollment. Losing those records can remove that historical-owner mapping.
The records contain token identities, not replacement copies of token secrets.

A server recovery procedure must capture the protected PBS configuration,
token/authentication state and PBS-X credential registry together with a
recoverable datastore. Inventory host status, queued run state and enrollment
state separately; old pending commands must not be blindly replayed after a
rebuild. Android encryption keys and full settings exports need an independent,
tested recovery copy. Recovering the server cannot recreate a lost client key.

This audit did not rehearse a complete clean-server recovery. Before relying
on the R710, restore the server state in a disposable environment, then verify
old encrypted snapshots, historical token ownership, re-enrollment, and a new
incremental backup into an existing history.

## Installation path for the R710

The intended first release can use the official PBS installer as its base;
a custom ISO is not necessary. The official ISO installs upstream PBS, so it
does not by itself install PBS-X.

1. Establish the R710's existing OS, boot/data disk identities and storage
   controller mode. For a ZFS datastore, use disks exposed directly through a
   suitable HBA/pass-through setup. Do not assume the installed PERC supports
   this; no controller firmware changes are part of this audit.
2. Install a compatible PBS 4.x/Debian 13 base on the designated OS disk(s),
   using the official PBS ISO. The installer overwrites selected disks. An
   existing correctly configured Debian 13 system is another supported upstream
   base, using the Proxmox repositories and packages.
3. Install the tested, versioned PBS-X Debian package set over that base, with
   the release's tested update-selection policy. Produce and validate this
   package set before providing production install commands; the current lab
   file-copy deployment is not a released installer.
4. Open `https://SERVER-IP:8007`, configure storage and create the datastore.
   The fork's RAID0 change applies to datastore ZFS-pool creation in the web
   UI; it is not a separate OS installer. A stripe has no disk redundancy:
   loss of one member can lose the entire datastore, so a separate backup copy
   matters if that layout is chosen.
5. Enroll clients with newly generated credentials, retain recoverable key/
   settings exports, and verify a backup, a changed-file backup, server-side
   verification and a restore to another destination before adding real data.

Official references checked for this audit:

- [PBS installation, disk requirements, Debian installation and repositories](https://pbs.proxmox.com/docs/installation.html)
- [PBS verification and maintenance](https://pbs.proxmox.com/docs/maintenance.html)

## Validation and scope

- Existing local PXAR restore, snapshot and native-backup package tests passed;
  the native-restore runner package has no standalone tests. The agent filesystem
  suite passed after an unsandboxed rerun because its test server binds a
  loopback socket.
- New temporary probes exposed the permission-bit failures above.
- A disposable VM104 systemd probe confirmed the ownership restriction.
- VM104's package upgrade was simulated, not applied. Both PBS services were
  active after the checks. Its installed server package's `md5sums` control file
  also contains a malformed escaped mount-unit entry, so `dpkg -V` aborts before
  verifying it. This is an observed lab package-state issue; its origin was not
  established as a fork regression and it was not changed during this audit.
- No production server installation, package upgrade, pool change, hypervisor
  restart, dev-VM change or physical-phone modification was performed. VM103
  remained stopped. Windows and Android device suites were not rerun in this
  audit; their previous results remain documented separately.

Next release gates are the Linux service/metadata fixes, a package-based fresh
install and upgrade/rollback rehearsal, and a server/key recovery drill.
