# Release audit follow-up — 2026-11-17

Branch: `pbsx-client-releases`.

## Changes

- Android restores write each incoming file to a sibling temporary document,
  verify the byte count, sync ordinary file descriptors, close, then promote it.
  Read errors, short payloads, cancellation and a full destination cannot
  truncate the existing file. Failed temporary files are removed; Skip retries
  can add files that remain missing.
- SAF replacements retain the original under a unique recovery name until the
  incoming file is promoted. Failed promotion rolls back when possible, and
  errors identify retained recovery copies. File-type checks precede rename and
  deletion. Normal mutations update cached names and document IDs, including
  providers whose IDs change on rename. Error paths refresh before rollback.
- APT publication requires a complete matching executable set whenever any core
  package is included. Duplicate identities, upstream packages, incorrect
  architectures and uncovered packages fail before repository operations.
  Repository-support-only updates remain supported. The read-only
  `--check-release` option exercises the same validator without signing access.
- Linux bundle manifests record the actual desktop glibc symbol requirement
  and shared libraries from `readelf`. Check mode and installation validate
  those requirements before creating installation directories or changing
  services. The static agent and CLI remain available with `--headless`.

## Verification

All 75 Python regression tests passed: 16 publication, seven Android source,
14 ISO, 16 host bundle, 11 APT update guard and 11 APT publication tests.
The Android Go core suite passed locally with memory/parallelism capped; its
race suite and five standalone Java promotion/rollback tests passed in
[publication checks 44022099748](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44022099748).
Read-only HTTP fixtures required loopback access outside the filesystem sandbox.

[Native checks 44022099639](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44022099639)
passed Linux race tests, Windows native tests, both desktop/CLI bundles,
installer checks and Linux window rendering. The runtime extractor and preflight
also passed against the existing native desktop, which requires glibc 2.38.
An incompatible-runtime regression verifies refusal before installation paths
or service commands are touched.

[Android checks 44045748486](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44045748486)
passed real ARM64/x86-64 Go bindings, debug/release APK compilation, both Java
unit-test variants and both lint checks. All three SAF instrumentation tests
passed on a fresh Android 13 AOSP ATD emulator, covering streamed documents,
staged replacement, conflicts, folder protection and the 100-file directory
cache regression. Compilation and emulator testing use separate hosted runners;
the AVD has an explicit location and bounded userdata size to fit runner disks.
Heavy builds and the emulator run on GitHub, not the development VM. CI uses a
disposable debug signer for instrumentation fixtures; the established preview
signing key is never copied to CI.

The deployed APT publisher was backed up and atomically replaced on `pbs-x-apt`.
The deployed read-only validator accepted the actual published 4.2.9-1+pbsx1
server/client/file-restore set and repository support 1.0.3. Rollback copy:
`/var/lib/pbs-x-apt/release/backups/publication-review-7386ae330/publisher.before.sh`.
This validation did not export repository metadata or change package payloads.

## Follow-up review and limits

The follow-up traced temporary-file cleanup, promotion and rollback failures,
document-ID changes, folder protection, Linux preflight ordering, APT validation
before mutation, and CI artifact/signing boundaries. It added short-stream and
disk-full probes, and fixed repeated directory refreshes in the initial restore
change. The SAF instrumentation includes a 100-file directory-cache regression.
No additional blocking issue was found in the reviewed changes after those
corrections and the final hosted provider checks.

SAF providers must support rename/deletion and correctly implement document
operations. They do not offer a universal atomic replacement API: a process kill
or power loss during final renames can leave staging/recovery documents that
need manual recovery. One additional incoming file must fit on the destination.
Previously completed files remain if a later file fails. Deep-tree and Samsung
storage-provider acceptance still need physical-device testing.

The public app downloads and server ISO retain their existing immutable release
bytes. A new client release with the established signer is needed to distribute
the Android fix and updated Linux installer. No physical phone, hypervisor or
PBS server VM was modified for this audit.
