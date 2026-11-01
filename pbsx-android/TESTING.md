# Android preview validation — 2026-11-01

Branch: `pbsx-android-app`, based on PBS-X `026c165a2`.
Phone target: Samsung S20 FE, Android 13 / One UI 5.1, unrooted with Shizuku.
No physical Samsung phone was available for these checks.

## 0.2.0: jobs and selective restore

The app now separates Jobs, Restore, Activity and Settings. Each local job has
its own folder selections, destination, interval, Wi-Fi and scheduled charging
constraints, and optional Shizuku sources. Existing settings migrate once into
a Phone backup job. Jobs sharing a PBS destination retain the same host group;
each snapshot contains that run's selections and records the job name in notes.

The snapshot browser pages directories and supports selecting individual files
across folders. New-folder restore remains the default. Skip adds missing files;
Replace also overwrites differing contents while leaving identical files
untouched; Rename keeps both copies with numbered suffixes. Existing folders
are reused, unrelated files remain, and no separate Merge option is present.
Type conflicts never delete existing directory trees.

Validation for this version:

- All 18 Go core tests passed with race detection, including selection
  normalization, path rejection, metadata pagination, empty folders, special
  entries rejected before writes, bounded content-cache eviction, cancellation,
  all three conflict modes, identical-file mtimes and existing rename suffixes.
- APK/AAR assembly, two Java unit tests and Android lint passed. Instrumented
  navigation caught and fixed a startup crash caused by accessing the window
  insets controller before materializing the window decor.
- **All 13 Android 13 instrumentation tests passed, with zero skips**. They
  cover the six baseline cases plus one-time settings migration, independent
  job settings, completion/interruption history, actual JobScheduler intervals
  and constraints, update/cancellation of individual schedules, tab/editor and
  in-app Back navigation, restore-mode choices, paginated native PBS archive
  browsing, selective SAF restores and all three conflict modes. Identical
  files retain their original mtimes; same-size differing files are replaced;
  existing numbered names and unrelated files are preserved. Hardware/system
  Back gestures remain a physical-device check because injected Back events
  were unreliable in ATD. Shizuku ran as shell UID 2000, not root.
- Live PBS-X integration passed again on VM 104 using only a disposable host,
  job and namespace. It verified job names in actual snapshot notes/listings,
  three snapshots and unchanged-chunk reuse, historical restore, paginated
  browsing, and selected-file restores with Skip, Replace and Rename through
  an authorized key and a separate destination identity. All disposable server
  resources and uploaded test tools were removed afterward.
- Signature, unchanged signing identity, ARM64/x86-64 libraries and 16 KiB APK
  ZIP alignment passed.

The Android tests use an expanded loopback-only HTTPS fixture serving a real
pxar archive over the PBS reader HTTP/2 protocol. It accepts dummy credentials
only; real PBS credentials never enter the emulator. Optional
`pbsx_screenshots=true` instrumentation captures software renders of actual
laid-out Android views under the app cache (`jobs.png`, `restore-files.png`,
`restore-modes.png`), since ATD's display output is black. These are view renders,
not evidence of Samsung/system-bar rendering. Review copies live in ignored
`build/reports/android-ui/`.

Replace uses the document provider's truncate/write operation. Failure or
cancellation can leave a partial file, and restores are not transactional.
No success is recorded for an interrupted run. Samsung's providers, system bars,
real source sizes and background scheduling still require phone testing.

## 0.1.1 follow-up: new phone destination setup

The S20 FE screenshots showed successful enrollment followed by a native upload
403: the phone host had no enabled job authorizing the chosen datastore and
namespace. Existing-host snapshot reads did not exercise this upload policy.
The phone's unscheduled `s20-fe-android` job now authorizes datastore `backup`,
root namespace, and refreshed its existing token's ACLs. No enrollment/key
rotation was needed, and existing Windows jobs were not changed.

Version 0.1.1 explains this condition with the exact host, datastore and namespace
and directs the user to Hosts → Jobs. Enrollment completion now mentions that
setup step. Notifications expand the complete message and show Cancel only
while an operation is running. The activity displays its build version.

Go unit/race tests, Java unit tests, APK assembly, lint, signature and 16 KiB ZIP
alignment checks passed. The live test now starts with a disabled target,
confirms rejection with actionable guidance, enables the target, and completes
backup and restore using the same enrolled key. The six Android instrumentation
tests described below were run on 0.1.0; the 0.1.1 follow-up used build/unit and
live-protocol validation. Full S20 FE backup/restore is still being user-tested.

## Initial 0.1.0 baseline

- Linux core tests: complete portable archive/restore round trip, unchanged
  chunk reuse, additions/changes/deletions, Unicode and empty files, nesting,
  source mutation, unsafe/duplicate paths, cancellation, enrollment, TLS pins,
  storage credential selection and failed socket binding. `go test ./...` and
  `CGO_ENABLED=1 CC=gcc go test -race ./...` passed.
- Existing `github.com/pbs-plus/pxar/backupproxy` package tests passed with the
  optional network dialer; its test package also cross-compiled for Windows.
  Existing desktop behavior keeps its default dialer.
- ARM64 and x86-64 native AAR compilation, development-signed APK assembly,
  two Java unit tests and Android lint passed. Lint has no errors; remaining
  warnings include English UI strings and newer optional test dependencies.
- APK signature verification and `zipalign -c -P 16 4` passed. The 16 KiB check
  covers APK ZIP alignment; no 16 KiB-page Android device was exercised.
- **Six real Android 13 instrumentation tests passed** on the AOSP ATD x86-64
  emulator: Android Keystore encryption/decryption; loading the native Go
  library and rejecting HTTP; launching the activity; SAF-compatible provider
  reads/writes, Unicode names, fresh restore subdirectories and refusal to
  overwrite; Shizuku APK access, immediate service reconnect and rejection of
  arbitrary private paths; a real HTTPS request using an Android Wi-Fi-bound
  native Go socket and loopback-only dummy server. Shizuku 13.6.0 ran as
  **shell UID 2000**, not root.
- Live PBS-X integration passed on test VM 104 (`10.0.1.21`). The Linux build of
  the Android Go core enrolled through the real API using its locally generated
  EC identity, uploaded three normal host snapshots into a temporary namespace,
  uploaded **zero new data chunks** for the unchanged snapshot, and restored
  both current and historical contents. Explicit source identity with an
  authorized key restored into a separately named destination client without
  adopting the source ID. This was a real PBS protocol test of the Go pipeline;
  the emulator was not given PBS credentials.
- The disposable PBS host, enabled job, namespace, snapshots, auto-issued token
  and uploaded test tools were removed afterward. Read-only checks showed the
  original host list and only the root datastore namespace remaining.

Instrumentation found and drove fixes for a fixture filesystem symlink mismatch,
Shizuku's asynchronous removal/rebind race, and Binder's inability to marshal a
server-thrown `RemoteException`. Shizuku failures now use a supported exception
type and each reader has a distinct service tag.
The core also verifies that cancellation interrupts a blocked provider pipe
write; transferred descriptors use Go's poller and close on cancellation.

## Artifact

Install `artifacts/pbsx-android-preview.apk`.

SHA-256:

```text
f7332259e64be7aa107b5bbdbaad95c6d1adb391da7c40ddf890a7b56277d56f
```

This is a preview signed with the local Android debug key. Its signing
certificate SHA-256 is
`d6f35fc4fc34b6acd83b2b2e3fd26f928339df400d1b9e256fab0181e5b3cab7`.
The private signing key is not included in source or build artifacts.

## Still requires physical-phone testing

- Samsung's actual SAF providers, photo collections and removable storage.
- Android/data and obb accessibility through Shizuku on One UI 5.1.
- Background scheduling, Wi-Fi handover, charging requirements, battery
  management and long-running transfers on Samsung firmware.
- Restoring APK sets into a folder works as ordinary file export; reinstalling
  split packages and restoring application-private state are separate concerns.
  This version does not install applications or restore private app state.

No full-phone image backup, private app database extraction, client-side PBS
encryption-key import or server-queued desktop-agent commands are implemented.

## Reproduce the isolated live test

Build `core/mobilecore` tests with `-tags integration -c`. Copy the test binary
and `test-live-pbs.sh` to the PBS-X **test** VM, then run the script as root with
the binary path. The script creates only a fresh `android-smoke-*` host, job and
namespace, generates enrollment on the VM, runs `TestLivePBS`, and cleans up.
Existing credentials are never copied off the VM. The script assumes its
disposable test datastore is named `backup`.

## Resource handling

VM 100 was killed by host-level OOM during the initial attempt; see
[the incident record](INCIDENT-2026-11-01.md). No command stopped or rebooted the
dev VM or hypervisor. Tools/caches now use disk under ignored `.tools/`; builds
are limited to 3 GiB and two CPUs. Emulator tests use a separate capped local
process. The normal Google APIs image repeatedly lost Android system services;
the smaller AOSP ATD image booted and completed instrumentation successfully.
ATD is optimized for headless tests and did not provide a useful rendered UI
screenshot. Physical Samsung UI and firmware validation remain outstanding.
