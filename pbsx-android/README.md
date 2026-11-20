# PBS-X Android preview

Native Android UI with the same vendored Go pxar/PBS engine used by the desktop
host agent. The APK contains ARM64 for phones and x86-64 for emulator testing.
Minimum Android 8; the initial device target is an unrooted Samsung S20 FE,
Android 13 / One UI 5.1, with Shizuku installed. This is an early file-backup
preview, not a full device-image or private-app-data backup tool.

## Audit follow-up: 0.5.3

Backup, restore and metadata requests now use the same HTTPS origin and port.
Use an explicit `:8007` for a direct PBS connection; an HTTPS URL without a port
uses 443. Equivalent host casing, trailing slashes and default ports share one
history encryption-policy check, including connection edits and settings imports.
Invalid known preferences and malformed legacy folder defaults are rejected
before settings are replaced. The enrollment dialog now describes namespace
grants directly. Existing 0.5.x vaults and settings exports remain compatible.

## Keys, encryption and complete settings exports

Preview 0.5.0 adds **Settings → Keys & credentials**, named connections and
backup keys, and independent **Connection** / **Encryption** selectors in each
job. Automatic enrollment adds a connection; existing API keys are supported.
Restore can select a connection and finds imported encryption keys by fingerprint.

**Create encryption key → Use custom passphrase** accepts a memorable key.
Enter exactly the same passphrase after a reset to recreate the same backup
key, without an export file. The passphrase is not saved. Random generation
and raw-key import remain available.

At the bottom of **Settings**, **Export all settings** includes every job, source selection, schedule, saved
preference, connection, credential, encryption key and activity entry. Choose
plain JSON or password-encrypted JSON. The export password is used for that
file only and is not saved. Import validates and replaces the saved state;
imported schedules stay paused until permissions are reviewed. Folder access
and Shizuku authorization must be granted on the destination device.

This test build uses a new vault format. Clear the previous 0.4.x app storage
and reconnect; no old-vault migration is included. Keep exported encryption
keys somewhere other than the device being backed up. See
[settings format and key management](SETTINGS-FORMAT.md) for details.
App settings are not a backup source in this release.

### Encryption privacy and deduplication

Existing snapshots already contain PBS chunk hashes. Keep them and run another
backup normally: the phone reads and hashes the selected files locally, then
uploads only missing compatible chunks. Encrypted backups use opaque chunk IDs
derived from both the content and a secret key, so an ordinary plaintext file
hash cannot identify a stored encrypted chunk. Reusing the same encryption key
preserves deduplication; changing keys or switching from plaintext to encryption
requires fresh encrypted chunks. Old snapshots keep their original encryption
state. Backup format/chunk-boundary changes can also reduce initial reuse.

File contents and the archive's filenames, paths and file metadata are encrypted
before upload. File-prefix hashes in resume checkpoints are inside an encrypted
envelope too. PBS still sees namespace/Backup IDs, snapshot timestamps, archive
and chunk sizes/counts, key fingerprints and repeated-chunk relationships.
Normal PBS deduplication therefore does not provide an "only total size is
visible" privacy guarantee. Choose nonsensitive namespace and Backup ID values;
administrator-entered snapshot notes are also plaintext.

The 0.5.13 source includes the privacy fix that stops encrypted jobs from
attaching their job labels/IDs as plaintext PBS snapshot notes. Labels remain in
the app's local job settings/history. This fix is not in the published
`0.5.12-preview` APK yet, and does not remove notes from older snapshots.

## Try it

1. Install `artifacts/pbsx-android-preview.apk`. Permit installation from the
   browser/file manager you use to open it. This is a development-signed build;
   keep the signing key when building upgrades.
2. In the PBS-X web UI, create a host and open **Agent → Generate enrollment token**.
   Choose the datastore and device namespace. The namespace defaults to the
   normalized host name and can be changed. A missing namespace is created;
   an existing namespace is reused. Generated keys receive backup and restore
   access to that namespace and its children. No server backup job is needed
   for Android. Paste the code into **Enroll with PBS-X code**. The private
   device identity stays on the phone. Android selects its backup sources locally.
3. Open **Jobs → New job**, name the job and select the matching datastore and
   namespace. New local jobs default to the enrolled datastore and namespace and a generated,
   editable **Backup ID**. Job name is cosmetic. **Use existing backup** selects
   a visible host history without changing your device identity. Add DCIM for camera photos/videos or other folders using **Add a
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
Editing or deleting a local job never deletes its PBS snapshots.

Restore modes reuse existing folders and keep unrelated destination files:

| Mode | Behavior |
| --- | --- |
| New folder (default) | Restore into a separate new subfolder. |
| Skip | Add missing files; leave existing files untouched. |
| Replace | Add missing files and overwrite files whose contents differ. Identical files remain untouched. |
| Rename | Add missing files; keep both copies for conflicts using `name (restored 1).ext`, etc. |

There is no separate Merge option. File/folder type conflicts are skipped in
Skip mode, renamed in Rename mode, or fail clearly in Replace mode; Replace
does not delete a destination directory. Each file is downloaded into a sibling
`.pbsx-stage-*` document, checked for complete size and closed before promotion.
Failed or cancelled downloads remove that temporary document, preserving the
original destination. This needs room for one additional incoming file and a
provider that supports file rename and deletion.

SAF has no universal atomic replacement API. Replace first renames the old file
to a unique `.pbsx-previous-*` recovery name, promotes the completed incoming
file, then deletes the previous copy. A failed promotion attempts to restore the
original name without overwriting another file. If rollback or cleanup fails,
the error identifies the retained recovery copy. A process kill or power loss
during those final provider calls can leave staging/recovery documents; their
bytes are preserved and may need manual recovery. The app never truncates an
existing destination in place. Completed earlier files remain after a later
file fails; retrying Skip adds files still missing.

An existing PBS API token can be used instead of enrollment. Enter its full
`user@realm!token` auth ID, secret, HTTPS server origin and SHA-256 **server leaf
certificate** fingerprint. Credentials are encrypted with Android Keystore;
cloud backup and device transfer of this application's state are excluded.

A saved connection’s **Change storage access key** retains the automatically enrolled device identity
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

Each job also has **Exclude files & directories**, with one backup-relative rule
per line. `DCIM/.thumbnails` skips that directory and all descendants; an exact
file path skips that file. Paths start with the selected folder's backup label,
are case-sensitive, and work with SAF and Shizuku. `*` matches within one name,
`?` matches one Unicode character and a complete `**` component spans any depth
(including zero directories), so `**/*.tmp` excludes temporary files anywhere.
Excluded subtrees are not traversed or uploaded; direct attempts to read an
excluded path are refused. Exclusions are included in settings exports and frozen
with a paused task, so later edits cannot change a resumed snapshot midway.
Existing snapshots are not modified by new exclusion rules.

Manual runs are the default. Each job can use an interval in whole hours or
selected weekdays and times, optionally in a fixed time zone. Directories can
use separate jobs for independent schedules. Android may defer the requested
time for charging, network availability or system limits. One overdue occurrence
is retained per job; missed occurrences are coalesced after completion rather
than accumulating a queue. Transfers run one at a time.

Wi-Fi-only is enabled per backup job; Settings controls Wi-Fi-only for browsing
and restores. Native TCP sockets bind to the selected Wi-Fi network. A backup
pauses when Wi-Fi is lost; a restore cancels. Scheduled backups pause when Android
stops them or charging/network requirements cease to be met, then resume the same
saved run when retried. Enrollment is a setup request and is not gated by Wi-Fi-only.

The task card offers **Pause** while running and **Resume backup** while paused.
Paused tasks remain visible without a notification; resuming starts the foreground
notification again. Job cards keep **Back up** and **Snapshots**, with no saved-run
Resume/Cancel controls. At most one unfinished task belongs to each job, and one
transfer runs at a time across the app; a paused task does not prevent other jobs
from running. Task cancellation remains on the task card.

A genuine failure finishes that activity entry as failed. The next attempt clears
its own unfinished server snapshot before creating a new task; cleanup secrets
stay private until the server confirms deletion. An uncertain completed snapshot
is verified instead of deleted. Failed tasks can also be cleared from Activity.
Android/charging/network pauses retain the original snapshot and history entry.
Resuming keeps the original sources and credentials. Job edits apply to the next
run. Manual pause holds automatic scheduling until you resume. An interrupted
file's saved prefix is checked locally; files completed before the checkpoint
are not reread. A small amount of work since the last checkpoint may be repeated
(default checkpoint interval: 64 MiB or 30 seconds).

Resume requires the matching PBS-X server checkpoint extension. Older servers
are rejected; the app never substitutes a new snapshot for a missing acknowledged
checkpoint. Recovery records are encrypted with Android Keystore independently
of optional backup encryption. Prepared device-export ZIPs stay in private app
storage until the run completes or is cancelled. Settings exports include recovery
secrets; imports hold pending runs and schedules, and cannot recreate missing
source permissions or prepared ZIP files. Cancel removes only an unfinished run
and keeps recovery information until server cleanup is confirmed. Completed
snapshots are never deleted by cancellation.

Jobs using the same datastore, namespace and **Backup ID** share a PBS snapshot
history. Each snapshot contains only the sources selected by that run; nothing
is merged with another job's contents. The job name is saved in snapshot notes.
Manual and scheduled runs of one job use the same history. Different Backup IDs
have separate histories. Renaming a job does not change its Backup ID; changing
the destination selects another history and leaves old snapshots in Restore.
Existing jobs retain their namespace and legacy device-ID backup group until
edited. Device IDs longer than 32 characters are split across namespace levels
so defaults respect PBS's component limit without truncating the identity.

Generated keys are normal PBS API tokens. Automatic grants cover the approved
namespace; additional namespaces and roles can be granted in **Permissions**.
Job edits preserve grants added manually to other namespaces. Normal PBS ACLs
and group ownership apply, including when using an existing broader key. See
[Job histories](JOB-HISTORIES.md) for details. Android 0.3.0 requires the matching
server update for custom Backup IDs with generated keys; older servers reject
those IDs rather than silently writing to the device-ID group.

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
job's editor, enable Shizuku mode, then opt into individual sources. New jobs
start with nothing selected; Shizuku-dependent options are disabled and greyed
out until its master switch is enabled:

- **Installed application APKs:** third-party installed packages, including
  base and split APKs, under `Installed-APKs/<package>/<index>.apk`.
- **Android/data:** shell-readable external app files under
  `/storage/emulated/0/Android/data`.
- **Android/obb:** shell-readable expansion files under
  `/storage/emulated/0/Android/obb`.
- **Contacts:** records, groups, photos and a standard `contacts.vcf` export.
- **SMS messages**, **MMS messages and attachments**, **Call history** and
  **Calendars and events:** independent structured-data exports.
- **Selected device preferences:** an explicit list of display/rotation,
  screen-timeout and feedback preferences. Account credentials and lock settings
  are excluded.

**All apps and external app folders** keeps the previous behavior for the three
file sources. **Choose apps** restricts APKs, Android/data and Android/obb to
selected packages/folders for that job. Device-data categories are independent
of that filter, disabled by default and require Android 12 or later. Missing
permissions or providers fail the selected export visibly; records are not
silently skipped. Older jobs keep All apps and have no device exports enabled.

SD-card folders already work through the ordinary folder picker. Shizuku is not
required to select those folders, and no separate SD-card mode was added.

Device exports appear under `Device-data/<category>.zip`, with versioned JSON
records and binary attachments; contacts also include `contacts.vcf`. Restoring
them uses the normal file restore flow. **Automatic import into Android's live
contacts/messages/calendar databases is not implemented.** The vCard can be
imported manually using a contacts application. See [Export format](EXPORT-FORMAT.md).

Unlike ordinary file sources, provider exports need temporary app-private cache
so the streaming backup engine knows each archive's size. This preview allows
up to 512 MiB combined exports per run, reserves 32 MiB of free space and times
out a category after ten minutes. It also limits provider records/field sizes.
Preparation shows the active category and bytes prepared; cancellation stops
the reader. Cache files are removed on normal completion/failure/cancellation;
orphaned files from process termination are removed on the next app start.

The UserService is read-only. File access is restricted to those roots and
package-manager-reported APK locations; exports use fixed provider categories.
There is no arbitrary command, path, SQL or provider-URI API.
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

Private app databases, RCS/private messenger databases, root backup and
device-image restore remain outside this preview. Secure Folder support will
require running the app inside that profile, with encryption planned separately;
it is not enabled by these additions. Client-side encrypted PBS key import is
also not implemented.

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
[the validation report](TESTING.md) for current instrumentation results and
remaining device work. Covered cases include job
migration, independent Android schedules, UI navigation, live progress, catalogue
discovery, container file browsing and selective SAF restore conflict modes.

No test uses a real person's phone data. Document providers with unknown file
sizes are rejected. Individual SAF directories are limited to 40,000 entries;
Shizuku Binder listings have a smaller explicit message-size bound. Files are
streamed; ordinary file datasets are not staged into a second copy. Structured
provider exports use the bounded private staging described above. Portable archives
store names, contents and reported mtimes, with synthetic file modes. Android
restore preserves contents and hierarchy, not POSIX ownership, ACLs, symlinks,
device nodes, hardlinks, or provider-unsupported timestamps.

Custom Android Backup IDs are visible in this app's Restore catalogue,
Datastore → Content and the server's Host → Backups view. The host page discovers
accessible histories in the standard device namespace, groups owned by current
or historical generated device keys, and legacy groups whose ID matches the
device ID. It does not depend on enabled server jobs. A manual shared key with
a custom ID outside the standard device namespace has no recorded device
association; use the global Restore catalogue or Datastore → Content for those
groups. The host page displays each actual Backup ID and preserves it when
queueing a restore to a desktop agent.

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
