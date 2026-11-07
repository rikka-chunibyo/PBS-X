# PBS-X Android preview

Native Android UI with the same vendored Go pxar/PBS engine used by the desktop
host agent. The APK contains ARM64 for phones and x86-64 for emulator testing.
Minimum Android 8; the initial device target is an unrooted Samsung S20 FE,
Android 13 / One UI 5.1, with Shizuku installed. This is an early file-backup
preview, not a full device-image or private-app-data backup tool.

## Try it

1. Install `artifacts/pbsx-android-preview.apk`. Permit installation from the
   browser/file manager you use to open it. This is a development-signed build;
   keep the signing key when building upgrades.
2. In the PBS-X web UI, create a host and an **enabled host backup job** allowing
   the desired datastore and namespace. The automatic token checks that target
   against enabled jobs. Generate the host's enrollment code and paste it into
   **Enroll with PBS-X code**. The private device identity stays on the phone.
   The Android preview selects files locally; its selected folders determine
   the archive contents, rather than the server job's source-path list.
3. Open **Jobs → New job**, name the job and select the matching datastore and
   namespace. Add DCIM for camera photos/videos or other folders using **Add a
   folder**. Give each folder a distinct archive label, such as `Photos` or
   `Documents`. Each job has its own sources, schedule and network settings.
4. Tap the job's **Back up** button, initially using a small folder. Wi-Fi-only is enabled by
   default. Allow notifications to see progress and cancellation controls.
5. Tap **Snapshots** on a job to open its configured destination, or use
   **Restore** to see every backup group visible to your storage key across
   datastores and namespaces. Search that list, open a group's snapshots, and
   check individual files or whole folders, including selections across folders.
   Tap **Restore selected**, choose a restore mode, then choose and confirm a
   writable destination. The default creates a fresh `PBS-X-restore-*` folder.
   Selected paths keep their archive hierarchy. Check restored contents before
   relying on this preview for important data.

The bottom navigation separates **Jobs**, **Restore**, **Activity** and
**Settings**. Activity retains the latest 100 local runs and their outcomes.
The app uses a dark theme for screens, menus and dialogs. Active transfers show
the current phase and file, completed-file/logical-byte counts, actual uploaded
chunk bytes, and a progress bar in the app and manual-transfer notification.
The percentage is for the **current file**, not the whole job. Scanning,
preparing sources, comparing files and saving the snapshot use an indeterminate
bar. Reading unchanged files still advances progress even when PBS reuses every
chunk and uploads zero new content. Job and Activity rows update while running.
Existing folder and schedule settings migrate into a `Phone backup` job on
upgrade; the existing connection and keys are retained. Editing or deleting a
local job never deletes its PBS snapshots.

Restore modes reuse existing folders and keep unrelated destination files:

| Mode | Behavior |
| --- | --- |
| New folder (default) | Restore into a separate new subfolder. |
| Skip | Add missing files; leave existing files untouched. |
| Replace | Add missing files and overwrite files whose contents differ. Identical files remain untouched. |
| Rename | Add missing files; keep both copies for conflicts using `name (restored 1).ext`, etc. |

There is no separate Merge option. File/folder type conflicts are skipped in
Skip mode, renamed in Rename mode, or fail clearly in Replace mode; Replace
does not delete a destination directory. Provider errors and interrupted
transfers stop the run visibly and may leave partially restored files. Replace
uses the provider's truncate/write operation rather than an atomic file swap.

An existing PBS API token can be used instead of enrollment. Enter its full
`user@realm!token` auth ID, secret, HTTPS server origin and SHA-256 **server leaf
certificate** fingerprint. Credentials are encrypted with Android Keystore;
cloud backup and device transfer of this application's state are excluded.

**Change storage API key** retains the automatically enrolled device identity
while selecting another token for datastore access. A broader authorized key
can list and restore another host's snapshots through the global **Restore**
catalogue. Clearing both storage-key fields returns to the automatic key.
Normal PBS ownership and ACL checks continue to apply; IDs are not adopted or
reassigned. This preview uses API tokens; password/ticket login is not included.

The server presently reports Android enrollment as OS family `other` for
compatibility with the current server schema. The web UI can show the initial
enrollment, but this preview does not claim or execute server-queued desktop
agent jobs or send continuous online-status heartbeats. Backups and restores
are initiated in the app or its Android schedule. Use the app's activity status
and datastore snapshots to check results.

## Changed files and scheduling

Every completed backup is a complete snapshot of that job's selected sources.
Content-defined chunking and PBS deduplication reuse existing chunks; unchanged
content is not uploaded again. This first version still **reads all selected
files** each run. Skipping unchanged file reads by reusing metadata is a future
optimization, since document providers may report missing or unreliable mtimes.
Deleting a file from the phone removes it from the next snapshot; earlier
snapshots remain subject to PBS retention settings. There is no file syncing.

Manual runs are the default. Each job can have an optional interval in whole
hours (0 disables it) and charging requirement. Put directories in separate
jobs to schedule them independently. Android chooses the exact execution time
and can interrupt jobs. Wi-Fi-only is enabled per backup job; Settings controls
Wi-Fi-only for browsing and restores. It includes native TCP socket binding
to the selected Android network; network loss cancels an active operation.
Enrollment itself is a setup request and is not gated by that setting.

Jobs using the same host ID, datastore and namespace share one PBS snapshot
group. Each snapshot contains only the sources selected by the job that ran;
the job name is saved in snapshot notes. Use distinct namespaces if separate
retention or access boundaries are needed, and authorize each namespace in a
server host backup job when using an automatic key.

Independent per-job histories under a device namespace are proposed in
[Job histories](JOB-HISTORIES.md). Manual and scheduled runs of one job would
share that job's history. Version 0.2.2 preserves existing configured destinations
and snapshots; it does not automatically create or move namespaces.

The Restore catalogue includes host, VM and container groups. Supported root
PXAR archives can be browsed for file restores, including container groups.
Snapshots containing only disk images or other archive layouts remain visible
with file browsing disabled and guidance to use PVE/PBS restore tools. The
catalogue uses the selected storage key and reports partial listing failures;
it does not derive access from the phone's local job list.

On Samsung, consider adding PBS-X Backup to **Battery → Background usage limits
→ Never sleeping apps** if enabling a schedule. Scheduled jobs can be deferred
or interrupted by Android or Samsung battery management. A manual run uses a
visible foreground service; the Android 15+ data-sync time limit is handled by
cancelling and stopping. Failed or interrupted operations are never reported
as a successful completed backup. An interrupted restore can leave a partial
new destination folder; check activity status before using its files.

## Optional Shizuku sources

Start Shizuku and grant PBS-X permission using the button in **Settings**. In a
job's editor, enable Shizuku mode, then opt into individual sources:

- **Installed application APKs:** third-party installed packages, including
  base and split APKs, under `Installed-APKs/<package>/<index>.apk`.
- **Android/data:** shell-readable external app files under
  `/storage/emulated/0/Android/data`.
- **Android/obb:** shell-readable expansion files under
  `/storage/emulated/0/Android/obb`.

The UserService is read-only and restricts reads to those roots and package
manager-reported APK locations. There is no arbitrary command or path API.
Permission loss, inaccessible directories, duplicate names or oversized Binder
directory listings fail visibly; disable an unavailable source and retry.
Shizuku is only contacted when enabled with a selected Shizuku source.

Unrooted Shizuku uses ADB shell privileges. It **cannot read arbitrary private
app data** under `/data/user/0`, and external data/obb access varies with Android
and vendor restrictions. Running applications can also modify files during a
backup; Shizuku does not make a consistent system snapshot or quiesce apps.
APK backups contain installers, not account logins, private databases or app
permissions. Restore exports APK files to the chosen folder; it does not
silently install them or write data back into applications. Reinstallation of
a split package needs its entire APK set and a compatible Android installer
(or `adb install-multiple` with the restored set).

No root mode, SMS/contact/settings extraction, client-side encrypted PBS key
import or device-image restore is implemented yet.

## Build and tests

Use Java 21, Go 1.27.3, Android SDK 36, NDK 28.2.13676358 and Gradle 8.13 /
AGP 8.13.2. `setup-tools.sh` optionally downloads the pinned Linux tools into
ignored, **disk-backed** `.tools/`, verifies release checksums, and presents
Android SDK licenses. It changes no VM lifecycle settings. The generated AAR
and APK are ignored build outputs.

```sh
cd pbsx-android
export JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64
./setup-tools.sh
./build.sh
```

On this shared development host, use a bounded build scope:

```sh
systemd-run --user --scope -p MemoryMax=3G -p MemoryHigh=2500M \
  -p MemorySwapMax=512M -p CPUQuota=200% ./build.sh
```

The script runs Go unit/round-trip tests, native bindings, APK assembly, Java
unit tests and Android lint. The Go tests cover portable paths, streamed file
descriptors, full archive/restore round trips, deduplication, changed/deleted
files, Unicode/empty/nested files, source mutation, TLS pinning, automatic
enrollment without private-key transmission, credential selection, cancellation
and socket-binding failure for all three network paths.
Cancellation tests also exercise a blocked document-provider pipe write.

Android instrumentation adds real Keystore encryption/decryption, native Go
loading, activity launch and document-provider read/create/restore semantics.
A private fixture provider exists only in the debug variant; release builds
exclude it. Run instrumentation on a disposable emulator after building the
AAR:

```sh
./gradlew --no-daemon --max-workers=2 :app:connectedDebugAndroidTest
```

The optional native-socket instrumentation uses `core/cmd/networkfixture`, a
loopback-only HTTPS server with dummy credentials. Start it with
`go run ./cmd/networkfixture -config ../artifacts/network-fixture.json` from
`core`, then pass that JSON as the `pbsx_mock_config` instrumentation argument.
Shizuku instrumentation requires its service to be running with permission
granted to PBS-X. These optional cases skip when their prerequisites are absent;
the recorded validation supplies both. See
[the validation report](TESTING.md) for tested cases and remaining device work.
Version 0.2.2 passed all 18 instrumentation cases, including job migration,
independent Android schedules, UI navigation, live progress, catalogue discovery,
container file browsing and native selective restores through SAF in each
conflict mode.

No test uses a real person's phone data. Document providers with unknown file
sizes are rejected. Individual SAF directories are limited to 40,000 entries;
Shizuku Binder listings have a smaller explicit message-size bound. Files are
streamed; selected datasets are not staged into a second copy. Portable archives
store names, contents and reported mtimes, with synthetic file modes. Android
restore preserves contents and hierarchy, not POSIX ownership, ACLs, symlinks,
device nodes, hardlinks, or provider-unsupported timestamps.

## Research references

- [Android Storage Access Framework](https://developer.android.com/training/data-storage/shared/documents-files): user-selected trees, persistent grants and Android/data/obb restrictions.
- [Shizuku developer guide](https://github.com/RikkaApps/Shizuku-API): UserService, permission handling, and shell versus root restrictions.
- [Android network socket binding](https://developer.android.com/reference/android/net/Network#bindSocket(java.io.FileDescriptor)): bind native TCP sockets before connecting.
- [Samsung application management](https://developer.samsung.com/mobile/app-management.html): sleeping/deep sleeping app limitations.
- [Android foreground service timeouts](https://developer.android.com/develop/background-work/services/fgs/timeout): data-sync time limits.
- [Go mobile bindings](https://go.dev/wiki/Mobile): native AAR/Java bridge.

The existing pxar engine is MIT licensed; see
`../pbsx-host-agent/third_party/pxar/LICENSE`. Shizuku API is MIT licensed. This
app is part of the PBS-X repository and follows its project license; bundled
dependency licenses are listed in `THIRD_PARTY_NOTICES.md`.
