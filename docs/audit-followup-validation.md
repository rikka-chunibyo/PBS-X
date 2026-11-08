# Audit follow-up validation

Branch: `pbsx-audit-followup`, based on integrated `pbs-x` commit `19adf0d3b`.
The overall Android progress bar remains deferred; the research note is in
[android-overall-progress.md](android-overall-progress.md).

## Changes

- The shared Go backup and reader transports use HTTPS port 443 when the URL
  omits a port, matching metadata/control requests. Explicit PBS port 8007 is
  preserved. Regression tests intercept sockets before connecting and compare
  all three request paths, including IPv6 in the shared transport.
- Android history validation uses the Go client's canonical HTTPS origin.
  Trailing slashes, host casing and explicit/default port 443 cannot bypass
  its shared-history encryption check. Imports use the same validator. Profile
  updates/default changes are checked before persistence, and backup startup
  rejects old saved conflicts before sending contents. Same-key shared histories
  remain supported.
- Settings imports reject wrong types for known preferences before installation.
  Legacy folder/schedule defaults are also validated before job migration can
  consume them. Failed imports preserve the existing vault/preferences; unknown
  typed preferences still round-trip.
- Host deletion holds the ACL configuration lock around its read/modify/save,
  then releases it before credential cleanup reacquires it.
- Android enrollment guidance describes automatic namespace grants rather than
  requiring a server-side job.

## Validation

- All 33 Go core tests passed with the race detector. Shared `backupproxy`,
  host-agent command/enrollment
  tests and host credential JavaScript tests passed.
- Six Rust enrollment/ACL unit tests and API/proxy/debug CLI builds passed.
- Live PBS tests passed for ACL serialization, namespace enrollment without
  server jobs, incremental backup/restore, shared backup identities, retained and
  revoked key lifecycle, and encrypted interoperability with the standard Rust
  client. All disposable hosts, jobs, namespaces and credentials were cleaned up.
- The ACL regression holds the real writer lock during host deletion, checks
  that deletion cannot edit ACLs while blocked, commits an unrelated fixture grant,
  releases the lock, and verifies that the grant survives and generated credentials
  are revoked without a cleanup deadlock.
- APK/AAR builds, Java unit checks, Android lint, signature and 16 KiB alignment
  checks passed. All 43 Android instrumentation tests passed with zero skips,
  covering the audit cases and existing app flows, including real Shizuku on
  the Android 13 emulator.

Logs are under `pbsx-android/build/reports/audit-fixes/`.

## Test deployment

Server binaries and UI deployed only to PBS test VM104. Rollback files:
`/var/backups/pbsx-audit-followup-20261108-015537.xBbCzV`.
Only PBS services inside VM104 were restarted. The host and development VM were
left running. The physical phone was not modified. No Windows VM deployment
was performed; the shared transport and Linux agent tests cover this source change.

Android version: `0.5.3-preview`, code 12. Install over the existing 0.5.x app;
no vault/settings-format change is required. The APK is
`pbsx-android/artifacts/pbsx-android-preview.apk`.

APK SHA-256: `7bb14d3fb2c10932979aafd7ed10f09eab57f4bdc0100274c59432f2075a4660`.
