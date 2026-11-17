# Client publication validation — 2026-11-17 UTC

Release: `pbsx-clients-2026.11.17-preview.1`.
Public page: https://pbsx.rikka.ca/downloads/ .
GitHub prerelease: https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.17-preview.1 .
GitHub access requires repository permission; public HTTPS downloads do not.

## Published builds

- Windows amd64 desktop, static CLI and background agent: `0.7.0-preview` ZIP.
- Linux amd64 desktop, static CLI and background agent: `0.7.0-preview` tarball.
- Universal Android ARM64/x86-64 APK: `0.5.3-preview`, version code 12.
  The public APK is release-mode, with debugging disabled and the existing
  preview signing certificate retained. No phone was installed or modified.

The desktop bundles were collected from successful native
[CI run 44002908307](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44002908307),
source `b74ed993e0d3d774596fe435abbdb454cc50d092`.
The publisher verified that the host-agent sources, installer sources and bundle
builder had not changed since that run. The separate updated version-input
workflow also passed in
[run 44007692620](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44007692620),
including Linux races, native Windows tests, GUI builds, installer/CLI checks
and Linux window rendering.

Android was rebuilt from committed source
`6bc55a20b09cd1589621de88ce5908156b04a277`. Go core tests, debug and release Java
unit tests, both variant lint checks, APK signing, certificate continuity,
16 KB alignment, universal ABI coverage and the non-debuggable check passed.
The production application logic is unchanged; release mode also disables
Shizuku's user-service debugger flag. Physical-device acceptance of this new
packaging was not repeated.

## Publication checks

- Nine publisher regression tests passed: corruption refusal before activation,
  immutable tags, idempotent retries, private-file exclusion, safe source paths,
  symlink rejection, checksum-index coverage, CI-lost execute bits and draft
  GitHub release lookup. Read-only publication CI passed in
  [run 44008608408](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44008608408).
- Ten existing host-bundle safety tests passed.
- The generated Linux tarball was extracted and passed the real installer's
  `--check`; its headless CLI printed the expected version. The Windows ZIP
  passed its CRC check; its executable bytes match the native-checked manifest.
- All five GitHub uploads reported `uploaded`, correct lengths and matching
  GitHub server-computed SHA256 digests. The prerelease is no longer a draft.
- The repository host rechecked staged file hashes, created an immutable release
  directory and atomically switched `downloads/latest`. Only allowlisted public
  assets were copied. The old repository homepage was backed up privately before
  adding its Apps/ISO links.
- All three app files, `release.json` and `SHA256SUMS` were downloaded through
  normal public HTTPS and matched the prepared release byte for byte. Two
  initial desktop transfers reset; sequential HTTP/1.1 retries passed.
- The root homepage, download entry page and current platform listing returned
  successfully. Existing APT and ISO files were preserved.

`SHA256SUMS` and `release.json` at the immutable public release path record the
exact published hashes, sizes, source commits, CI run and Android certificate.
The full `release --build-android` command was exercised; its initial draft tag
lookup exposed GitHub's unpublished-draft API behavior. The lookup now uses the
numeric release ID discovered by GitHub CLI, with a regression test, and
publication resumed without rebuilding the already verified APK.

No signing keystore or SSH private key was copied to CI or public downloads.
The existing local GitHub/SSH access performs explicit maintainer publication.
See [the publishing guide](pbsx-client-downloads.md) for repeatable commands.
