# Server update publication — 2026-11-17

Published version: **`4.2.9-1+pbsx2`**, Debian 13 amd64, with matching
`proxmox-backup-server`, `proxmox-backup-client` and
`proxmox-backup-file-restore` packages.

- Signed normal update feed: https://pbsx.rikka.ca/apt, suite `trixie`.
- [GitHub prerelease and verified bundle](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-4.2.9-pbsx2).
- Source: `4a7f2884d07ee7bca935eb70c4419029f74efdd0`.
- [Client preview.3](client-release-validation.md#third-client-preview--2026-11-17)
  supplies desktop/agent/CLI `0.7.2-preview` and Android `0.5.5-preview`.

Existing installations update with `apt update` and `apt upgrade`. Keep
`pbsx-repository` installed for trust, pins, state backup and maintenance hooks.
The immutable offline ISO still contains `+pbsx1`; its configured feed upgrades
it to `+pbsx2` when connected. This publication did not rebuild the ISO.

## Resulting behavior

The server now accepts repeated identical native backup/restore completion
acknowledgements without rewriting history. Conflicting results fail, and an
interrupted completion is retired before work can be dispatched again. The
release also includes the queued host snapshot/file browser, selected-path
restore UI and dispatch coordination with protected APT maintenance.

Client source-read, sparse-restore and incremental-verification fixes are in
the separately published clients. See the
[completion/native-read notes](completion-source-read-safety-fixes.md) and
[sparse/incremental notes](sparse-incremental-safety-fixes.md).

## Build and provenance

The dedicated Debian 13 build VM used one build worker, one archive compression
thread and an 8 GiB memory cap. The original development checkout and prior
release bundles were retained. Release-mode workspace tests passed twice:
**171 passed, zero failed, two ignored**. All five production completion-module
tests ran in the full server crate.

Release inputs were checked against the pushed commit, including materialized
Git LFS hashes and committed symlink types. Debian's generated Cargo
configuration and generated online-help index were the two expected build
outputs excluded from the source comparison.

The first build exposed a package-linter restriction on the intentional NUL
regex. The equivalent string check preserves rejection of unsafe restore paths,
and the browser regression test covers carriage return as well. The source
export helper now preserves committed symlinks before overlaying materialized
LFS files; a real export verified both symlink fixtures and all three LFS inputs.

Freshness checks also found stale standalone command-line targets in the reused
compiler cache. `make -B cargo-build` rebuilt the executable set with the same
Debian build flags; generated package staging was reset and the full checks and
packaging were repeated. ELF build IDs in all eight primary packaged
executables matched fresh compiler output. The live API and proxy contained the
exact expected revision. Clean release exports remain the normal supported
build route; existing Makefile targets alone are insufficient evidence of a
fresh cached build.

`SHA256SUMS`, `BUILD-INFO.txt`, Debian build-info and changes accompany the three
core packages and installer. Debug-symbol packages remain on the builder.

## Candidate upgrade and live checks

The complete package set was first published to signed `trixie-testing`.
Disposable VM115 accepted the public signing key and TLS, selected all three
candidate versions, and upgraded through the existing APT protection hooks.
Its protected pre-upgrade archive was readable, mode-restricted, and contained
server configuration and the credential registry.

After the upgrade:

- All three installed core versions matched `4.2.9-1+pbsx2`, `dpkg --audit` was
  empty, and server/proxy/agent services were healthy.
- The running version API identified the exact source revision above. The API
  represents upstream `4.2.9` as `version: 4.2`, `release: 9`.
- Real backup and restore completion API calls accepted repeated acknowledgements,
  rejected conflicting results without rewriting history, and retired matching
  interrupted running state without redispatch. These passed for old history
  and again for newly created runs.
- The published Linux agent completed a service ZFS-snapshot backup and queued
  restore. File hashes, special modes, ownership, ACLs, capabilities, symlink
  targets, hardlink identity and sparse allocation matched the source.
- The test job was disabled again. Its temporary testing source was removed;
  ordinary stable APT then selected all three tested package versions.

These are VM and hosted CI checks. They do not validate the user's physical
Samsung phone, R710 installation, storage hardware or disaster-recovery plan.

## Public verification and rollback

After stable activation, normal public HTTPS downloads of `InRelease` and the
package index were verified with the committed archive key. The signature and
index SHA-256 matched; all three downloaded `.deb` sizes and hashes matched
both the signed index and staged release files. All eight GitHub asset digests
matched before the prerelease became visible.

Repository state and the prior public APT tree were archived and verified before
publication, without copying signing keys, under
`/var/lib/pbs-x-apt/release/backups/pbsx2-publication-20261118-005211` on the
repository container. The previous package payloads and client previews remain
available; a package rollback still requires the explicit procedure and protected
state described in the [installation guide](pbsx-installation.md).

The build and installation-test VMs started for this run were returned to their
original stopped state after confirming idle work and healthy services. The
repository container remains running. The development VM and unrelated guests
were left running; the hypervisor was not restarted. The builder retains its
8 GiB/4-vCPU cap for future runs.
