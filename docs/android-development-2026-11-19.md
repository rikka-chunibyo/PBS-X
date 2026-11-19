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
  the first CI emulator run exercised all eleven selected tests. Native staged
  restore passed on Android 13/FUSE. Two new assertions failed: Android JSON
  escapes `/` in SMIL text, and child controls inherit the disabled alpha from
  their parent. These tests now inspect parsed JSON and effective alpha. Added
  a twelfth test for durable/coalesced schedule occurrence behavior.
- Added a bounded/versioned encoder checkpoint format that reconstructs nested
  directory state and the exact mid-file offset without replaying earlier
  source data. Byte-identical v1/v2 reconstruction tests pass. Failed outputs,
  malformed state, unknown/trailing fields and mismatched streams are rejected.
  This API is not wired into the production uploader. Server acknowledgment,
  durable client journal, walker state and recovery remain necessary.
- Checkpoint testing also found unchecked short writes of split payload
  headers/content. Those writes now reject short output, and a failed stream
  cannot produce a checkpoint. Linux and Windows host-app CI passed for the
  first development checkpoint; final follow-up checks are still required.
- Follow-up development checkpoint `dc1ce5488`: Android build and all twelve
  Android 13 instrumentation tests passed (44503343675). Linux/Windows checks
  passed (44503343700), as did publication checks (44503343811). This is a
  development branch validation, not a public release.
- Added a separate document continuation producer with bounded serialized
  traversal state. Tests discard the producer mid-file, roll output back to
  the last checkpoint, reconstruct it and obtain byte-identical archives.
  Completed files are not reopened and folders are not relisted. Only the
  interrupted file's saved prefix is rehashed before continuing, so even
  same-size/same-mtime edits to that prefix are rejected without appending data.
- Added an experimental checkpoint-aware producer-side chunk writer. Checkpoint
  flushes a partial chunk, waits for upload/dedup and server index acknowledgment,
  then returns exact lengths/counts/index checksums. Tests cover restored-prefix
  validation without retransmission, corrupt/mismatched prefixes and failed
  appends. It is not connected to the production uploader or Android UI.
- Client foundation commits `ce84f66d4` and `dc1ce5488` are pushed. New server
  recovery work is still in progress and has not been released or installed.
- Started only build VM 103 (`pbsx-build`, verified guest IP `10.0.1.20`) after
  verifying available host memory. VM 104 remains stopped. Development VM 100
  and the user's phone backup server `10.0.1.39` were not changed. VM 103 login
  is `rikka`, not root. All new server source tests run in the isolated directory
  `/home/rikka/pbsx-resume-checkpoint-ce84`, with two compiler jobs and the
  existing cache at `/home/rikka/pbsx-safety-release-8cab2b1b9/target`.
  Existing builder checkouts have uncommitted work and must not be overwritten.
- Added opt-in server resume protocol work: `pbsx-resume=1` on the normal
  authenticated backup upgrade, secret `PBSX-Resume-Token` header, exact auth-ID
  and random-token binding, two alternating valid DIDX prefixes and an atomic
  private resume record. Normal PBS sessions retain their existing cleanup.
  Resume reopens index writers and registers only verified checkpoint chunks.
  Non-hidden prefix indexes are marked by existing datastore GC. There is no
  completed manifest until normal finish validates the two root archives.
  Admin task abort/cancel and final manifest/fsync failures retain ordinary
  cleanup; resumable transport interruption retains its last active checkpoint.
  Paused snapshots remain until resumed, cancelled or forgotten/pruned; no
  automatic expiration promise has been implemented.
- Continuation ciphertext now has a dedicated size-bounded `resume_state`
  upload endpoint. It is not a normal archive/blob in the final manifest.
  A checkpoint must consume freshly uploaded state for its exact sequence;
  alternating slots cannot overwrite the currently committed state.
  Checkpoint state and index downloads use generated fixed names, never arbitrary
  paths. Ordinary blob uploads are refused inside these opt-in sessions except
  for the final manifest. Backend support is currently filesystem datastores.
- Server prefix testing found the existing dynamic reader could not map an
  empty DIDX; it now handles an empty slice and empty offset lookup safely.
  Both new prefix tests passed. All 50 server and 34 datastore library tests
  passed in VM 103 (zero skips). Logs: `~/pbsx-resume-protocol-tests.log` and
  `~/pbsx-resume-datastore-tests.log`. A debug proxy executable was built and a
  stripped test copy staged for the separate runtime VM described below.
- Added resume-upgrade tests proving secrets stay in headers and malformed
  tokens are rejected before dialing. Added an always-encrypted AES-256-GCM
  continuation envelope (independent per-run key, random nonce, bounded zstd,
  run/sequence AAD) so source paths stay private even for unencrypted backups.
  Its latest race test log is `/tmp/pbsx-resume-codec-tests.log`.

### Live synthetic recovery fixture

- VM 104 was also started after rechecking host memory. Verified hostname
  `pbsx-test`, IP `10.0.1.21`, MAC `bc:24:11:97:9f:50`, SSH login `rikka`.
  VM 103 has no installed PBS runtime; VM 104 is the live fixture host.
- Created only a new datastore `pbsx-resume-fixture-20261119`, path
  `/var/lib/pbsx-resume-fixture-20261119`, with a fixture marker and dedicated
  expiring datastore-scoped test user/token. Original config copies and test
  credentials are private in `/home/rikka/pbsx-resume-live-20261119` on VM 104.
  The local credential file is ignored at
  `artifacts/android-resume/private/fixture.json`; never print it.
- A temporary systemd drop-in on VM 104 runs the test proxy from
  `/usr/local/lib/pbsx-resume-fixture-20261119/proxy`. Original installed binaries
  are untouched. `/run` is mounted noexec, so the initial staging attempt there
  failed and the helper automatically restored the original service; executable
  staging was moved to `/usr/local/lib` without weakening mount protection.
- **Runtime proxy is currently the experimental fixture version.** Rollback:
  `ssh rikka@10.0.1.21 python3 /home/rikka/pbsx-resume-fixture-20261119.py restore`.
  The helper removes only its drop-in and restarts the original test-VM proxy.
  Source helper is `/tmp/pbsx-resume-fixture-20261119.py` locally. Do not stop or
  change the phone's server `.39` or development VM 100.
- Live tests passed with race detection: pause/reconnect to the same snapshot,
  completed-file/directory reuse, exact restored file bytes, wrong-token rejection
  without deleting the checkpoint, forced transport loss after uncheckpointed
  writes, and GC marking paused prefixes before successful recovery. Two cases
  passed in 8.22 s (`/tmp/pbsx-resume-live-tests.log`). Test source:
  `backupproxy/resume_live_integration_test.go`. It refuses other endpoints,
  stores and credentials, and uses only synthetic data.
- Explicit cancellation is being added with `pbsx-resume-discard=1`: authenticate
  the original token before entering a worker, skip loading possibly corrupt
  continuation data, and use normal unfinished cleanup on close. Completed
  manifests cannot be discarded. The builder's updated 50 library tests and
  proxy build passed; the live proxy was refreshed and all three cases passed. Logs: `~/pbsx-resume-cancel-tests.log` and
  `~/pbsx-resume-cancel-proxy-build.log` on VM 103. Original staged binary SHA
  was `bbc9ac61720b8fc60278440e31557f8e1b28f9c915d779be8ee4511149d0dc43`;
  calculate/verify a new hash when refreshing it.
- A Go coordinator now connects walker, checkpoint streams, encrypted envelopes,
  durable preparation/confirmation/final-candidate callbacks and the server
  extension. It bootstraps a checkpoint before user file reads and defaults to
  64 MiB/30 s checkpoint intervals. It still needs the mobilecore bindings and
  Android encrypted pending-run store, lifecycle/UI and scheduling integration.

### Android integration and validation (continued)

The coordinator is now connected through `mobilecore/resume.go` to Android
`Operations`, `PendingBackups`, foreground controls and scheduled retries.

- A versioned per-run descriptor freezes the original timestamp, target, source
  identity, effective credentials and encryption fingerprint. Independent random
  tokens and state keys protect continuation data even for unencrypted backups.
  The small Android descriptor is AES-256-GCM encrypted with a separate Android
  Keystore key, atomically replaced and fsynced before checkpoint callbacks return.
- Resume verifies the server's committed sequence, encrypted walker state and
  prefix indexes; a missing acknowledged checkpoint never starts from zero.
  A final candidate is saved before Finish. A completed run can be verified without
  opening Android sources after app death, against manifest identity/signature,
  exact candidate file descriptors and root index checksums.
- Pause first asks for a checkpoint. A five-second bound interrupts a stalled
  operation and preserves the previous checkpoint. Cancel authenticates the
  original unfinished run and confirms its removal before dropping local secrets;
  completed snapshots cannot be discarded. Offline cancellation stays pending.
- Foreground UI and notifications have Pause, jobs have Resume / Cancel saved run,
  and retries reuse the same history entry. Scheduler constraint stops request a
  pause; paused occurrences are not advanced. Explicit manual pauses suppress
  automatic scheduling until resumed. The existing global operation guard permits
  only one transfer at a time, including scheduled jobs.
- Frozen jobs use explicit connection IDs. Keys/connections referenced by pending
  runs cannot be deleted, and connection edits are blocked until resume/cancel.
  Pending records are included in complete settings exports. Imports hold them
  and schedules; a different device identity is refused for source continuation.
- Shizuku export ZIPs for pending runs live under private files storage, survive
  process death and close, and have durable length/hash markers. A resumed run
  refuses missing or changed prepared exports instead of generating different
  bytes into the same snapshot. Completion/cancellation removes its own exports.
- History trimming retains pending-run entries. Scheduler configuration follows
  the vault/job lock order, and completion no longer holds the active-job map lock
  while rescheduling.

Live server fixture: all three synthetic protocol cases passed with race detection
in 8.62 s, including explicit cancellation and completed-snapshot protection
(`/tmp/pbsx-resume-live-tests.log`). The Android Go binding's encrypted live case
also passed: pause at source open, fresh-client resume of the same snapshot,
restored exact bytes, verification of an uncertain final response, rejection of an
altered final candidate, and completed-backup discard protection
(`/tmp/pbsx-mobile-resume-live-tests.log`). The lower-level case separately covers
mid-file recovery after a transport loss and garbage collection.

Added five Android instrumentation cases for encrypted pending storage reload and
corruption, history retention/job deletion, manual versus automatic scheduling
holds, exact export-cache reuse/missing/changed-file refusal, and settings export/import
including recovery secrets, import holds and credential edit protection. The Android 13
workflow now requires at least seventeen tests. Local universal Go bindings, debug
app/test compilation, Java unit tests and lint passed. The new instrumentation
cases still require emulator execution; do not describe them as passed yet.

Before publication: finish interruption/constraint/UI regression checks, inspect
all changed sources, run Android instrumentation and native checks, restore the
lab proxy, then build/sign release artifacts. This feature needs the matching new
PBS-X server extension; do not publish the Android client against an older server
or silently fall back to restarting an ordinary backup. Provider imports remain
unavailable pending safe, additive fixture-tested implementations.

## Research references

- Android JobInfo.Builder setPeriodic: constraints unmet in the interval wait
  until satisfied; charging loss can stop a running JobService.
  https://developer.android.com/reference/android/app/job/JobInfo.Builder#setPeriodic(long)
  https://developer.android.com/reference/android/app/job/JobService
- PBS technical overview: the client hashes source chunks locally and can use
  a previous snapshot's chunk list to avoid sending known chunks.
  https://pbs.proxmox.com/docs/technical-overview.html
- Android provider import research: ordinary SMS/MMS provider writes require
  the user-selected default SMS app; automatic contact aggregation is not a
  complete duplicate-prevention strategy. Calendar supports a separate LOCAL
  calendar, making isolated append-only imports a candidate. These documents do
  not establish that arbitrary Shizuku/Samsung provider writes are safe. Keep
  provider imports unavailable until fixture tests and explicit item selection,
  idempotence, account mapping and rollback behavior are implemented.
  https://developer.android.com/reference/android/provider/Telephony
  https://developer.android.com/reference/android/provider/ContactsContract.RawContacts
  https://developer.android.com/reference/android/provider/CalendarContract

## Remaining work

Run Android instrumentation for MMS, scheduling, independent per-source app
selections, privileged file restore and durable coordinated resume,
including end-to-end Android lifecycle and constraint validation. Research safe
provider imports and make only verified modes available. Run regressions, inspect final source state,
build signed APKs, and document exact capabilities/limitations. Keep this file
accurate as work proceeds; it is a repository work log, not a claim of shipment.
