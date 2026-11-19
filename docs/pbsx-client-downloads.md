# Publishing and installing client previews

Public downloads: **https://pbsx.rikka.ca/downloads/**. The repository homepage
links this page. It lists the current Windows desktop/CLI bundle, Linux
desktop/CLI bundle and Android APK, plus their versions and checksums. Each
release has an immutable `/downloads/releases/TAG/` directory; `latest` switches
to a fully verified directory in one filesystem operation. Older releases remain
available. The same files and checksums are uploaded to a GitHub prerelease.
The GitHub repository is private, so those downloads require repository access.

The desktop companion provides enrollment, status/progress and service control;
configure desktop jobs in the PBS-X server UI. Linux retains its static
`pbsx-agentctl` and background agent for systems without a desktop. Android
manages jobs in its app. These are preview distributions: Windows executables
are unsigned, and Android retains its established development signing
certificate, with debugging disabled in the downloadable release-mode APK.
This does not enable automatic client updates.

## Install

- Windows amd64: extract the ZIP, open a 64-bit Administrator PowerShell in the
  extracted folder, then run
  `powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1`.
- Linux amd64 with systemd: extract the archive and run
  `python3 install.py --check`, then `sudo python3 install.py`. Add `--headless`
  when installing only the agent and CLI. The desktop needs a graphical session
  and its system graphics libraries. Its manifest records the minimum glibc
  version and required shared libraries; both check mode and installation verify
  these before changing the service. Use `--check --headless` to check only the
  static service/CLI on a system without those desktop dependencies.
- Android 8+ on ARM64 or x86-64: open the APK and permit installation from that
  browser/file manager. Updates retain the existing signing certificate; do not
  uninstall an existing app just to update it.

Desktop installers retain credentials when updating their managed installation
path. Older lab services installed at another path need an explicit transition;
the preview installer refuses to replace an unrelated service. See
[desktop setup](host-desktop-preview.md) and [Android setup](../pbsx-android/README.md).

## Repeatable release command

Run publication from this checkout on the trusted Linux development/build
machine. GitHub CLI needs release write access; SSH needs the existing
repository-container access. Neither credential is embedded into client files,
committed, copied into CI, or made available through the public server.

1. Commit and push the intended source. Native Windows/Linux checks run through
   `.github/workflows/host-app.yml`. For a new desktop version, dispatch it with
   `gh workflow run host-app.yml --ref YOUR_BRANCH -f version=0.7.1-preview`.
   Wait for a successful run and obtain its run ID.
2. From the repository root:

   ```sh
   python3 tools/publish-pbsx-clients.py release \
     --run-id SUCCESSFUL_HOST_APP_RUN_ID \
     --tag UNIQUE_CLIENT_RELEASE_TAG \
     --build-android \
     --output artifacts/client-releases/UNIQUE_CLIENT_RELEASE_TAG \
     --host root@10.0.1.37 \
     --identity /home/rikka/.ssh/id_ed25519 \
     --known-hosts PATH_TO_VERIFIED_KNOWN_HOSTS
   ```

   Use your normal SSH configuration instead by omitting `--identity` and
   `--known-hosts` if the server is already trusted there. The destination host
   key must be verified; the publisher does not disable SSH host-key checking.
   Android build dependencies and its existing preview keystore must be present.
   The command selects the installed Java 17 when `JAVA_HOME` is unset.

The command downloads the two successful native CI artifacts, verifies their
manifests, executable architectures, shared version and source commit, and
refuses stale application source. It builds Android using the normal Go/Gradle
checks, then verifies its signature, pinned certificate, universal ABIs and
16 KB alignment. An already verified current Android build can be reused by
omitting `--build-android`. Reuse verifies the Android tree and every local Go
replacement module, including the shared PXAR implementation. Older build-info
records without these input hashes require one rebuild. The builder captures
source inputs before compilation and rejects input changes detected afterward;
workspace overrides are disabled so resolution follows the core module's
committed `go.mod`.

Only allowlisted binaries, installers, manifests and generated instructions enter
the desktop archives. Android settings, credentials, local keystores and device
data are excluded. Linux execute bits lost during CI artifact downloads are
restored in the archive. `release.json` records the exact source commits, CI run,
versions, Android certificate fingerprint, asset sizes and hashes.

GitHub publication starts as a draft. Each completed upload's GitHub digest is
checked. Files then go to private staging on the repository container, where
checksums are rechecked before a versioned directory is activated. Finally the
GitHub prerelease is made visible. Publication uses the existing SSH connection;
it requires no public SSH port or new CI deployment/signing secrets.
Activation flushes the copied files and directory entries before switching
`latest`, then flushes that pointer change too. File or directory flush failures
before the switch preserve the previous latest release and can be retried.
SSH and SCP explicitly require strict host-key checking even if local SSH
defaults permit automatically accepting new host keys.

For separate review and publication, use `prepare` with the run/tag/output
arguments first, then `publish --output DIRECTORY --host HOST` with the SSH
options. Retry a failed publication using `publish` against the same directory.
Matching uploaded assets can be reused; conflicting assets or a changed existing
release tag are rejected. A failed zero-byte `starter` upload in a draft is
rechecked and removed before retrying; completed assets and non-draft releases
are preserved.

Activation compares timezone-aware release creation times while holding the
publication lock. Retrying an older release keeps a newer `latest` link in
place, while retaining the older immutable files and finishing its GitHub
publication. For a deliberate rollback, pass `--allow-rollback` to `publish`
with the existing release directory. To rebuild different bytes, choose a new
tag; published release directories are never overwritten.

`.github/workflows/client-publication.yml` checks corruption rejection, safe
activation, immutable releases, retry behavior, private-file exclusion, traversal
and symlink rejection, Linux archive permissions, and Android source freshness.
It has read-only repository
permissions; publication itself remains an explicit maintainer command.

See [the first client release validation](client-release-validation.md) for the
published versions, source provenance, CI checks and public download verification.
The [2026-11-18 publication and audit](pbsx-publication-and-audit-2026-11-18.md)
records desktop `0.7.3-preview`, Android `0.5.6-preview` and their verification.
The [subsequent preview.2 publication](client-release-2026-11-18-preview-2.md)
records desktop `0.7.4-preview` and Android `0.5.7-preview` with the directory-flush fix.
The [preview.3 publication](client-release-2026-11-18-preview-3.md) adds Android
`0.5.8-preview` with the Shizuku shared-folder picker, including Download.
The [preview.4 publication](client-release-2026-11-18-preview-4.md) updates Android
to `0.5.9-preview`, defaulting normal folder labels to the selected folder name.
