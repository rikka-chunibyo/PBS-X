# Client preview 2026.11.18.4 publication

Published Android **0.5.9-preview**, version code 18, to
[public downloads](https://pbsx.rikka.ca/downloads/) and
[GitHub](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.18-preview.4).
Windows/Linux remain **0.7.4-preview** using their previously validated native builds.

The normal Android folder picker now uses the selected folder's actual display
name as the default backup label. Selecting DCIM prefills DCIM; the label stays
editable. A numbered fallback remains for providers that cannot supply a name.
Metadata is read in the background. Existing saved folder labels are retained.

Android source/publisher commit: `8341161d787a8839ab005b9618d8de1eaedf9822`.
Native source: `dea31770b41fe8421a3170ff424c3d752d7bb13b`.

## Verification

- The clean local signed build passed Go tests, all ten Java unit tests, both
  APK variants, lint, pinned-certificate verification, universal ABIs,
  non-debuggable release checks and 16 KiB alignment.
- [Android CI 44444091662](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44444091662)
  passed build/lint/unit checks and all seven Android 13 instrumentation tests
  with no skips.
- Native bundles reuse successful [run 44395702832](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44395702832).
  Publication checked their source freshness and all prepared assets.
- After publication, all five release files were downloaded independently from
  public HTTPS and authenticated GitHub and matched the prepared bytes and
  GitHub digests. Latest metadata and the downloads page selected this release.

APK SHA-256: `62ae9187c774f537d03940825e9ec7da0bed98205b38c43199c5b68943003e89`.

The existing Android signing certificate is retained. Install over the current
app to preserve settings and keys. Physical Samsung/Shizuku testing was not
performed for this publication. Server APT packages and ISO bytes were unchanged.

[Checksums](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.18-preview.4/SHA256SUMS)
and [build details](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.18-preview.4/release.json)
record exact hashes and provenance.
