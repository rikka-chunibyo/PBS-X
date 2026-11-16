# Build and release automation

| Component | Automated today | Remaining manual step |
| --- | --- | --- |
| Linux/Windows host apps | GitHub CI tests, native builds, bundle validation, Linux window smoke test and preview artifacts | Release approval, packaging/signing and publication |
| Server Debian packages | `tools/build-pbsx-release.sh`: clean source export, Debian package build/tests, matching bundle, hashes and installer | Invoke on the Debian 13 builder and review the result |
| Android | `pbsx-android/build.sh`: Go tests, gomobile library, Gradle debug APK, unit tests, lint and checksum | Invoke with Android SDK/toolchain; production APK signing and publication |
| APT feed | Repository package tooling, signed metadata/package checks and publication scripts; deployed metadata refresh timer | Approve and invoke publication of a tested release |
| Offline ISO | Relevant-change regression CI; manually dispatched full ISO build with authenticated inputs, offline dependency checks, final package hashes and artifact upload | VM/hardware installation checks and release publication |

These are separate stages. A green host-app run does not rebuild the server or
Android app. An ISO build consumes already published PBS-X packages; it does not
publish new Debian packages. Repository signing keys stay on the repository
administration system, outside source control and GitHub CI. ISO builds record
the package versions and hashes actually used rather than assuming that the
repository's newest version will stay fixed.

See [offline installation media](pbsx-offline-iso.md),
[server installation](pbsx-installation.md) and
[APT repository administration](apt-repository-host.md).
