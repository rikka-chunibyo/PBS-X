# Client preview 2026.11.19.1 publication

Published Android **0.5.11-preview** (version code 20) to
[public downloads](https://pbsx.rikka.ca/downloads/) and
[GitHub](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.19-preview.1).
Windows/Linux remain **0.7.5-preview** using their previously validated native bundles.
Android source/publisher commit: `ef54fdf1da247d1f07920f9c29827f5c5d715c00`.
Native source: `720c46cfaedf884c70a1bf6cd8074872cb598819`.

## Restore selection changes

Select all now selects supported immediate children individually across every
page of the current directory, instead of adding the directory itself as a
single selected path. Each checkbox in that directory stays editable. Directory
checkboxes retain their normal recursive meaning. Empty directories retain a
separate Select empty folder action.

The removal prompt replaces Keep selection with Invert selection. Invert toggles
its removal checkboxes while leaving the prompt open; Remove selected applies
the removal. Dismissing the prompt preserves the original restore selection.

Bulk selection preserves the previous selection on errors, cancellation or
existing selection-limit failures. It skips unsupported entries and retains
the existing 1,000-path and selection-size limits. Restore modes and destination
write behavior are unchanged.

## Verification

- The clean signed build passed Go tests, all ten Java unit tests, both APK
  variants, lint, pinned certificate, universal ABI, non-debuggable release and
  16 KiB alignment checks. The instrumentation APK also compiled before release.
- [Android CI 44491284578](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44491284578)
  passed its build/lint/unit checks and all seven existing Android 13
  instrumentation tests with no skips. These are the existing SAF, jobs,
  settings-transfer and control-state regressions; physical phone interaction
  testing of the new selection behavior was not performed.
- Native bundles reuse successful [run 44448795381](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44448795381).
  Publication checked their source freshness and all prepared asset hashes.

All five release files were downloaded independently from public HTTPS and
authenticated GitHub after activation and matched the prepared bytes and GitHub
digests. The downloads landing redirect, latest page and latest metadata select
this release. Android retains the established signing certificate and can be
installed over the existing app to preserve settings and keys.

APK SHA-256: `e0492e30f8497ded9873faf153a12c1f557a01a0da8cf5382362c4bb19253d40`.

[Checksums](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.19-preview.1/SHA256SUMS)
and [build details](https://pbsx.rikka.ca/downloads/releases/pbsx-clients-2026.11.19-preview.1/release.json)
record exact hashes and provenance. No phone, live restore or PBS server VM was
modified; server APT packages and ISO bytes were unchanged.
