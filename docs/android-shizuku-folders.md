# Shared folders through Shizuku

Android 11 and later prevent selecting the Download directory itself through
the system folder picker. A job can instead use **Optional Shizuku sources →
Enable Shizuku for this job → Add folder via Shizuku → Download → Use this folder**.
Start Shizuku and grant PBS-X permission in Settings first. The normal Add folder
button remains available for other folders and SD cards.

The second picker browses directories in the app's current Android profile's
internal shared storage. Select a directory, optionally rename its label in the
backup, and save the job. Add several folders if needed. No folder is selected
by default. Disabling Shizuku preserves selections but disables their controls
and excludes them from runs, consistent with the other Shizuku options.

These sources do not use the All apps/Choose apps filter. Selecting Download
alone does not include APKs, Android/data, Android/obb, or device exports.
Those remain separate explicit sources. Android's private app data and Secure
Folder are outside this picker. Shizuku access still depends on the Android
version and device policy; denied/missing selected folders fail the backup.

Job JSON stores `shizuku_folders` as an array of `{ "label": "Downloads",
"path": "Download" }` objects. Paths are relative to the current profile's
shared-storage root, not absolute filesystem paths. Settings export/import
includes them. A label must be unique across both folder pickers and must not
collide with generated sources. Symlinks, traversal, storage-root selections,
and the Android subtree are rejected; use the dedicated app-folder controls
for Android/data and Android/obb.

Files stream through the existing read-only Shizuku service and backup engine;
optional backup encryption, chunk deduplication and restore selection operate
as for ordinary folder sources. Restoring still uses the existing destination
picker and conflict modes. This feature does not add privileged restore writes.

Coverage includes JVM path/symlink checks; Android job serialization, source
counting, disabled-source handling, malformed imports and label/path collisions;
settings export/import; and editor control defaults. The optional Shizuku
instrumentation test creates its own MediaStore Download fixture, verifies
exact bytes through AndroidDocuments, checks picker listings and denial paths,
and deletes only its own fixture file. Run that test on a disposable emulator
with Shizuku started and PBS-X authorized, not on a personal phone without
permission to create test files.
