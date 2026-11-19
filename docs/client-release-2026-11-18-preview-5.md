# Client preview 2026.11.18.5 publication

Published Android **0.5.10-preview** (version code 19)
and Windows/Linux **0.7.5-preview** to [public downloads](https://pbsx.rikka.ca/downloads/)
and [GitHub](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.18-preview.5).
Source/publisher commit: `720c46cfaedf884c70a1bf6cd8074872cb598819`.

## Upload-counter correction

Android's data-mode backup streams through UploadSplitArchive, which calls
UploadArchive concurrently for metadata and payload. UploadArchive never invoked
OnUploadProgress. Consequently the Android display stayed at zero uploaded
bytes even while PBS was receiving gigabytes of data.

PBS sessions now accumulate acknowledged encoded-chunk bytes across ordinary,
split and interleaved uploads. Callback delivery is serialized independently of
session state so concurrent streams produce monotonic totals. Deduplicated
chunks advance processed counts without increasing uploaded bytes, and failed
chunk uploads do not report successful transfer bytes. Subsequent metadata and
catalogue archives continue the same totals. Backup bytes, encryption, index
publication and completion handling are unchanged.

## Verification

Two new regressions reproduced the original missing updates before the fix.
Five new test functions cover live progress before EOF, concurrent split streams,
plain/encrypted-compressed encoded-byte totals, deduplication, failure handling
and interleaved-to-ordinary cumulative totals. Shared datastore/transfer/backup
race tests and Android core race tests passed locally.

- [Native CI 44448795381](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44448795381)
  passed Linux and Windows tests, shared-module race suites, native bundles,
  installer validation and the Linux desktop window smoke test.
- [Android CI 44448795350](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44448795350)
  passed its build, lint, Java tests and seven Android 13 instrumentation tests
  with no skips. The clean local signed APK also passed Go/Java tests, lint,
  pinned certificate, ABI, non-debuggable and 16 KiB alignment checks.
- [Publication CI 44448795431](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44448795431)
  passed the publication/source/durability regressions.

The publisher checked component source freshness and GitHub asset digests before
activating the immutable release. All five public files were then downloaded
independently from HTTPS and GitHub and matched their prepared bytes. Latest
metadata and the downloads page selected preview.5.

APK SHA-256: `e9dd9408307b01a6bbb7a6f8a37818a17117ba6a7ef6d64854bb0fe559978af1`.

The existing Android signing certificate is retained. Let a running backup
finish before installing the update; updating Android can stop its process.
The old running app retains its old counter until updated. This publication
does not restart or modify the user's running backup, phone or PBS server.
Server APT packages and ISO bytes were unchanged.

[Checksums](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.18-preview.5/SHA256SUMS)
and [build metadata](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.18-preview.5/release.json)
record exact hashes and provenance.
