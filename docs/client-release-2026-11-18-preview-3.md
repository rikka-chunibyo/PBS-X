# Client preview 2026.11.18.3 publication

Published to [public downloads](https://pbsx.rikka.ca/downloads/) and
[GitHub](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.18-preview.3):

- Android `0.5.8-preview`, version code 17.
- Windows/Linux desktop, background agent and CLI remain `0.7.4-preview`.
- Android source and publisher commit: `491c5b6d73793757c2d1cdaed5ef685b842b6f3e`.
- Native source remains `dea31770b41fe8421a3170ff424c3d752d7bb13b`.

Android jobs now offer **Add folder via Shizuku** for Download and other internal
shared-storage folders. Selections are independent of the app filter, saved per
job and included in settings exports. The existing encryption and backup engine
handles their contents. See [usage and boundaries](android-shizuku-folders.md).

## Validation

- [Android checks 44413310482](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44413310482)
  passed both APK variants, universal Go bindings, Java tests, lint and seven
  Android 13 instrumentation tests with no skips. The instrumentation covers
  SAF restores, job serialization/validation, complete settings export/import,
  and disabled/enabled Shizuku controls.
- The clean local signed build passed all ten Java unit tests, Go tests, lint,
  certificate, universal-ABI, non-debuggable and 16 KiB alignment checks. The Go
  core race suite also passed.
- Actual Samsung Download reads through Shizuku remain pending device testing.
  An optional Shizuku instrumentation test creates its own Download fixture,
  checks exact reads/access boundaries, and removes its own file.
- Native bundles reuse successful [run 44395702832](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44395702832).
  Publication verified that their application/build inputs match current source.

The publisher verified staged files and GitHub digests before activating the
immutable directory. All five files were subsequently downloaded from both
public HTTPS and authenticated GitHub and matched the prepared bytes. The
landing page's normal redirect, latest page and latest metadata select preview.3.
The existing Android signing certificate is retained; install over the previous
app to retain settings and keys.

APK SHA-256:
`9b57473858aca5b72ad2af0d46ffa62e8359485328110402a7684e8292035d25`.

[Checksums](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.18-preview.3/SHA256SUMS)
and [build metadata](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.18-preview.3/release.json)
record exact file hashes and provenance. Server APT packages and ISO bytes were
unchanged. No physical phone or server VM was modified.
