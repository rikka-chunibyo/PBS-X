# Recovery durability and task ownership audit — 2026-11-21

Branch: `pbsx-audit-restore-durability`, continuing `pbsx-audit-20261121`.
Fix source: `0951964a42fe209a3361998fb04e49e1020065e1`.
Lifecycle fixture source: `bf5892486a0286311cd08f109139421a0e86f8b0`.

This pass covers private recovery-file commits, prepared exports, restore
directory persistence and foreground-service notification ownership. All
fixtures use disposable directories and dummy settings. No live server,
datastore, phone, repository container or hypervisor was changed.

## Checked journal and export-marker commits

The application previously relied on `AtomicFile.finishWrite` returning normally
as confirmation that the recovery file or export marker was committed. Android
13's implementation logs final sync/close/rename errors instead of propagating
them. See the [AOSP implementation](https://raw.githubusercontent.com/aosp-mirror/platform_frameworks_base/android13-release/core/java/android/util/AtomicFile.java).
Explicitly syncing the file beforehand did not detect a failed final rename.
A failed journal commit could therefore become an in-memory cached record and
acknowledge recovery state that had not actually replaced the saved file.

Private writes now use a checked sequence: create a sibling temporary file,
write and sync its contents, close it, atomically replace the target, then sync
the parent directory. Each failure is propagated and temporary files are
cleaned up. Existing directories/symlinks are refused rather than deleted or
followed. Failed journal writes invalidate the cache, including failures after
the rename, so subsequent recovery reads the actual on-disk state.

Legacy `.bak` recovery also uses a checked rename and directory sync. An orphaned
legacy `.new` file with no main record is reported as damaged/incomplete storage
instead of silently becoming an empty task list. The encrypted record format,
Keystore alias and frozen source identities are unchanged.

Prepared export markers use the same checked writer. An export is added to its
source map only after its marker commit succeeds. Private export directories
are created one level at a time with child/parent barriers, so the prepared
files are not acknowledged inside a hierarchy whose entries were never synced.

Seven standalone tests cover complete replacement, final-rename failure,
directory conflicts, unavailable parents, legacy recovery, symlink refusal and
repeatable directory creation. Android fixtures exercise real encrypted record
reload/recovery, refused journal/marker commits and orphaned first-write state.

## Restore directory barriers

Descriptor-anchored restores synced file data and file renames, but creating a
restore directory did not sync either the new directory or its parent entry.
Empty directories and new hierarchy entries could therefore lack the expected
durability barriers when a restore returned successfully.

Fresh restore roots and ordinary directories now sync both descriptors before
returning. Reusing an existing directory repeats those barriers, including a
retry after mkdir succeeded but a sync failed. Traversal still rejects links,
and file/directory conflicts preserve existing content.

A disposable Linux fixture creates a fresh root and a nested empty directory,
reuses the nested directory and checks a file conflict. Syscall tracing of the
same fixture showed **zero** durability calls before the fix and **eight
successful `fsync` calls** afterward, covering each child/parent pair. The
private-writer trace likewise confirms file sync → rename → directory sync.
These traces demonstrate requested OS barriers, not an actual power-cut test.

Android 13's [MediaProvider FUSE implementation](https://android.googlesource.com/platform/packages/providers/MediaProvider/+/refs/heads/android13-security-release/jni/FuseDaemon.cpp)
also implements directory sync by forwarding it to the backing directory's
descriptor. This source check supports the use of directory barriers on shared
storage; it does not replace testing Samsung's actual storage implementation.

## Notification ownership

Stopping/timing out a foreground service removed its notification, but a late
timer or worker progress callback could still post the notification again.
Worker progress now runs through the main-thread handler and checks the service
instance, cancellation token and whether notifications are still allowed for
that lifecycle.
Timeout/destruction suppresses future updates while allowing the worker to
retain its checkpoint. A timeout fixture explicitly delivers an old refresh
callback after stopping and checks that the notification stays removed.

Pause/Cancel pending intents previously shared an identity across operations.
A retained action could therefore control a later resumed transfer. Each
operation now has a distinct pending-intent data URI and command token; the
service rejects stale/unscoped commands. Ignored commands stop an idle service
using their start ID, without stopping a newer queued start. A real-service
fixture keeps an old Cancel pending intent, resumes the task, sends that old
action and verifies that the new operation remains unaffected.

Service fixtures wait for actual operation acquisition and destruction, rather
than assuming an idle main looper means Android has delivered a service start.
Control assertions wait for the service's start ID to advance on the main
thread, so delayed Pause/Cancel delivery cannot produce a false pass. Hosted
failures now retain a bounded logcat capture alongside instrumentation output.

## Validation

Local validation passed:

- Complete mobile Go race suite, including directory creation and existing
  selective-restore, integrity, failure and cancellation cases.
- **26 Java tests per variant**, zero failures/errors/skips; both lint variants
  and instrumentation APK compilation.
- **12 standalone Java durability/SAF rollback tests**; seven private-file
  cases were also traced against the real temporary filesystem.
- Fault injection with `strace`: both child/parent restore-directory sync
  failures stop the restore; both private-file/parent sync failures propagate
  and remove temporary files. A parent-sync failure after promotion leaves a
  complete new file, but is still reported as failure. All injected failures
  ran against disposable Linux directories, not an Android device.
- Workflow parsing and whitespace checks.

Hosted checks:

- [Publication checks 44950453912](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44950453912)
  passed corruption/refusal tests, durable host completion, mobile Go race tests
  and all 12 standalone Java cases.
- [Android checks 44951567152](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44951567152)
  at the lifecycle fixture source passed universal Go bindings, Go race tests,
  both APK variants, Java unit tests, lint and **all 52 Android 13 tests**.
  Instrumentation output contains exactly 52 successful per-test result records,
  matching `OK (52 tests)`, with no failure/skip result codes. This includes the
  two new service lifecycle cases and three new pending-storage failure cases.

The first emulator run exposed a fixture startup race: `waitForIdleSync` could
return before ActivityManager delivered the foreground-service request. The
fixtures now wait for operation acquisition, command delivery and service
destruction. The complete suite above passed with those corrections.

## Limits

The Device panel reports no local Android emulator, so device execution uses
the disposable hosted runner. Samsung/Shizuku and storage hardware acceptance
remain physical-device tests. This pass did not simulate power loss, repeat
live PBS integration or change desktop/Rust server code. Directory sync failures
are errors; no successful durability claim is made when the filesystem refuses
a required barrier.

SAF provider restores still lack a universal transactional replacement API;
their process-kill/recovery-copy limitation is unchanged. Public APKs, desktop
bundles, APT packages and the offline ISO have not been replaced by this branch.
