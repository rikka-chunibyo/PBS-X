# Android overall backup progress — deferred

Status: deferred at the user's request. Keep the existing per-file progress and
processed-file/byte counters; do not implement an overall bar yet.

The desired feature is an overall progress bar alongside the current-file bar,
without adding significant computation, startup delay or backup slowdown.
Using the previous backup's size as the primary denominator was not preferred.

## Research findings

- **Restic:** counts files and sizes concurrently with backup. This avoids
  waiting for counting before starting, but adds metadata I/O. Its `--no-scan`
  option disables progress estimation to avoid that overhead.
  [Restic tuning documentation](https://restic.readthedocs.io/en/stable/047_tuning_parameters.html)
- **Duplicati:** its normal path runs a parallel file counter alongside backup;
  some modes fall back to previous-backup totals.
  [Backup implementation](https://github.com/duplicati/duplicati/blob/master/Duplicati/Library/Main/Operation/BackupHandler.cs)
- **Borg:** archive creation reports processed sizes, file count and current
  path, without an overall completion percentage.
  [Borg create documentation](https://borgbackup.readthedocs.io/en/stable/usage/create.html#description)
- **Proxmox VM backups:** QEMU supplies disk/block totals and dirty-block
  information. This is different from discovering arbitrary Android files.
  [Proxmox backup implementation](https://github.com/proxmox/qemu-server/blob/master/src/PVE/VZDump/QemuServer.pm)

## Android options to investigate

1. **Folder metadata:** Android 10/API 29 introduced
   `DocumentsContract.getDocumentMetadata()`, `METADATA_TREE_SIZE` and
   `METADATA_TREE_COUNT`. Providers may return folder totals, but support and
   performance depend on the provider. Android 13's standard filesystem
   provider uses `Files.walkFileTree()` internally, so this is not a guaranteed
   instant-size lookup. It may reduce cross-process overhead. Samsung's actual
   implementation and timings have not been tested.
   [API documentation](https://developer.android.com/reference/android/provider/DocumentsContract#METADATA_TREE_SIZE),
   [Android 13 implementation](https://github.com/aosp-mirror/platform_frameworks_base/blob/android-13.0.0_r1/core/java/com/android/internal/content/FileSystemProvider.java#L158-L193)
2. **MediaStore:** the existing media index exposes file sizes and could provide
   a faster count for explicitly media-only sources. Its visibility, permissions
   and indexing coverage must match the backup selection; do not use it as a
   complete inventory of arbitrary mixed folders or silently omit unindexed files.
   [Android media documentation](https://developer.android.com/training/data-storage/shared/media)
3. **Shizuku:** direct enumeration of permitted paths may reduce document-provider
   overhead, but still requires traversing and counting those files. SD-card
   access through the ordinary picker does not require Shizuku.

## Before revisiting implementation

- Measure ordinary SAF and Shizuku enumeration, plus provider folder-metadata
  calls, on the Samsung S20 FE / Android 13 / One UI 5.1 and SD card.
- Compare backup alone against backup with concurrent metadata counting; record
  startup latency, total duration, CPU use and provider-query counts for both
  many small files and large media files. No timings have been collected yet.
- Investigate sharing directory listings between counting and backup, with
  bounded memory and without weakening source validation.
- If counting is worthwhile, start backup immediately, display “Calculating
  total…” while counting, then use this run's measured total alongside the
  per-file bar. Use logical bytes processed rather than uploaded bytes, since
  deduplication changes upload volume. Display snapshot finalization separately.
- Account for changing files, unsupported/incomplete metadata and cancellation.
  Provider totals must never determine which files are actually backed up.

These are future evaluation steps, not an active implementation plan.
