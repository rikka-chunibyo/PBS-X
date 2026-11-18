# Safety audit and fixes, 2026-11-18

Three review cycles covered release publication, Android restore integrity and
desktop credential selection, followed by a review of the fixes and CI coverage.
Branch: `pbsx-safety-audit-20261118`.
All fixtures use disposable files and dummy credentials. No live phone, server
VM, repository container or hypervisor was changed during this audit.

## Release publication

ISO preparation previously checked downgrades only against an existing ISO.
Adding the first ISO to a populated APT feed could therefore prepare older
packages. It could also prepare rebuilt bytes under an unchanged package
version, even though reprepro retains the already published version's bytes.
The final activation check detected the mismatch only after repository work.

Preparation now compares the candidate against both the live stable package
index and the previous ISO. It rejects downgrades and changed content/identity
under an equivalent Debian version before public staging or pending-transaction
creation. Initial empty and partial repositories remain supported. Maintainers
must increment a changed package's version; equivalent version spellings are
not an escape from package immutability.

Six added Python regressions cover same-version rebuilds, adoption of an
existing stable repository, Debian-equivalent versions, partial bootstrap and
version-comparison errors. A follow-up damaged-index probe showed that dpkg's
error exit was being treated as a false comparison; it now stops preparation.
The three original failing cases were reproduced before the fix.

## Android restore integrity

Android previously selected archive format by attempting a split-index download,
then trying a legacy archive on failure. It also maintained a weaker duplicate
of the desktop dynamic-index verifier, which omitted offset validation.

Android and desktop now share the same manifest-driven root-index loader and
index checks. A declared split archive requires both streams and never falls
back after a missing or failed download. Duplicate entries, incomplete/mixed
split archives, checksum/size mismatches, non-increasing or oversized offsets,
and empty metadata indexes fail before restore writes. Legacy archives remain
supported when the manifest declares them. Omitted and explicit `none` modes
are treated equivalently.

Android additionally rejects unknown manifest/file encryption modes and
duplicate filenames. `sign-only` entries require key selection and a valid
manifest signature, just as encrypted entries do; a missing signature no longer
lets them take the unsigned path. Historical-key selection and device-independent
passphrase derivation are unchanged.

Regression coverage includes real TLS-upgrade/HTTP2 Android reader sessions
where either split stream is unavailable and a legacy archive also exists.
The tests assert an error and zero legacy download requests. Additional shared
tests cover declared format selection, protected/plain modes, duplicate entries
and split preflight. Android tests cover signature/mode validation and invalid
offsets with otherwise matching checksums.

## Credential selection and follow-up review

The desktop agent previously ignored errors reading `StorageCredentialMode`,
then fell back to automatic credentials. A registry I/O or permission failure
could therefore select a different credential than the configured custom key.
The agent now stops on mode-read errors, retaining the underlying error.
A specifically missing legacy value still selects automatic credentials;
existing custom-key refusal behavior remains intact. Regressions assert that
no credential is even read after a failed policy lookup.

The follow-up found that `go test ./...` in the host module does not execute
tests in the nested shared PXAR module. Linux and native Windows CI now run its
datastore, transfer and backup-proxy suites explicitly. ISO workflow path filters
also include the APT publisher and its tests, and ISO checks run those tests.

The expanded native Windows run exposed seven tests using colon-containing PBS
timestamp directory names. Five session/index tests now use portable fixture
paths because their explicit-path APIs do not depend on PBS timestamp names.
The two actual on-disk PBS snapshot-layout tests (`TestBackupGroupRoundTrip` and
`TestBackupInfoFiles`) explicitly skip on Windows, with the POSIX filename
requirement as their reason. They continue to run on Linux. Setup operations
in those tests now check errors instead of silently ignoring failed writes.
The Windows app's remote PBS, archive, credential and restore tests remain enabled.
The direct-store manifest test now exercises both default and durable writes.
On Windows, a permission error specifically from required directory fsync must
leave the completed manifest absent. The default-write case still completes
and checks its manifest/index; Linux exercises both successful publication modes.

## Validation and limits

Local validation passed the complete Linux host-agent race suite, Android Go
core race suite and shared datastore/transfer/backup-proxy race suites. All
85 Python tests passed: 26 combined-publication, 14 ISO, 11 APT-publication,
16 client-publication, seven Android-source and 11 installed APT-guard tests.
Python compilation and patch whitespace checks passed. Build parallelism and
Go memory use were bounded.

Hosted validation passed:

- [Windows/Linux checks 44258970570](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44258970570):
  native agent tests, shared archive/transport suites, both desktop bundles,
  installer checks and the Linux window smoke test.
- [Android checks 44258970444](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44258970444):
  real universal Go bindings, debug/release APK builds, Java tests, both lint
  variants and SAF provider instrumentation on an Android 13 emulator.
- [Publication checks 44258970484](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44258970484):
  corruption/refusal tests, durable host completion, Android Go race tests and
  Java restore promotion/rollback tests.
- [ISO checks 44257639390](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44257639390):
  all 26 combined-publication cases plus ISO and APT protection regressions.

The client runs used source `83f154ea96223fe4614cbcb44b2264d432581a97`.
The ISO run used `8854fb585d98a986b163bbbcbf7752d6b1fb2c2d`; subsequent
commits changed only shared Go test fixtures and this report. Rust server code
was unchanged and was not rebuilt; its existing ignored integration tests were
outside these client/publication review cycles.

These are source fixes. Existing public package, app and ISO release bytes are
unchanged; distributing the fixes requires a subsequent verified release.
Physical Samsung acceptance and bare-metal recovery remain separate tests.
An audit covers the paths reviewed and the failures exercised; it does not
establish that all possible data-loss or recovery cases have been eliminated.
