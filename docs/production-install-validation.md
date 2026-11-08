# PBS-X package installation validation — 2026-11-08

Branch: `pbsx-production-install`. Server/Linux fidelity changes: `983fe02fa`.
Native re-enrollment follow-up: `dc3f0ed19`. Package version:
`4.2.9-1+pbsx1`; Linux agent: `0.6.0-preview`.

## Fixes

- Integrated official PBS 4.2.9 changes from
  `e0d20d290aac07ed86a86cf3b5a7f68c8b66e525`, including post-4.2.6 security and
  data-integrity fixes. Existing PBS-X host, namespace and ownership checks remain.
- Built real Debian server, client and file-restore packages. Their scoped APT
  policy selects fork updates, rejects upstream executable replacements and
  leaves unrelated system updates available. Version matching uses a regular
  expression; a middle-of-version wildcard did not work in the real resolver.
- The package build tolerates repeated configuration, skips memory-intensive
  optional DWARF rewriting, and explicitly bounds archive compression threads.
- The Linux service can restore ownership, ACLs, capabilities and special modes
  and operate snapshot providers. It sees host `/tmp`, `/etc` and home directories
  and creates its own state/configuration directories, retaining
  `NoNewPrivileges=yes` and an explicit capability bound.
- Unix capture preserves setuid/setgid/sticky bits. Linux restore applies
  ownership before ACLs, final permissions and file capabilities.
- Linux capture retains nanosecond modification times without an extra stat
  call. Restore retains that precision using confined file descriptors. This
  avoids missing same-size edits within one second in metadata change detection.
- A newly supplied enrollment code takes precedence over existing certificates.
  Failed enrollment does not fall back to the old identity. Consumed codes are
  cleared; stale initialization environment values do not overwrite an enrolled
  profile. Missing registry values are distinct from I/O failures.
- The installer starts services explicitly after package replacement. Upstream's
  upgrade hook uses `try-reload-or-restart`, which cannot start services the
  installer intentionally stopped for the coherent state backup.

## Fresh installation

Created VM115 (`pbsx-install-test`) from a checksum-verified official Debian 13
cloud image, not a clone of VM104. It has 4 vCPU, 4 GiB maximum RAM, a 32 GiB OS
disk and two empty 8 GiB data disks. DHCP assigned `10.0.1.35`.

The official signed Proxmox repository supplied dependencies, documentation and
the Proxmox kernel (`7.0.14-26-pve`). The cloud image needed its GRUB installation
target set to its OS disk during kernel setup; subsequent boot and `dpkg --audit`
passed. This was a Debian cloud-image preparation issue, not a custom PBS-X ISO.

The bundle installer completed on the clean base. Both PBS services and HTTPS
were healthy. Installed server, client and file-restore versions were
`4.2.9-1+pbsx1`. Checksums/control metadata were valid. `dpkg -V` showed only the
intentional enterprise-repository conffile change; packaged executables and GUI
verified. APT update completed without repository errors. The installer disabled
only the newly introduced default enterprise source, retaining the previously
configured community source.

## Live results

- Created a real RAID0 datastore through the PBS-X disk API using the two
  verified-empty VM disks. `zpool status` showed two top-level disks, ONLINE,
  with no errors. A separate child source dataset had POSIX ACL support enabled.
- Enrolled and installed the actual Linux agent service. Required-mode ZFS
  snapshots, backup commit and snapshot cleanup passed.
- Restores into a disposable directory under `/etc` matched file hashes,
  owner/group, setuid/setgid/sticky bits, POSIX ACLs, file capabilities, file and
  directory mtime precision, symlink targets, hardlink identity and sparse layout.
- Incremental snapshots covered changed, added and deleted files, a 3 MiB random
  payload, and a same-size modification with different nanoseconds within the
  same second. Restored contents matched the latest source.
- Re-enrollment revoked the original key, retained its historical owner identity
  and permitted the replacement key to append to that existing backup group.
- Stopped PBS, archived server configuration/authentication material and the
  credential registry, moved both directories aside and restored them from the
  archive. Permissions and contents matched. The existing agent/key then
  appended and restored successfully through historical-owner checks.
- All six shared-client live suites passed: incremental/restore modes,
  namespace enrollment, independent/shared backup identities, retained/revoked
  credentials, encrypted Go/Rust interoperability, and ACL-writer concurrency.
  Their disposable hosts, jobs, namespaces and credentials were removed.
- Upgraded all three packages to a clearly labeled **version-only test repack**
  `4.2.9-1+pbsx2`, with the same executable payload. Backup/restore still passed.
  Automatic downgrade was refused. Explicit rollback to the original bundle
  passed, preserving configuration, credentials and backup access. This tests
  package lifecycle mechanics, not compatibility with an unknown future schema.
- Corrupt and mixed-version bundles were rejected before package-state changes.
- After rebooting VM115, the pool imported, all three services started, agent
  identity was unchanged despite a stale INIT environment fixture, and another
  backup/restore passed. The temporary environment fixture was then removed.
- Web PAM login was verified; SSH remained key-only.

## Automated checks

- 96 targeted Rust tests passed: configuration, datastore, PBS-X API types,
  server library and pruning, including future-timestamp coverage.
- Targeted Go suites passed with the race detector. Added permission, timestamp,
  enrollment-priority, failed-enrollment and initialization-environment cases.
- The opt-in service test installed a real systemd unit through the production
  service library and exercised ownership, special permissions, file capabilities,
  host filesystem visibility and mounting.
- Privileged Linux restore regressions passed inside VM115. Shared lifecycle and
  agent command tests cross-compiled for Windows. No new Windows VM execution or
  physical Android testing was performed in this run.
- Real APT resolver tests passed locally and on Debian 13, including a newer
  upstream candidate, a newer fork candidate and prevention of automatic downgrade.

## Artifacts and scope

The local bundle is under
`artifacts/releases/pbsx-4.2.9-1+pbsx1-amd64/`, with `SHA256SUMS`, Debian build
metadata, the installer and Linux agent. The first package build embeds the
development label `1eace048e-production-install`; `BUILD-INFO.txt` records the
corresponding committed source. The clean build script records the actual source
commit for subsequent builds. Debug-symbol packages remain on the build VM.

The rehearsal retained the OS/client state and datastore while restoring server
configuration. It does not establish recovery from lost datastore disks, a lost
client encryption key, or every possible cross-version migration. The actual
R710/controller/disks have not been touched or validated. See
[installation and recovery](pbsx-installation.md) for the deployment procedure.

VM103 and VM104 are left stopped; VM115 is the running test instance. VM103's
original 32 GiB/8 GiB balloon configuration is restored; its build disk was
expanded from 64 to 96 GiB for packaging. VM114 remains stopped. The dev VM and
hypervisor services were not changed or restarted. The example Linux job is
disabled; its fixtures and snapshots remain available for inspection.
