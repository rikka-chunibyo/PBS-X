# Client preview 2026.11.18.2 publication

Published to [public downloads](https://pbsx.rikka.ca/downloads/) and
[GitHub](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.18-preview.2):

- Windows/Linux desktop, background agent and CLI: `0.7.4-preview`.
- Android: `0.5.7-preview`, version code 16.
- Source and publisher commit: `dea31770b41fe8421a3170ff424c3d752d7bb13b`.

This release includes the direct-local datastore directory-flush hardening
from `2f3be65a297b1aa95f27e0e64b90cd84cc246461`. Durable index/manifest
publication flushes the renamed directory entry before reporting success and
propagates failures. The shipped clients continue to use the remote PBS write
protocol. See [the finding and its resolution](pbsx-publication-and-audit-2026-11-18.md).

## Validation

- [Native checks 44395702832](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44395702832)
  passed Linux race suites, native Windows tests, shared archive/transport
  tests, both desktop bundles, installer checks and the Linux window smoke test.
- [Android checks 44395703160](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44395703160)
  passed universal Go bindings, both APK variants, Java unit tests, lint and
  Android 13 SAF provider instrumentation.
- The local signed Android release build passed Go tests, Java unit tests,
  lint, signature verification, pinned-certificate verification, 16 KiB alignment
  and the non-debuggable/universal-ABI checks. The existing certificate remains
  `d6f35fc4fc34b6acd83b2b2e3fd26f928339df400d1b9e256fab0181e5b3cab7`.

The publisher checked source freshness and every prepared asset before upload,
checked GitHub's server-computed digests, and activated the complete immutable
directory using the durable publication path. All five public release files
were then downloaded over normal HTTPS and matched the prepared bytes and
GitHub digests. The latest page and versioned metadata selected the new release.
Earlier release directories remain available.

Checksums and source/build metadata are available at the immutable release:

- [SHA256SUMS](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.18-preview.2/SHA256SUMS)
- [release.json](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.18-preview.2/release.json)

Only client downloads were published. Server APT packages and ISO bytes were
unchanged. No hypervisor, server VM or physical phone was modified. Android
updates retain the app's signing identity and data; uninstalling is unnecessary.
