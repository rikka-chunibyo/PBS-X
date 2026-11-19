# Android development work in progress

Branch: `pbsx-android-restore-scheduling`, based on `65e8d425e`.

The prior authorized publication is complete: Android 0.5.11-preview/code 20,
native 0.7.5-preview, tag `pbsx-clients-2026.11.19-preview.1`. All five public
and GitHub assets, latest metadata and downloads page were verified. The release
is documented in `client-release-2026-11-19-preview-1.md`. Android CI
44491284578 and reused native CI 44448795381 passed.

## Current user requirements

- Diagnose/fix the screenshot's MMS export ENOENT. Do not silently claim a
  complete export when a genuine binary attachment is missing.
- Add Shizuku restore destinations for shared folders blocked by SAF and
  shell-writable Android/data and Android/obb. Preserve existing data, staging,
  verification, skip/replace/rename semantics and traversal/symlink boundaries.
- Improve scheduling with local times, selected weekdays, intervals, charging
  and Wi-Fi constraints. Keep one pending occurrence per job and serialize
  transfers globally; missed times must not create a backlog.
- Add real Pause/Resume. **The user explicitly corrected the earlier retry
  interpretation: after Android stops/retries a job, resume the interrupted
  backup, not a fresh run from the beginning.** Do not label cancellation plus
  a full restart as Resume. Durable file/chunk checkpoints and reconnect support
  must be investigated/implemented; partial work must never become a completed
  restorable snapshot.
- Improve Shizuku backup app selection: independent APK/data/OBB choices and
  clearer advanced controls rather than the current shared app filter.
- Perform deduplication locally before uploads. Explain chunk hashing already
  happens locally; fix Android's absent PreviousBackupRef and per-archive
  previous-index registration before inventing a parallel file-hash format.
- Safe selective SMS/MMS/calendar/contact provider imports are authorized if
  they preserve existing data and avoid duplicates. Files first. Do not expose
  unverified destructive database replacement as a restore mode.

No new feature has been published from this branch. Never modify the personal
Samsung phone during development. Use fixtures/disposable tests. Do not restart
the development VM, hypervisor, or PBS server handling the user's backup.

## Findings and implementation so far

- Scheduling now uses persisted one-shot JobScheduler occurrences, with interval
  or selected-weekday/local-time rules, optional timezone and charging/Wi-Fi
  constraints. Due times survive reconfiguration and coalesce missed runs.
  Completion reloads the saved job and verifies its occurrence before advancing
  it; deleted jobs cannot be recreated by a late completion callback.
  onStopJob still cancels and requests retry. **This is not durable resume and
  must be replaced before the pause/resume requirement is considered complete.**
- PBS backup sessions keep writer/index/known-chunk state in memory. In
  `src/api2/backup/mod.rs` failed/disconnected sessions remove unfinished
  snapshots. `environment.rs` only accepts registered chunk references. Resume
  cannot safely be implemented by simply caching digests locally or appending
  unregistered chunks in a new connection.
- Android backupNamed now resolves a complete same-group previous snapshot and
  passes its reference into the uploader (uncommitted). `previous.go` and its
  unit test were added. A failed baseline query falls back to a full upload with
  an explicit warning.
- The shared uploader now loads/registers the prior index for **each** archive,
  not just whichever metadata/payload stream happens to initialize knownChunks
  first. It no longer trusts a reader-download fallback to register server
  chunks. Two new tests cover both indexes and safe full-upload fallback.
- Shared encoder/datastore/transfer/backupproxy race tests passed after that
  change. Android core race tests passed, including previous-snapshot selection
  and staged local restore conflict/traversal/symlink cases.
- MMS draft fix recognizes inline textual/SMIL payloads even if a provider has
  a stale `_data` path, and adds a real Android MatrixCursor regression. Missing
  binary attachments now identify the part ID. This does not prove the phone's
  specific missing attachment was text; genuine missing files still fail closed.
- APK, Android/data and Android/obb each have independent All/selected-package
  controls, opt-in checkboxes and compatibility with existing saved filters.
  Package inventory and external app folders are scoped to the app's Android
  profile rather than hard-coded profile 0.
- Restore can select a Shizuku shared-storage destination, including Android
  subfolders when the shell can access them. An authorized bound service uses a
  Go descriptor-anchored destination: no symlink traversal, exclusive staging,
  verified writes, atomic skip/rename conflict checks and staged replacement.
  The same selected paths and conflict modes are used as SAF restore. No direct
  provider-database replacement or automatic app-data path inference was added.
- Calendar unit tests cover multiple times, weekdays, catch-up and DST gap/
  overlap behavior. Debug and instrumentation APK compilation, JVM tests and
  lint passed locally. Android FUSE-specific staging/no-replace/fresh-directory,
  inline MMS, independent app-source and schedule tests were added to CI;
  instrumentation has not yet run for this branch.
- Added a bounded/versioned encoder checkpoint format that reconstructs nested
  directory state and the exact mid-file offset without replaying earlier
  source data. Byte-identical v1/v2 reconstruction tests pass. Failed outputs,
  malformed state, unknown/trailing fields and mismatched streams are rejected.
  This API is not wired into the production uploader. Server acknowledgment,
  durable client journal, walker state and recovery remain necessary.

## Research references

- Android JobInfo.Builder setPeriodic: constraints unmet in the interval wait
  until satisfied; charging loss can stop a running JobService.
  https://developer.android.com/reference/android/app/job/JobInfo.Builder#setPeriodic(long)
  https://developer.android.com/reference/android/app/job/JobService
- PBS technical overview: the client hashes source chunks locally and can use
  a previous snapshot's chunk list to avoid sending known chunks.
  https://pbs.proxmox.com/docs/technical-overview.html

## Remaining work

Run Android instrumentation for MMS, scheduling, independent per-source app
selections and privileged file restore. Implement durable coordinated resume,
including explicit pause and constraint-driven suspension. Research safe provider imports and
make only verified modes available. Run regressions, inspect final source state,
build signed APKs, and document exact capabilities/limitations. Keep this file
accurate as work proceeds; it is a repository work log, not a claim of shipment.
