# Build and release automation

| Component | Automated today | Remaining manual step |
| --- | --- | --- |
| Linux/Windows host apps | GitHub CI tests, native builds, bundle validation, Linux window smoke test and preview artifacts; client publisher packages verified builds for GitHub and HTTPS | Invoke publication of a successful CI run; production Windows signing |
| Server Debian packages | `tools/build-pbsx-release.sh`: clean source export, Debian package build/tests, matching bundle, hashes and installer | Invoke on the Debian 13 builder and review the result |
| Android | `pbsx-android/build.sh`: Go tests, gomobile library, Gradle debug APK, unit tests, lint, signature/certificate/alignment checks and provenance; same client publisher | Invoke with Android SDK and existing preview keystore; production signing remains separate |
| APT feed | Repository package tooling, signed metadata/package checks and publication scripts; deployed metadata refresh timer | Approve and invoke publication of a tested release |
| Offline ISO | Relevant-change regression CI; manually dispatched full ISO build from pinned release inputs, offline dependency checks, final package hashes and artifact upload | VM/hardware installation checks and release publication |

These are separate stages. A green host-app run does not rebuild the server or
Android app. An ISO build consumes already published PBS-X packages; it does not
publish new Debian packages. Repository signing keys stay on the repository
administration system, outside source control and GitHub CI. ISO builds record
the package versions and hashes actually used rather than assuming that the
repository's newest version will stay fixed.

The complete ISO CI build, checksum and artifact upload passed in
[run 44003946462](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44003946462).
CI uses the four hash-pinned packages mirrored into the private GitHub preview
release, so Cloudflare browser challenges on the public feed do not block it.
The installed server still uses `https://pbsx.rikka.ca/apt` for normal updates.

Client downloads are published at `https://pbsx.rikka.ca/downloads/`. One
maintainer command can rebuild Android, collect a successful native Windows/Linux
CI run, package the apps, verify uploaded GitHub digests, and activate the public
downloads atomically. See [client publishing](pbsx-client-downloads.md). The
existing local GitHub and SSH access are used; no deployment key or Android
signing keystore is added to CI.

See [offline installation media](pbsx-offline-iso.md),
[server installation](pbsx-installation.md) and
[APT repository administration](apt-repository-host.md).
