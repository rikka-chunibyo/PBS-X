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

## Publication audit follow-up

Fix commit: `d62f82be54ece679ad01b017b5598c909a3e3ec3`.

- ISO dependency overlays cannot replace the three PBS-X core packages, the
  repository package or the generated installer defaults. Final package hashes
  must still match the selected release inputs.
- Android build records include shared local Go module replacements, including
  PXAR. Capture before compilation and verification afterward prevent stamping
  changed sources onto an older APK. Legacy records require a fresh build.
- Retrying an older client publication preserves a newer `downloads/latest`.
  Rollback requires an explicit flag; same-tag retries leave the symlink intact.
- Interrupted GitHub uploads recover only empty draft `starter` assets after
  refreshing their state. Completed assets with matching hashes are reused.

All 58 lightweight regression tests passed: 16 publication, seven Android
source-record, 14 ISO, ten host-bundle and 11 APT guard tests. Source capture
also resolved the real Android Go module graph with workspace overrides disabled.
Publication checks passed in
[run 44016327613](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44016327613).
The full ISO build, checksum verification and artifact upload passed in
[run 44016442643](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44016442643).
The full image build ran on GitHub's runner after the dev VM suffered another
OOM restart. A fresh Android APK build was not performed for these tooling fixes.

The private publisher helper on `pbs-x-apt` was backed up and updated to the
fix commit. A live same-tag activation retry preserved both the latest symlink
inode and the hashes of all three applications, `release.json` and `SHA256SUMS`.
The published application and ISO artifacts retain their existing release bytes.

## Second client preview — 2026-11-17

Release: [`pbsx-clients-2026.11.17-preview.2`](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.17-preview.2).
Public entry: https://pbsx.rikka.ca/downloads/ . Both client platforms were built
from `ae711fa0bc461bdc6cf4453b73fcfc34dd0cb740`, merged and pushed to `pbs-x`.
Every remote PBS-X feature branch is an ancestor of that merged main branch.

- Windows/Linux desktop, agent and CLI: `0.7.1-preview`.
- Android universal release APK: `0.5.4-preview`, version code 13. The existing
  certificate remains
  `d6f35fc4fc34b6acd83b2b2e3fd26f928339df400d1b9e256fab0181e5b3cab7`.
- [Native checks 44152411374](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44152411374)
  passed Linux races, native Windows tests, both desktop bundles, installers,
  CLI checks and the Linux window smoke test.
- [Android checks 44152411369](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44152411369)
  passed universal bindings, debug/release builds, Java tests, lint and all
  three Android 13 SAF instrumentation cases.
- [Publication checks 44152411386](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44152411386)
  passed publication regressions, Go core races and SAF promotion/rollback tests.
  The local signed Android build additionally verified certificate continuity,
  both ABIs, 16 KB alignment and non-debuggable release packaging.

This release contains the [desktop data-safety fixes](desktop-data-safety-audit.md)
and the previously unpublished Android restore/cache and Linux installer fixes.
The publisher checked all five GitHub server-computed asset digests before
private staging and atomic repository activation. The GitHub release is a
visible prerelease, and `downloads/latest` selects preview.2. Preview.1 remains
available for rollback.

All three apps, `release.json` and `SHA256SUMS` were downloaded through public
HTTPS with the normal curl client and matched the prepared files byte for byte.
The entry page's HTML redirect and `/downloads/latest/` version listing were
also verified. The first page assertion expected the listing directly at the
entry URL; checking its intended redirect resolved that test assumption without
changing the website.

No signing key was copied to CI or public storage. No phone, server VM or
hypervisor was modified. Existing APT packages and server ISO payloads remain
unchanged; this publication updates the client downloads.

## Third client preview — 2026-11-17

Release: [`pbsx-clients-2026.11.17-preview.3`](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.17-preview.3).
Public entry: https://pbsx.rikka.ca/downloads/ .

- Windows/Linux desktop, agent and CLI: `0.7.2-preview`, built from
  `0e78f4830277c91772f3b5229029d83cbd0d3036`.
- Android universal release APK: `0.5.5-preview`, version code 14, built from
  `8cab2b1b9d0bb52b5dd479ce6ea7c70cd9273497`. Its captured Android and shared
  PXAR input trees still match the publishing source. The established signing
  certificate is unchanged.
- [Native checks 44206546363](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44206546363)
  passed Linux races, actual Windows tests, both desktop bundles, installer/CLI
  checks and the Linux window smoke test.
- [Android checks 44205851527](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44205851527)
  passed universal bindings, both app variants, Java tests/lint and Android 13
  SAF provider instrumentation. The local release APK also passed certificate,
  alignment, source provenance and non-debuggable checks.
- [Publication checks 44206546427](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44206546427)
  passed the locked Rust completion tests, publication regressions, Go core races
  and SAF promotion/rollback tests.

The release includes the [sparse and incremental fixes](sparse-incremental-safety-fixes.md)
and client portions of the [completion and native read fixes](completion-source-read-safety-fixes.md).
The first runtime validation exposed Windows' deferred change timestamps;
native reads now exclude writers and deletion for the lifetime of each file
handle, with tests for existing writers, new writers and releasing protection.
The first isolated Rust CI run also exposed its dependence on an untracked root
lockfile; it now uses its own committed, locked dependency fixture. The Linux
virtual display startup failure did not recur in the successful validation run.

GitHub's asset digests and the repository activation validator verified all
five public files. Normal public HTTPS downloads using curl matched the staged
hashes for the three applications, `release.json` and `SHA256SUMS`; the
`downloads/latest` metadata and version listing select preview.3. The public
edge rejected Python urllib with HTTP 403; the normal curl client worked with
standard TLS verification. Previous immutable previews remain available.

Completion acknowledgement fixes require the corresponding server package
update; installing only these clients does not update server behavior.

The published Linux agent was additionally installed on disposable VM115,
retaining its lab credentials/service and saving the previous executable for
rollback. With the original `4.2.9-1+pbsx1` server, a real service ZFS-snapshot
backup and queued restore passed. Restored hashes, special modes, owners/groups,
ACLs, capabilities, symlink target, hardlink identity and sparse allocation
matched the source. The test job was disabled again afterward.
