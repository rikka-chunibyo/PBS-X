# Sparse restore and incremental reuse fixes — 2026-11-17

These changes address the follow-up review of client preview.2. They are local
source changes and require a new release before public downloads include them.

## Sparse restores preserve the verified payload

Normal restore now reconstructs sparsity while writing the fresh sibling
temporary file. The saved hole map identifies candidate ranges, but the writer
seeks over a candidate only when the incoming verified bytes are all zero.
Nonzero bytes inside a stale map are written normally. There is no later
hole-punch or zero-data operation that can overwrite restored content.

The temporary file is truncated to the verified logical length (including any
trailing hole), synced, closed and then promoted. Invalid sparse metadata,
content read/write failure, a short stream or a sync failure cannot replace an
existing destination. Sparse processing uses the incoming buffers and does not
perform a second scan of the restored file.

Windows marks the temporary file sparse before streaming. Unsupported
filesystems use ordinary dense writes. Linux and other Unix systems seek over
verified zeros; filesystems without sparse allocation still preserve logical
bytes. Content-only restore retains its existing behavior.

## Metadata reuse uses archive paths and verified previous snapshots

Live source paths and archive paths are now separate lookup coordinates.
For example, `./sub/file`, `/root/sub/file` and `C:/Users/device/sub/file`
all refer to `/sub/file` in the corresponding archive catalog. Unchanged files
can reuse previous payloads, while new or changed files are read from the source
and deleted entries remain absent from the new snapshot.

The plaintext manifest/index/chunk verifier is shared between desktop restores,
backup recovery and incremental backup. Before starting an incremental upload,
the engine verifies the previous snapshot's identity and completed manifest,
root index checksums, sizes and offsets, and the metadata chunks used to build
its catalog. Downloaded payload chunks and chunks copied between local stores
are verified as well. Invalid prior data returns an error instead of becoming a
new committed backup. Previous readers and active upload sessions are closed on
failure.

Whole chunks already present in a remote datastore can still be reused by
verified index reference without downloading their entire contents. This is
incremental reuse, not a full datastore verification pass. Signed/encrypted
previous snapshots continue to require encryption-aware support; Android's
existing encrypted data-mode pipeline is unchanged.

Standalone local stores expose their plain `index.json` through the verifier's
blob interface. An existing damaged `index.json.blob` never falls back to a
different manifest.

## Regression coverage and validation

Seven new test functions cover:

- A real direct-source sparse file changing after hole scanning, followed by
  the actual backup and hash-verified TLS/H2 restore pipelines. Both normal and
  content-only restore preserve the updated source bytes.
- Sparse stream writes crossing hole and buffer boundaries, mixed data within
  stale ranges, trailing-zero file lengths and output errors.
- Invalid sparse metadata retaining the pre-existing destination and cleaning
  up its temporary file.
- Incremental backups with relative, absolute Unix and Windows-style source
  roots, nested unchanged files, changed/new files and deleted files. Read
  counters assert that unchanged files are never opened.
- Missing/wrong-identity manifests, damaged indexes, a self-consistent index
  that disagrees with its manifest, and valid-CRC chunks with wrong SHA-256
  digests. These cannot commit a new snapshot.

The complete Linux host-agent race suite, shared PXAR backup/datastore/transfer
race suites and Android Go core race suite passed. Windows agent/CLI binaries
and test binaries for the changed packages cross-compiled successfully. Test
runs use one build worker, `GOMAXPROCS=1` and the Go soft memory limit
`GOMEMLIMIT=256MiB`.

No physical device, hypervisor or server VM is needed for these regressions.
Native Windows runtime checks remain for the next hosted CI run; cross-building
does not substitute for executing them.
