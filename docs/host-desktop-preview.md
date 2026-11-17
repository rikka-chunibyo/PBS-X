# Windows and Linux desktop preview

The native desktop companion runs as an ordinary user. The background agent owns
credentials and performs server-configured backup/restore jobs independently of
any open windows. Administrative actions use Windows UAC or Linux PolicyKit
(`pkexec`); normal status viewing needs neither elevation nor access to the vault.

The first window provides enrollment, connection/service state, live backup and
restore counters, and service start/stop/restart. Job editing, scheduling,
retention and restore requests remain in the PBS-X server UI. This is a companion
preview, not yet a complete local job-management application.

The desktop and CLI read a limited public snapshot written every two seconds.
It includes phase, connection status, byte/file counts and last-operation outcome;
it excludes filenames, URLs, host IDs, enrollment codes, storage API keys and raw
errors. A snapshot older than 15 seconds is treated as stopped/unavailable.
Linux uses `/var/lib/pbsx-agent-status/status.json`; Windows uses the separate
`HKLM\SOFTWARE\PBS-X\PublicStatus` key. Windows credential keys (including retained legacy keys) are explicitly restricted
to SYSTEM and Administrators; their DPAPI-encrypted values are retained. A
[Microsoft DPAPI note](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata) explains why machine-local encryption also requires
restricting access to the encrypted blob.
The activity bar is indeterminate: a full-tree pre-scan is still deferred.

## Build

Use the Go version in `pbsx-host-agent/go.mod`. From the repository root:

```sh
python3 tools/build-host-app.py --os linux --version 0.7.0-preview --output artifacts/host-app-linux
python3 tools/build-host-app.py --os windows --version 0.7.0-preview --output artifacts/host-app-windows
```

The full PBS repository contains Linux-only filenames (including a systemd
unit name with a literal backslash). Windows Git rejects those paths even with
sparse checkout. The workflow exports only portable host-app sources on Linux,
then builds and tests that archive natively on Windows. Download its
`host-app-sources` artifact and extract the inner ZIP to build on Windows; it
includes a `.repoid` source-commit marker. Archive builds record `dirty: null`,
since there is no Git index against which to detect local edits.

The Windows build command runs natively with a C compiler on PATH. To build
it on Linux, add `--cc x86_64-w64-mingw32-gcc`. Native desktop builds need the
[Fyne platform dependencies](https://docs.fyne.io/started/). For the current
Fyne version, Linux needs OpenGL, EGL, X11, Wayland and xkbcommon development
headers. The workflow lists the Debian/Ubuntu package names.

Add `--headless` to omit the GUI and all C/graphics build dependencies. The agent
and `pbsx-agentctl` are always built with CGO disabled. All executables in a bundle
receive the same version; its manifest records file hashes, lengths, source
commit, dirty-tree state and Go version. Build on the oldest supported Linux
userspace for desktop distribution; the service and CLI are static executables.
The builder reads the Linux desktop's actual versioned glibc symbols and shared
library dependencies with `readelf`, recording them as `desktop_runtime` in its
manifest. Both installer check mode and installation verify this requirement
against the current system before touching services or installed files. The
current preview requires glibc 2.38 or newer and its graphics libraries; future
builds report their own minimum. Legacy desktop bundles without runtime metadata
need rebuilding. `--headless` bypasses desktop requirements for the static CLI
and service. The Windows desktop imports only Windows system DLLs.

## Install a trusted local bundle

These preview manifests provide corruption checks, **not publisher authentication**.
Manual preview downloads are available at [pbsx.rikka.ca/downloads](https://pbsx.rikka.ca/downloads/)
and in private GitHub releases. These include the checked native binaries and
installers. Windows Authenticode and a production signed automatic-update
channel remain release work. Publication does not enable automatic updates.
See [client publishing](pbsx-client-downloads.md) for the repeatable release command.

On an amd64 systemd Linux system, run inside the Linux bundle:

```sh
python3 install.py --check
sudo python3 install.py
```

For a server without a desktop, pass `--headless`. Application files go under
`/opt/pbsx-agent`, commands are linked in `/usr/local/bin`, and the desktop entry
is installed under `/usr/local/share/applications`. The first installation creates
the service; use the desktop Connect page or CLI enrollment before starting work.
The desktop needs a working PolicyKit authentication agent for its admin prompt.

On Windows amd64, open a 64-bit Administrator PowerShell window in the bundle:

```powershell
.\install.ps1 -Check
.\install.ps1
```

Use `-Headless` to omit the desktop. Files go under `Program Files\PBS-X\Agent`,
and the Start Menu receives a PBS-X Agent shortcut. Close the desktop window
before replacing its executable. The installer checks that an existing service
uses this installation path; earlier lab installations from source directories
must use their existing installer or be transitioned explicitly.

Both installers retain enrollment/storage credentials and the existing service
configuration when updating an installation at the expected path. An existing
running service is stopped during replacement and restarted afterward. Fresh
operation status blocks installation while a backup/restore is reported active;
install during an idle maintenance window, since this snapshot check is not an
atomic cross-process job drain. Executable replacement and service-command failures restore
the previous executable set. Linux recovery copies are root-only under
`/var/backups/pbsx-agent-install-*`; Windows copies are admin-only under
`Program Files\PBS-X\install-backups`. Keep them until the update is accepted.
An abrupt power loss may require restoring that copy manually.

## Linux CLI

```sh
pbsx-agentctl status
pbsx-agentctl status --json
pbsx-agentctl watch
sudo pbsx-agentctl enroll --stdin
sudo pbsx-agentctl start
sudo pbsx-agentctl stop
sudo pbsx-agentctl restart
```

For enrollment, paste the code into stdin and signal EOF, or redirect a protected
code file. Avoid putting the code in shell history or process arguments. The
shared controller uses a private temporary file and clears it afterward; the
privileged `setup` helper validates input before changing enrollment, stops the
old service, bootstraps, installs its definition and starts it. A failed bootstrap
leaves the service stopped with the enrollment pending. Generate a fresh code if
the original one was consumed or expired. Neither GUI nor CLI copies credentials
into user preferences. Closing the window does not stop scheduled backups.

## Automated checks

`.github/workflows/host-app.yml` runs Linux race tests, native Windows unit tests,
GUI builds, bundle checks, Host UI tests and a Linux virtual-display smoke test.
It uploads preview artifacts only; it has read-only repository permissions and
no signing/publishing credentials. The maintainer publishing script collects a
successful run, verifies it and publishes matching GitHub and HTTPS downloads.
Windows uses a portable source archive, and
the Linux smoke test isolates its X11 display from any inherited Wayland session.
The local smoke command is:

```sh
xvfb-run -a -s '-screen 0 1200x800x24' env LIBGL_ALWAYS_SOFTWARE=1 python3 tools/smoke-host-desktop.py artifacts/host-app-linux/pbsx-desktop artifacts/desktop-linux.png
```

This runs offline with administrator controls disabled and temporary GUI settings.
It captures the actual X display; the toolkit's framebuffer capture produced a
black image under the lab's software renderer and is not used as evidence.
