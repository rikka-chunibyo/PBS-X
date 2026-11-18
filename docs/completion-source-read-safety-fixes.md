# Completion and native source-read fixes — 2026-11-17

These fixes follow the next safety review. Client changes are published in
[preview.3](client-release-validation.md#third-client-preview--2026-11-17),
with Windows/Linux `0.7.2-preview` and Android `0.5.5-preview`. Server completion
changes are published in [PBS-X `4.2.9-1+pbsx2`](server-pbsx2-publication.md). The
[sparse and incremental fixes](sparse-incremental-safety-fixes.md) are included.

## Completion acknowledgements can be retried

Backup and restore completion now consult durable history before requiring a
running command. An identical retry succeeds without changing the original
result, completion time or progress. A conflicting result fails without
overwriting history.

History is persisted before retiring the running command. If execution stops
between those operations, the next claim validates the saved completion and
retires the matching command instead of executing the backup or restore again.
Malformed history and mismatched commands remain errors. Retirement syncs the
running directory. Failure to prune old backup history no longer turns a
successfully committed completion into a failed acknowledgement.

This is a server fix; rebuilding only the agent does not fix completion retries.

## Changed native source files fail the backup

Native file readers capture size, mode, precise modification time and the
platform's change timestamp from the opened handle. They check those values
after reading and again when closing. Growth, truncation, an incomplete read,
or a changed timestamp fails the backup. Change timestamps also detect ordinary
writes whose modification time is subsequently restored on Unix. Windows can
defer timestamp updates, so native reads also deny concurrent writes and
deletion until the read handle closes. Existing writable handles cause the
native open to fail; use a snapshot to back up files held open for writing.

The archive encoder propagates source-close failures before committing a
snapshot, and failed backup sessions are closed. The reader deliberately does
not expose the underlying file's optimized copy method, which could bypass its
validation.

These checks do not reread file contents. They detect changes during a file's
read; they do not make direct backups an atomic snapshot of multiple files or a
running application. Snapshot modes remain preferable when a consistent source
snapshot is available. Filesystem timestamp resolution still matters.

## Windows modification times retain their precision

Native Windows metadata keeps FILETIME precision through conversion to PXAR,
so same-size changes within one second do not compare equal solely because the
fractional time was discarded. The conversion handles dates beyond the range
of a signed Unix nanosecond count. A process-local hint carries the precise
time without changing the legacy CBOR timestamp encoding.

## Validation

- Five Rust tests compile the actual production completion module and cover
  duplicate acknowledgements, conflicting results, interrupted completion,
  failed history writes and invalid state for both backup and restore paths.
  The client-publication workflow now runs them.
- Native reader tests cover stable/empty files, rewrites, growth, truncation,
  changes after the final read, restored modification times and a real backup
  that must not commit mixed file versions.
- Shared encoder tests assert that source-close failures prevent a committed
  manifest and close the upload session. Incremental tests check a same-size
  edit within one second and restore the updated bytes.
- Timestamp tests cover precise metadata conversion, unchanged CBOR encoding,
  and dates from 1601 through 2500. A Windows-only test exercises actual native
  stat and directory enumeration precision.

The complete Linux host-agent race suite, shared PXAR
backup/datastore/transfer race suites, and Android Go core race suite passed.
Windows agent and CLI binaries and the affected test packages cross-compiled.
Builds use one worker and capped Go memory settings.

Native Windows runtime tests subsequently passed in hosted CI. The full Debian
server package build passed its optimized workspace tests: 171 passed, zero
failed, two ignored, including all five completion tests. The candidate then
passed a signed APT upgrade on the disposable installation VM, real completion
API retry/conflict/recovery checks against old and newly generated history, and
a ZFS-snapshot service backup and metadata/hash-checked restore. See the
[server publication record](server-pbsx2-publication.md) for source provenance,
public signature/hash checks and the bounded build/test setup. Physical phone
and production-server tests remain separate from these lab checks.
