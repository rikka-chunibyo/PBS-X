# Public PBS-X installation and APT updates

The public repository is `https://pbsx.rikka.ca/apt`, suite `trixie`, component
`main`, for Debian 13 amd64. It currently serves the tested
`4.2.9-1+pbsx1` server/client/file-restore build and `pbsx-repository` 1.0.3.
The Ubuntu repository container is an HTTP backend at `10.0.1.37:80`;
Nginx Proxy Manager terminates public HTTPS. There is no container TLS listener.

## Install

The [offline PBS-X ISO](pbsx-offline-iso.md) already includes PBS-X and this
repository configuration. The following bootstrap is for an upstream PBS base.

1. Install the official PBS 4 ISO onto the chosen OS disks. Configure its network
   and appropriate official repositories (enterprise with a subscription, or
   `pbs-no-subscription` without one). Keep Debian security repositories enabled.
2. Run `install-pbs-x.sh` from the TFM runner's
   `sys/linux/proxmox/host` directory, **inside the dedicated PBS system**.
   The script rejects PVE hypervisors and unsupported OS/architecture/base versions.

Direct download:

```sh
curl -fsSL https://tfm.thealtair.net/scripts/sys/linux/proxmox/host/install-pbs-x.sh -o install-pbs-x.sh
sudo bash install-pbs-x.sh
```

Use `--check` for preflight without changing package/repository state, or
`--reinstall` to reinstall the PBS executables. Check mode reports missing
dependencies without installing them. Normal mode installs required download/key
tools if needed. There are no disk formatting, datastore creation or reboot steps
in this script. Existing PBS logins and datastore configuration are retained.

The installer authenticates the repository key against the pinned primary
fingerprint `85177E09BEAB50C669C0D68125C3A26BDAB0FCB4`, then lets APT validate
signed metadata and checksums. It uses a temporary source to install the support
package first. That package installs the permanent source and safety hooks
together, before the separate PBS executable transaction.

## Updates and failure handling

```sh
sudo apt update
sudo apt upgrade
```

New PBS-X packages become available when a release is built, tested and published.
Debian and unrelated Proxmox packages retain their normal sources. Fork package
rules reject upstream executable replacements. The feed is restricted by APT
policy to the known PBS-X executables and `pbsx-repository`.

Keep `pbsx-repository` installed. Before APT changes PBS executable packages,
its hooks check for running PBS tasks and native host backup/restore records.
Detected active work aborts the package transaction for retry when idle. The
checks include other PBS tasks, so run updates from SSH or a local console with
other PBS activity idle. The
hooks stop the server services, save a coherent configuration/credential/run-state
archive under root-only `/var/backups/pbsx-apt-*`, and restore previously active
services after dpkg returns. Pending offline-device commands are retained.

An interrupted transaction leaves a restart marker in `/run`. A recovery timer
checks each minute and resumes services once actual APT/dpkg locks are free.
It does not roll packages or configuration back. A failed package operation can
still require normal APT repair:

```sh
sudo apt --fix-broken install
```

Direct `dpkg -i` does not run these APT hooks. For a planned package rollback,
keep the previous [release bundle and protected configuration](pbsx-installation.md).
State archives are not datastore copies or full OS backups. The checks are not
an atomic maintenance gate against every possible concurrently starting job;
schedule updates while clients and scheduled tasks are idle.

## Maintainer operations

On LXC116, `/usr/local/sbin/pbsx-apt-repository` signs/publishes packages using
private state under `/var/lib/pbs-x-apt/release` (mode `0700`), outside the web root.
It permits only the expected package names. Publishing any core executable
requires exactly one each of server, client and file-restore at the same PBS-X
version. Repository-support-only updates remain allowed. Run
`bash tools/pbsx-apt-repository.sh --check-release BUNDLE_DIRECTORY` on the build
machine for the same read-only checks before publication; publication repeats
them before changing repository state.
The `pbsx-apt-refresh.timer` runs daily
to refresh the signed metadata's 14-day validity. Monitor that service and disk
space. Unreferenced package payloads are retained for clients with cached indexes;
retention cleanup must preserve supported rollback artifacts.

The publisher explicitly makes archive files web-readable, including after a
refresh invoked under `umask 077`. Private administration/signing directories
remain mode `0700`, outside the public tree. Support package 1.0.3 enables its
recovery timer during ISO/chroot installation without trying to contact a
running systemd; normal installations start it immediately.

The current primary key expires after two years, with a signing subkey expiring
after one year. Rotate before expiry; distribute overlapping trusted public keys
through a newer repository package before switching the signer. Update the
bootstrap script's pinned identity during a primary-key rollover. Keep the
signing primary offline when a separate signing workflow is introduced.

An encrypted secret-key recovery export and its separate passphrase file are
mode `0600` on the container. Restore into a private temporary keyring was tested.
A second copy, explicitly approved by the user, is in ignored mode-`0700`
`artifacts/apt-repository-private/` on the development server. Neither recovery
material nor passphrases belong in Git, public artifacts or chat.

## Validation

VM115 downloaded real packages through public HTTPS and completed a fresh
repository-support bootstrap plus protected PBS reinstall. The published TFM
script matched the tested source byte-for-byte and appeared in the runner index.
The installer rejected the old lab signing identity without package changes.

Eight protection unit tests passed. Live APT tests refused a running-host-state
fixture before any service stop, recovered services after a deliberately failing
package, completed normal APT repair, and respected real POSIX dpkg locks during
interrupted-update recovery. Native ZFS snapshot backup/restore passed afterward.
Earlier [signed repository tests](apt-repository-validation.md) covered package
upgrade/downgrade selection, TLS/signature failures, expiration and corruption.

The earlier report's missing APT-protection hooks are implemented by this support
package. Physical R710/controller validation and future schema migration testing
remain separate. The cloud test VM lacks the optional Proxmox guest restore image;
these native-agent restore tests do not validate PVE guest-image file restoration.
