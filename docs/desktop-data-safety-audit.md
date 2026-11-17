# Desktop data-safety follow-up — 2026-11-17

Branch: `pbsx-client-releases`.

## Changes

- Backup retries exclude unfinished snapshots and snapshots whose manifest PBS
  could not read. Previous-snapshot selection uses the same filtered list.
  Recovering an already committed run now downloads its completed manifest,
  checks its host/type/timestamp, and verifies its root archive indexes before
  reporting success. A damaged recovery candidate returns an error.
- Desktop restore loads the manifest before selecting the split or legacy root
  archive. Both index checksums and manifest sizes/checksums must match. Invalid
  offsets, duplicate manifest filenames and incomplete split archives fail.
  A failed split download never silently selects a legacy archive instead.
- Every downloaded desktop restore chunk is decoded, checked against the
  requested SHA-256 digest, then exposed to the archive reader. Verification
  happens before a temporary restored file can replace its destination. The
  shared lazy reader can consume verified plaintext directly, avoiding a second
  decompression of compressed chunks. Existing chunk sources keep their prior
  decoding behavior, including Android's encryption-aware source.
- Hardlinks are created under a random sibling temporary name before replacing
  the destination. Link-creation or file-replacement failure preserves existing
  files. Temporary links are also removed when repeated restoration finds the
  destination already linked to the same inode. Failed deferred hardlinks are
  no longer registered as successful targets for subsequent aliases.

## Regression coverage and verification

Sixteen new test functions cover:

- Completed and unfinished snapshot listings, unreadable manifests, foreign
  identities, successful recovery, and manifests/indexes lost or damaged after
  listing.
- Valid split and legacy restores over the real local TLS upgrade and HTTP/2
  reader transport. A valid-CRC payload served under the wrong SHA-256 digest
  raises an error, preserves the original destination, and leaves no temporary
  restore files.
- Manifest identity, encryption modes, duplicate filenames, root selection,
  checksum/size errors, truncated indexes, and invalid chunk offsets.
- Hardlink failure with no archived fallback, immediate and deferred paths,
  real cross-filesystem EXDEV failure on Linux, replacement over a nonempty
  directory, successful replacement, and repeated restoration.
- Compressed/plain chunk verification, download errors, and the lazy reader's
  decoded-source path and verification-error propagation.

The complete Linux host-agent suite passed with `go test -race -tags agent
-timeout 5m ./...`. The shared PXAR transfer/datastore race suites passed, as did
the additional decoded-source regression and the Android Go core race suite.
Windows agent/CLI binaries and test binaries for the changed packages were
cross-compiled. Native Windows execution remains a follow-up CI check.

Tests used one build worker, `GOMAXPROCS=1`, and the Go soft memory limit
`GOMEMLIMIT=256MiB`. Loopback mock servers required execution outside the
filesystem sandbox. No remote server, hypervisor, VM, or phone was modified.

## Limits

Desktop encryption-key support is separate work. Encrypted or signed snapshots
are rejected explicitly rather than read without the required verification.
Android's existing key support is unchanged.

Recovery validation checks the manifest and root indexes without reading every
payload chunk; it is not a full datastore verification pass. Restore verifies
each chunk it actually reads. Unsigned checksums provide content-integrity
checks, not authentication against a party able to replace all backup metadata.

A hardlink restore whose filesystem cannot create links and whose archive entry
has no independent payload reports an error while retaining the destination.
It does not silently claim a successful restore or delete the original.

These are source changes; published client downloads and server images require
a subsequent release to include them.
