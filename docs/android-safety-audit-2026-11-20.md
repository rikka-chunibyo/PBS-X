# Android task and recovery audit — 2026-11-20

Branch: `pbsx-audit-20261120`, based on the published client preview.2.
Final fix source: `238732779afade9455e8924819265e41502377bd`.

This pass reviewed task completion and failure recovery, scheduled retries,
checkpoint/source binding, exclusion enforcement, restore staging and conflict
handling, encrypted manifest/chunk verification and settings import references.
It also ran the local release-publication refusal suites. No physical phone,
PBS datastore, download repository or hypervisor was modified.

## Fixed

`JobStore.finish` previously ignored an unsuccessful synchronous history write
and accepted a nonexistent activity ID. Its backup caller could then remove the
local recovery descriptor without durably recording completion. Completion now
requires an existing activity entry and a successful write before the caller
may clear recovery state. Interrupted-history recovery also checks its write
result. A completed server snapshot is not deleted by this change.

Calendar scheduling previously returned the first wall-clock time that mapped
after the previous occurrence. During a daylight-saving gap, adjusted wall
times can be out of chronological order: in Toronto, `02:30,03:00` becomes
`03:30,03:00` on the spring transition day. The scheduler now chooses the earliest
actual instant for that date. Regressions cover reordered times, the following
occurrence and two wall times resolving to the same instant. The new test failed
against the previous implementation before the fix.

Settings validation previously required only that a pending backup's activity
ID appeared somewhere in the imported history. Duplicate IDs, a different
job's activity or a restore activity could pass that check. Import now rejects
duplicate history IDs and requires the pending entry to belong to the same job
and be a backup. Normal exports and frozen recovery descriptors are unchanged.

Recovery also previously reconciled only `running` activity entries. If a
terminal checkpoint had been saved while the activity still said `paused`, a
crash could leave the task permanently displayed as paused. Recovery now
reconciles that pair too, preserving real pause reasons and already-recorded
successful completions. A regression exercises failed and cancelling checkpoints
against the older paused history after clearing the recovery cache.

## Regression coverage

New Android instrumentation injects failed preference commits and a missing
activity entry, checks that the encrypted checkpoint remains available, and
rejects duplicate/wrong-job/restore history links. The fixtures use isolated
private directories and dummy credentials.

The regular Android workflow now includes all credential-vault, settings-safety
and earlier audit-policy cases, alongside the task, provider, schedule and UI
checks. Its minimum increases from 24 to 41 cases. Besides checking the runner's
final result, CI counts successful per-test completion records so a skipped
case cannot satisfy the gate.

## Validation

- Android Go core race suite passed, including restore failure and manifest
  integrity fixtures.
- The complete Linux host-agent race suite (`-tags agent`) and shared
  datastore, transfer and backup-proxy race suites passed.
- Debug and release Java suites each passed 19 tests, with zero failures,
  errors or skips. Instrumentation APK compilation and both lint variants passed.
- Standalone calendar and SAF promotion/rollback suites passed all nine tests.
- All 78 Python checks passed: 18 client publication, seven Android source,
  11 APT publication, 28 combined ISO publication and 14 ISO builder cases.
- Workflow YAML, the instrumentation completion gate and patch whitespace checks
  passed.

Hosted Android run
[44823898300](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44823898300)
passed at the final fix source: both universal native bindings, debug/release
APK builds, Java tests, both lint variants and all **41 Android 13 instrumentation
cases**, with 41 individual successful completion records and zero skips.

## Limits

The Device panel reports no available Android emulator on this machine; device
tests use the disposable Android 13 hosted runner. Samsung storage-provider,
Shizuku and charging/background behavior still need physical-phone acceptance.
Live PBS integration fixtures and Rust server builds were not repeated because
this audit changes only Android Java and its workflow. Public client releases,
server packages and the offline ISO have not been replaced by this audit.

SAF providers still lack a universal transactional replacement operation. A
process kill during final renames can leave staging or recovery copies requiring
manual recovery; existing provider requirements and that limitation remain in
[the restore audit](release-audit-followup.md). Checkpoint resume preserves
already-read data and revalidates the interrupted file; it is not a filesystem
snapshot of a changing phone.
