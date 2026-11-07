# Restore audit fixes — 2026-11-07

The six audit findings are fixed in the production desktop agent, shared PBS
reader and Android catalogue. The tests exercise the real restore entry point
and native PBS transport.

## Changes

- Desktop filesystem writes use one `os.Root` for the destination. Directory
  symlinks and Windows junctions cannot redirect content outside it. Metadata,
  hardlinks, symlinks, ZIP output and temporary-file operations use the same
  directory capability. Windows ACL, timestamp and ADS opens use
  `NtCreateFile` relative to a resolved parent handle, rejecting unexpected
  reparse points. Native Windows root directory markers and root metadata
  reopening have explicit regressions.
- Both desktop restore modes write the archived payload, even when the
  destination has matching size and mtime. Short payloads fail before replacing
  the destination. Normal mode additionally restores supported metadata.
- Startup no longer scans for and deletes `.pxar-restore-*` files. Failure
  cleanup only removes the exact temporary file allocated by that operation.
  An interrupted process can leave an orphan; it is deliberately retained.
- Linux FIFOs use nonblocking, no-follow metadata handles. Sockets use relative,
  no-follow metadata operations without opening them as data streams. Other
  platforms report unsupported special files explicitly.
- Reader cancellation closes the transport during HTTP upgrade, HTTP/2 setup
  and downloads. Handshake deadlines are bounded to 30 seconds or an earlier
  context deadline; established transfers retain cancellation without that
  handshake deadline.
- Backup IDs use PBS rules independently of device IDs in desktop restore,
  Android browsing and server restore command/history schemas. Tests cover
  one-character IDs, leading underscores and 100-character IDs. Device ID
  validation remains separate.

## Validation

Linux native-agent packages, restore regressions, the vendored PBS reader and
all 27 Android core tests passed with the Go race detector. FIFO/socket tests
exercise normal and content-only restores. Reader tests cancel deliberately
stalled upgrade, HTTP/2 setup and downloads. Rust API/config tests passed
(14 tests), and the API, proxy and debug binaries built successfully.

All 29 Android 13 instrumentation tests passed with zero skips and a real
Shizuku service at UID 2000. Java unit tests, lint, release/test APK assembly,
signature continuity and 16 KiB alignment checks passed. Three live Android
core suites passed against the test PBS server, including enrollment and
backup identity behavior. See [Android validation](../pbsx-android/TESTING.md)
for the updated APK.

Windows validation runs on the dedicated Windows Server 2025 PBS-X VM, using
disposable source/destination directories and a separately scoped test PBS
namespace. The existing automatic storage credential returned HTTP 401, so
live tests use a temporary namespace-only API key. Original encrypted storage
settings are restored exactly afterward; existing tokens are not rotated.

The final native Windows unit run passed 98 top-level tests. Three existing
ACL/read-only probes skipped because their fixtures or required handles were
unavailable; all new regressions ran and passed. Live PBS restores passed for
all three custom IDs in both content-only and normal metadata modes. The VSS
round-trip suite passed all 11 cases: multiple sources, empty/Unicode names,
exclusive locks, hardlinks, relative symlinks, case-insensitive exclusions,
chunked/empty ADS, long paths, attributes/mtime, metadata-incremental ADS
changes and selected-file content-only restore. Oversized ADS rejection also
passed. Shadow-copy inventories were zero before and after.

The server schema fix was deployed only to PBS-X VM104 after verified rollback
copies at `/var/backups/pbsx-restore-audit-20261107-193158.ctMpsL`.
Installed checksums matched the tested artifacts; both PBS services were
healthy and idle. The live identity suite passed again after deployment.
All disposable audit hosts, jobs, namespaces, keys, ACLs and staging files
were removed. Original encrypted Windows storage settings were compared
exactly after restoration, and host/token identities were unchanged.

Build VM103, PBS VM104 and Windows VM114 were returned to their original
stopped state. VM103's 32 GiB/8 GiB balloon and VM104's 8 GiB/4 GiB balloon
configuration were restored; VM114's 8 GiB allocation was unchanged.

The actual Samsung phone and the dev VM are untouched. No physical Samsung
backup/restore was performed in this run. Logs and compiled artifacts are
under the ignored `pbsx-android/build/reports/` and
`pbsx-android/artifacts/restore-audit/` directories.
