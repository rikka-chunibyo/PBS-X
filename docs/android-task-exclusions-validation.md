# Android task controls and backup exclusions — 2026-11-20

Built and tested **0.5.13-preview**, code **22**, on branch
`pbsx-android-task-excludes`. Published in
[client preview.2](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.20-preview.2)
at https://pbsx.rikka.ca/downloads/ alongside desktop/CLI `0.7.7-preview`.
The build includes the preceding encrypted snapshot-note privacy fix.

## Behavior

Pause and Resume belong to the task card. While running, a backup offers Pause;
when paused, its saved task offers Resume backup. The same snapshot and activity
entry continue, using frozen sources and credentials. The foreground notification
is removed while paused and starts again on resume. Job cards retain Back up and
Snapshots without separate saved-run Resume/Cancel controls. A job with a paused
task cannot create a second one. Other jobs may run while it is paused; actual
transfers remain serialized across the app.

Genuine failures finish their activity entry as failed. Failed/cancelled recovery
records are cleanup-only and do not become resumable tasks or permanently hold
schedules. Before creating a replacement task, the app verifies an uncertain
completion or removes only that run's unfinished server snapshot. It retains
cleanup secrets until server cleanup succeeds. Activity also offers Clear task.
Completed snapshots are protected. Import preserves terminal cleanup states,
while live imported tasks and schedules remain held for review.

Service completion now runs on the main thread with ownership and start-ID
checks. New transfers remain gated until that service has finished removing its
notification; an old worker cannot stop a newly resumed task or replace its
notification. Pause is accepted even before the native client is attached.

Each job has a multiline Exclude files & directories field. Rules are
case-sensitive and relative to the selected backup folder labels. For example,
`DCIM/.thumbnails` excludes that directory and its subtree. Exact file paths,
`*` within names, `?` for one Unicode character, and recursive `**` components
are supported. `**/*.tmp` excludes temporary files at any depth. Matching uses
bounded path/segment matching rather than backtracking regular expressions.

Filtering covers SAF, Shizuku folders/app sources and device-export ZIP files.
Excluded subtrees are omitted from enumeration; direct stat/read/traversal of
excluded paths is refused. Entire excluded sources need not be opened, and
excluded device-export ZIPs are not prepared. Restores are unaffected by backup
exclusions. Existing snapshots retain their contents.

Rules survive job save, editor recreation and settings export/import. Nonempty
rules are frozen into the task's source identity. Empty rules are omitted when
serializing old jobs so existing pending source identities stay valid.

## Verification

[Android workflow 44779500341](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44779500341)
passed at source `03991e5d076b7b5f7b5652a4e9b4b17b56645dc3`:

- Mobile Go race suite and universal ARM64/x86-64 bindings.
- Both app variants, instrumentation APK, Java unit tests and lint.
- **24 Android 13 instrumentation tests**, including a real foreground-service
  pause/notification-removal check and actual task/job view controls.
- Failure terminal states, duplicate-task refusal, independent paused jobs,
  schedule recovery and settings imports with terminal records/exclusions.
- SAF enumeration/read/traversal boundaries and existing restore behavior.

The final local build also passed the Go suite, both variants and lint, with
**18 Java unit tests per variant**, zero failures/errors/skips. Exclusion cases
cover exact/subtree matching, case and prefix collisions, recursive wildcards,
Unicode, malformed/unbounded rules and adversarial wildcard combinations.

The notification fixture grants notification permission on the disposable
emulator and uses instrumentation privileges to start the test service; it does
not revoke a runtime permission during the instrumentation process. Test state
is restored after each UI/service fixture. No physical phone, datastore or
hypervisor was changed for this work. The local Device panel reported no
available Android emulator, so device execution used the hosted test runner.

The final publication source is `98b673827a3de9ea68dd0def021afbd00819f7ad`,
which adds release documentation and native version defaults to the tested code.
[Android run 44796621183](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44796621183)
passed all 24 instrumentation cases again. The APK was rebuilt and verified from
that clean source; all five client assets were downloaded and hash/size-verified
from both public HTTPS and GitHub after activation.

APK: `pbsx-android/artifacts/pbsx-android-preview.apk`, 27,032,611 bytes.
SHA256: `6176e3f13c22c139b6aa9036bc65663e89eaec2018107532ab3f4ac877b16abd`.
Signing certificate remains
`d6f35fc4fc34b6acd83b2b2e3fd26f928339df400d1b9e256fab0181e5b3cab7`.
The build records clean source and the exact Android/shared-core input trees.

## Encryption and deduplication

The restore wire format is unchanged. Android downloads ciphertext chunks and
authenticates/decrypts them on the device; file browsing, path selection and
writes to SAF/Shizuku destinations happen locally. Validated HTTPS also protects
transport. It protects data up to its TLS endpoint; plaintext metadata sent to
that endpoint remains readable there.

Encrypted PBS snapshots already share stored chunks using key-dependent content
IDs. Deduplication is decided during backup; the stored snapshots continue
sharing those chunks afterward. No separate decrypt-and-deduplicate pass is
required. Arbitrary randomized ciphertext cannot be compared by ordinary
plaintext hashes, which is why PBS uses stable secret-dependent chunk IDs.
The existing archive and checkpoint privacy limits are documented in
[the Android README](../pbsx-android/README.md#encryption-privacy-and-deduplication).
