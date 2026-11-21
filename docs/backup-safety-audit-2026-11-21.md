# Backup safety audit — 2026-11-21

Branch: `pbsx-audit-20261121`, continuing the previous audit branch.
Final fix source: `7d7fb919fcc1da3e3d86d4edc6fcaac38a093e67`.

This pass reviewed Android settings/credential consistency, enrollment admission,
scheduled checkpoint recovery, restore decoding and release activation guards.
Tests use isolated settings, temporary files and dummy credentials. No live PBS
server, phone, repository container or hypervisor was changed.

## Settings and credentials during transfers

Credential mutations checked `Operations.running()` and then saved the vault
without reserving the transfer slot. A transfer could start after that check,
while its effective connection or job references were being changed. Settings
export had the same check-then-capture gap, and job editing validated policy
separately from saving it. Deleting a job during early transfer preparation
could also orphan its subsequently created recovery record.

These operations now reserve the same atomic slot as transfers, releasing it
on success or failure. Job validation and save share the existing profile/job
lock order, and normalize the target before checking its encryption policy.
The job editor uses these guarded save/delete paths. Paused tasks can still have
their next-run job settings edited; their frozen recovery settings remain intact.

Enrollment reserves the slot before consuming its one-time code and keeps it
until the connection is saved. A busy rejection therefore happens before the
bootstrap request. The profile monitor is not held across the network request.

A deterministic Android test holds nine credential/job/export operations at
their preference access boundary and checks that a second operation cannot
enter. Additional cases cover failed edits, busy enrollment, gate release and
conflicting normalized backup histories without changing the saved job.

## Interrupted import reference checks

Credential removal and connection updates previously checked job/pending-run
references before calling `Vault.data`, which can replay an interrupted settings
import. Those checks could therefore use the pre-import empty state, then remove
a key or alter credentials needed by the newly restored jobs or paused backups.
Job deletion could similarly report success against the pre-import job list.

These mutations now complete the import journal before checking any references.
An Android fixture installs a complete but unreplayed import containing a job,
encryption key and paused run. Key deletion, credential replacement and job
deletion must each be refused after recovery, preserving the original key,
credentials, job and descriptor. Transfer reservations are released after refusal.

## Schedule replacement and stale callbacks

The scheduler previously included the entire job configuration and pending
history ID in its replacement hash. Creating the recovery record changed that
hash even when the due time and Android constraints were identical. Opening the
app or otherwise refreshing schedules could therefore replace/stop the running
scheduled job solely because it now had a checkpoint.

Replacement now depends on the job identity, effective Wi-Fi/charging constraints
and the separately checked due time. Checkpoint creation and edits to next-run
sources do not replace a running job. Frozen constraints remain in use until the
saved task is finished; actual constraint changes still reschedule the next run.

Scheduled admission now also reloads the current job and occurrence after
reserving the transfer slot and recovering any settings import. A callback for
a disabled or superseded occurrence returns before creating an activity entry,
cleaning an older checkpoint, opening sources or making network requests.

Android regressions check the unchanged replacement hash/minimum latency across
checkpoint creation, retained frozen constraints, actual constraint replacement,
and refusal of disabled/stale callbacks without creating a task.

## Blob decoding limits and format checks

The shared Go blob encoder already enforced a 128 MiB plaintext limit, but its
decoders did not enforce the corresponding encoded/decompressed limits. The
default Zstandard decoder allowed much larger expansion. The encrypted decoder
also accepted a valid ciphertext whose header had been changed to a plain or
unknown magic value. Existing restore digest/signature checks remain separate;
this change enforces the format before exposing decoded contents.

Decoding now bounds encoded input and Zstandard output/window size, including
unknown-size and concatenated frames. Encrypted decoding requires an encrypted
magic value. Valid archive formats, AES authentication and chunk IDs are unchanged.

Four new Go regressions failed against the old code: oversized plain/encrypted
decompression, concatenated expansion, oversized encoded input and invalid
encrypted magic. After the fix all pass. A fifth test accepts a valid blob
exactly at the format limit and verifies subsequent pooled-decoder reuse.

## Validation

Local checks passed:

- Complete Android Go core and Linux host-agent race suites.
- Shared datastore, transfer and backup-proxy race suites, including the new
  size-limit tests and existing corruption/refusal/restore cases.
- Nineteen Java unit tests per variant, both lint variants and compilation of
  the instrumentation APK. Existing local native bindings were used for this
  Java-only compilation; hosted CI rebuilds the shared Go bindings from source.
- Workflow YAML, test-completion gate and patch whitespace checks.

Hosted validation:

- [Android checks 44946857645](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44946857645):
  passed universal bindings, both APK variants, Java tests, lint and all
  **47 Android 13 instrumentation tests**, with 47 individual successful
  completion records and zero skips.
- [Native checks 44946062598](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44946062598):
  passed native Windows/Linux tests, shared archive suites, desktop bundles,
  installer/CLI checks and the Linux window smoke test.
- [Publication checks 44946062577](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44946062577):
  passed publication refusal, durable completion and core/restore regressions.

Native/publication checks used `baf600775493b54aa66c65ecf10bc30f436bfd82`; their Go, archive and
publication inputs are unchanged in the final Android-only follow-up.
The Android workflow requires one successful completion record for each
reported test and rejects skips or an incomplete suite.

## Scope and limits

The Device panel reports no available Android emulator locally; device tests
use the hosted disposable Android 13 runner. Physical Samsung/Shizuku, charging
and long-running background acceptance remain hardware tests. Rust server code,
live PBS integration and offline installation were not rebuilt or exercised.
Shared blob changes passed on Linux and in the native Windows workflow. The
existing two POSIX datastore-layout fixtures remain skipped on Windows because
PBS timestamp directory names contain colons; those fixtures run on Linux.
Release-upload jobs are intentionally skipped for ordinary audit pushes.

This branch does not publish new app versions or replace the existing APT feed,
client downloads or offline ISO. Settings formats and paused-task identities
are unchanged. The prior audit's SAF process-kill/power-loss and changing-source
limitations still apply; see [the preceding audit](android-safety-audit-2026-11-20.md).
