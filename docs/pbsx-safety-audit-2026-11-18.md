# Safety audit and fixes, 2026-11-18

Three review cycles covered release publication, Android restore integrity and
desktop credential selection, followed by a review of the fixes and CI coverage.
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

Five added Python regressions cover same-version rebuilds, adoption of an
existing stable repository, Debian-equivalent versions and partial bootstrap.
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

## Validation and limits

Local validation covers the complete Linux host-agent race suite, Android Go
core race suite, shared datastore/transfer/backup-proxy race suites, publication
and APT-guard regressions, Python compilation and patch whitespace checks.
Build parallelism and Go memory use are bounded. Hosted checks provide the
native Windows and Android build validation for the audit branch.

These are source fixes. Existing public package, app and ISO release bytes are
unchanged; distributing the fixes requires a subsequent verified release.
Physical Samsung acceptance and bare-metal recovery remain separate tests.
An audit covers the paths reviewed and the failures exercised; it does not
establish that all possible data-loss or recovery cases have been eliminated.
