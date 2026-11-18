# Client publication and follow-up audit, 2026-11-18

Published [client preview 2026.11.18.1](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.18-preview.1)
to [the public downloads](https://pbsx.rikka.ca/downloads/):

- Windows/Linux desktop, background agent and CLI: `0.7.3-preview`.
- Android: `0.5.6-preview`, version code 15, with the existing pinned certificate.
- Native/client source: `e0c0fc15a3aae801a2e3e6f7813df34cf89055b3`.
- Publisher source: `16d4b09afd6337e57840497f1028184d22e7eebd`.

The tested safety-audit branch was fast-forwarded into `pbs-x`. Downloadable
Android updates retain their established signer and app data. All five public
release files were independently downloaded over normal HTTPS and matched the
prepared bytes and GitHub's server-computed digests. The GitHub release is
visible as a prerelease, not a draft. Previous immutable downloads were retained.

## Validation and repository deployment

- [Native Windows/Linux checks 44386214984](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44386214984)
  passed both native builds, tests and bundle checks.
- [Android checks 44386215038](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44386215038)
  passed universal Go bindings, both APK variants, Java tests, lint and Android
  13 SAF provider instrumentation. The local release build separately passed
  signature, pinned-certificate, alignment and non-debuggable checks.
- [Publication checks 44387250968](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44387250968)
  passed the new publisher regressions, existing source/provenance checks,
  durable host completion tests, Android Go race tests and Java rollback tests.

The audit's repository-helper fix was deployed with a verified rollback copy
under
`/var/lib/pbs-x-apt/release/backups/iso-helpers-audit-publication-20261118-e0c0fc15a/`.
Read-only checks against the installed helper accepted the current package set
and rejected changed bytes under each of the four current package versions.
The deployed helper hash and the previous-source backup hash were checked.
No pending APT/ISO publication remains.

The current signed public APT index was authenticated with the tracked archive
key and compared with all four ISO input hashes. It still matches server,
client and file-restore `4.2.9-1+pbsx2` and repository support `1.0.3`.
These package bytes and the current ISO are unchanged. This validation did not
redownload the entire unchanged ISO or perform a new VM/physical installation.
No hypervisor, server VM or phone was modified; deployment used only the
repository container.

## Publisher defects fixed during this review

1. **Client activation lacked durability barriers.** The file-copy and final
   pointer-renaming sequence could report publication success without flushing
   the payload files and their directory entries. Injecting a file flush failure
   reproduced a switch to the incoming release before the error was returned.
   Activation now flushes all files and the staged directory, commits parent
   directory entries, switches `latest`, and flushes that pointer change.
   Directory-flush failure before promotion preserves the previous pointer and
   supports retrying the immutable prepared directory.
2. **Client SSH policy depended on local defaults.** Client publishing did not
   explicitly require strict host-key checking, unlike server publishing.
   SSH and SCP now both set `StrictHostKeyChecking=yes`; regression coverage
   verifies every connection command.

The relevant publication suite now has 18 passing tests. The injected failure
cases and host-key assertion failed before these fixes. The newly published
release used the corrected activator.

## Direct-local datastore finding (subsequently fixed)

**P2 at review: the direct-local datastore helper did not flush snapshot directory
renames under `SyncWrites`.** In
`pbsx-host-agent/third_party/pxar/backupproxy/store.go`,
`localSession.publishSnapshotTemp` flushed the temporary file and renamed it,
then returned without syncing `s.baseDir`. A process could therefore acknowledge
durable index/manifest publication before those new directory entries are
durable; power loss could lose them.

A syscall trace of the existing disposable
`TestDatastoreStorePublishesManifestLastWithoutLoadingReusedChunks/durable`
fixture confirmed file flushes and successful index/manifest renames, followed
by test success without a snapshot-directory flush. The trace did show the
separate chunk-directory flush, distinguishing the missing step. This was a
trace of an isolated test, not a power-loss experiment.

No production caller of `NewDatastoreStore` was found in this checkout.
The shipped Android and desktop backup runners use `NewPBSStore` and the remote
PBS protocol. The finding did not affect that active write path.

The subsequent source fix flushes the snapshot directory after each durable
index/manifest rename and propagates open/flush errors. Default writes retain
their existing behavior. Real Unix permission failures now test index and
manifest publication, verify that `Finish` returns no successful manifest, and
check temporary-file cleanup. These permission probes require a non-root Unix
user; root runs explicitly skip them because root bypasses the tested denial.
The existing Windows durability probes also accept a refusal at the new index
directory barrier, while default publication continues to complete.

The shared datastore, transfer and backup-proxy race suites pass. A new syscall
trace confirms a successful snapshot-directory `fsync` immediately after each
of the two index renames and the final manifest rename. This is source
hardening after the client release above; it does not rebuild or change that
immutable release's files.
