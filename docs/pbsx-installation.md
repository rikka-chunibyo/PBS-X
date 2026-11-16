# Installing and updating PBS-X

PBS-X can be installed directly using the [offline PBS-X ISO](pbsx-offline-iso.md),
which includes the operating system, kernel, ZFS, PBS-X packages and signed APT
repository configuration. Installation needs no Internet connection. Once
connected, use `apt update` and `apt upgrade` for updates.

The alternative procedure below installs Debian packages over a PBS 4.x /
Debian 13 amd64 base. The official upstream PBS ISO supplies the base but does
not contain this fork.
This branch uses upstream PBS 4.2.9 plus the PBS-X changes, packaged as
`4.2.9-1+pbsx1`.
See the [fresh-install validation](production-install-validation.md) for tested
behavior and remaining hardware/recovery limits.

The public [APT installer](pbsx-apt-installation.md) configures the signed feed
at `https://pbsx.rikka.ca/apt` and protected APT updates. The bundle procedure
below remains available for manual installation and rollback.

## Install on a dedicated server or VM

1. Install the official PBS ISO on the intended OS disk(s). Selected disks are
   overwritten. Keep the backup-data disks separate from that selection.
   Configure the hostname, network and administrator password in the installer.
2. Configure the appropriate official PBS package repository and signing key.
   A subscription uses the enterprise repository; without one, disable that
   repository and configure `pbs-no-subscription`. Keep Debian repositories
   enabled for system security updates.
3. Copy the entire PBS-X release bundle to the machine. From that directory:

   ```sh
   sha256sum --check SHA256SUMS
   sudo ./install-pbsx.sh .
   ```

4. Open `https://SERVER-IP:8007` and sign in as `root@pam` with the password
   chosen during the base installation. Configure the datastore, verification,
   retention and a recoverable second copy as appropriate for the chosen layout.
5. Enroll a test client, run a backup and an incremental backup, then restore
   to a separate destination and inspect the restored contents and metadata.

The installer validates the checksums, package identities and matching PBS-X
versions before using APT to install the server, client and file-restore
packages. Dependencies come from the configured signed repositories. It refuses
to run on a Proxmox VE hypervisor. It checks for active PBS tasks, downloads
dependencies before stopping services, and saves existing server state in a
root-only `/var/backups/pbsx-install-*` directory. The release checksum file
detects transfer corruption; obtain the bundle from a trusted source.

On a plain Debian 13 base, prepare the Proxmox repositories and install a
compatible Proxmox kernel and ZFS tools first if ZFS will be used. Boot that
kernel before creating a pool. Existing bootloader configuration needs checking;
the Debian cloud-image test needed its GRUB OS-disk target corrected when the
Proxmox kernel was installed. The official ISO avoids that cloud-image setup.

The fork's RAID0 option creates a striped **datastore pool** through the web UI.
It does not change how the OS installer works. ZFS needs directly exposed disks
through a suitable HBA/pass-through configuration. The R710's actual controller
and disk layout still need checking before choosing installation targets.
A stripe has no disk redundancy: losing one member can lose the datastore.

The server package uses upstream documentation as a dependency. The small
release bundle omits the separately built documentation and debug-symbol
packages. Project-specific guides remain in this repository.

## Install the updated Linux host agent

The server package does not install an agent on every client. On a Linux host,
copy the bundle's tested `pbsx-host-agent-linux-amd64` executable, then:

```sh
sudo install -d /opt/pbsx-agent
sudo install -m 0755 pbsx-host-agent-linux-amd64 /opt/pbsx-agent/pbsx-agent
sudo /opt/pbsx-agent/pbsx-agent enroll ENROLLMENT_CODE
```

Generate the enrollment code in that host's Agent page. Enrollment starts the
agent in the foreground; after enrollment succeeds, stop that foreground run
and install/start the service:

```sh
sudo /opt/pbsx-agent/pbsx-agent install
sudo /opt/pbsx-agent/pbsx-agent start
```

The service creates its state/configuration directories. It is a privileged
backup/restore agent: its filesystem view includes host `/tmp`, `/etc` and home
directories, and its bounded capabilities permit ownership, ACL, file-capability
and special-permission restoration plus snapshot operations. It retains
`NoNewPrivileges=yes`. Install and test the required ZFS/Btrfs/LVM tools for the
source's snapshot provider; selecting snapshot-required mode prevents live-read
fallback when a provider is unavailable.

For an existing service, stop it, replace its executable, uninstall and reinstall
the service definition, then start it. Replacing the executable alone does not
update an already installed systemd unit. Preserve `/etc/pbsx-agent` and
`/var/lib/pbsx-agent`, including the local credential-sealing keys.

## Updates and rollback

When installed through the public bootstrap, use `apt update` followed by
`apt upgrade`. The `pbsx-repository` package supplies repository trust, policy
and the APT protection hooks described in the [APT guide](pbsx-apt-installation.md).
Keep this package installed while using the feed. Direct `dpkg -i` bypasses the
APT hooks; the bundle installer supplies its own checks and backup instead.

The server package installs `/etc/apt/preferences.d/pbsx.pref`. It permits PBS-X
versions of the fork's executable packages and rejects upstream replacements of
those packages. Other Debian/PBS dependencies, kernels and documentation remain
updatable. New PBS-X releases must review/integrate applicable upstream changes;
this policy does not automatically turn upstream packages into fork updates.

Install each new complete bundle with the same installer. Preserve the previous
bundle and the installer-generated state backup. A planned package rollback is:

```sh
sudo ./install-pbsx.sh --allow-downgrade /path/to/previous-bundle
```

Use the installer from a trusted bundle. Restoring an older release may also
require restoring its pre-upgrade configuration while PBS is stopped. The
installer does not guess at backward-compatible configuration changes or
automatically restore an older database after a failed install.

## Server and encryption-key recovery

Back up `/etc/proxmox-backup` and
`/var/lib/proxmox-backup/pbsx-agent-credentials` coherently with a recoverable
datastore. The latter contains the generated credential identities and retained/
retired owner history used after re-enrollment. `/etc` alone is insufficient.
The installer also captures host status, run and enrollment state for recovery
inspection; do not replay old pending commands blindly after rebuilding.

After a rebuild, restore server configuration/authentication material and the
credential registry with original ownership and permissions while PBS is
stopped, register/import the existing datastore as appropriate, then test
historical reads, re-enrollment and append access before enabling schedules.
Keep Android's exported settings and encryption keys independently recoverable.
The server cannot reconstruct a missing client encryption key.

## Build a bundle

Check out `pbsx-production-install` for this validation build. Use a Debian 13
amd64 builder with the official Proxmox development repository and its signing
key configured, then install the source's build dependencies:

```sh
sudo apt-get install git git-lfs devscripts debcargo clang fakeroot
git lfs install
git lfs pull
sudo apt-get build-dep -Pnodoc .
tools/build-pbsx-release.sh
```

Commit source changes first. The script copies tracked, materialized files to
an isolated temporary directory, preserving this checkout's `build/reports`.
It writes packages and `SHA256SUMS` under `artifacts/releases/`. Default build
parallelism is two; set `PBSX_BUILD_JOBS` for a larger builder. Existing Debian
`DEB_BUILD_OPTIONS` are honored. Normal test execution remains enabled unless
the caller explicitly supplies `nocheck` and validates tests separately.
Set `PBSX_AGENT_BINARY` to include an independently built/tested Linux agent.
The bundle retains Debian build metadata recording the exact dependency versions.

References: [official PBS installation](https://pbs.proxmox.com/docs/installation.html),
[APT version selection](https://manpages.debian.org/trixie/apt/apt_preferences.5.en.html).
