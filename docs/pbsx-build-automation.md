# Build and release automation

| Component | Automated today | Remaining manual step |
| --- | --- | --- |
| Linux/Windows host apps | GitHub CI tests, native builds, bundle validation, Linux window smoke test and preview artifacts; client publisher packages verified builds for GitHub and HTTPS | Invoke publication of a successful CI run; production Windows signing |
| Server Debian packages | `tools/build-pbsx-release.sh`: clean source export, Debian package build/tests, matching bundle, hashes and installer | Invoke on the Debian 13 builder and review the result |
| Android | Hosted universal Go bindings, debug/release compilation, Java tests/lint and Android 13 SAF instrumentation; publication CI also runs Go race tests. `pbsx-android/build.sh` performs the maintainer build, certificate/alignment checks and provenance | Invoke publication with the SDK and existing preview keystore; production signing remains separate |
| APT feed | Combined server publisher builds/verifies a matching ISO before stable activation; repository tooling verifies package sets and signs metadata | Invoke the combined publisher with a tested bundle |
| Offline ISO | Combined publisher dispatches hosted builds from exact mirrored package hashes, verifies artifacts, publishes GitHub/HTTPS and switches the latest-image link with stable APT | Hardware-specific installation checks and production signing remain separate |

Client and server builds remain separate. A green host-app run does not rebuild
the server or Android app. Stable server publication now couples APT and ISO:
the matching image is completely built and verified before the stable package
index can change. Testing-channel publication and daily signature refresh do
not trigger image builds. Repository signing keys stay on the repository
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

Android compilation and emulator tests use separate hosted runners to keep
system images and native build caches from exhausting one disk. The emulator
receives short-lived debug-signed test APKs from its build job. These fixtures
are not release downloads and cannot replace previews signed with the pinned
maintainer certificate. Public client publication remains an explicit command.

See [offline installation media](pbsx-offline-iso.md),
[combined server/ISO publishing](pbsx-server-publishing.md),
[server installation](pbsx-installation.md) and
[APT repository administration](apt-repository-host.md).
