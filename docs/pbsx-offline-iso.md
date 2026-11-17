# PBS-X offline installation media

The PBS-X amd64 ISO installs the OS, kernel, ZFS, PBS-X server/client/file-restore
and repository configuration from the media. Installation requires no Internet
connection. The installer still asks for the target disks, hostname, network
settings and administrator password. Selected disks are erased.

The initial preview uses PBS-X `4.2.9-1+pbsx1` and `pbsx-repository` 1.0.3:

- [Download ISO](https://pbsx.rikka.ca/iso/pbsx_4.2.9-1+pbsx1-offline-amd64.iso)
- [SHA256 checksum](https://pbsx.rikka.ca/iso/pbsx_4.2.9-1+pbsx1-offline-amd64.iso.sha256)
- [GitHub preview release and alternate download](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-4.2.9-pbsx1-iso-preview)

The GitHub repository is private, so its download requires repository access.

Keep the ISO's original filename when checking its checksum. Remove installation
media after installation completes so the machine boots from its installed disk.
The public ISO location supports resumable downloads. If a download is
interrupted, keep its partial file and retry with a client that supports resume.

After installation, open `https://SERVER-IP:8007` and sign in as `root@pam` with
the password you chose. Normal local backups do not require Internet access.
When Internet access becomes available, run:

```sh
apt update
apt upgrade
```

`pbsx-repository` installs the public signing key and the signed
`https://pbsx.rikka.ca/apt` feed, with package pins that preserve PBS-X and
protection hooks around APT upgrades. Debian repositories supply system updates;
the Proxmox `pbs-no-subscription` repository supplies kernel and other PBS
system dependencies. The ISO disables the default subscription-only PBS source.
These defaults are applied once during installation; package reinstalls do not
reset subsequent administrator changes. An entirely offline server can instead
be updated from a new, complete release bundle copied to it.

## Build

On an amd64 Debian/Ubuntu build machine:

```sh
sudo apt-get install xorriso squashfs-tools debian-archive-keyring gnupg curl python3
python3 tests/pbsx-iso.test.py
python3 tools/build-pbsx-iso.py --output artifacts/iso/pbsx-offline-amd64.iso
```

Building normally needs Internet access. The builder checks the pinned SHA256
of the official PBS 4.2-1 base ISO, authenticates the PBS-X feed with the tracked
public key and its pinned fingerprint, and verifies each downloaded package
against the signed index. An isolated APT resolver downloads missing dependencies
from signed Debian and Proxmox repositories. It does not change the build host's
APT sources, installed packages or services. No signing private keys are needed.

Use `--version 4.2.9-1+pbsx1` to select a published release. Without it, the newest
published server version with a matching client/file-restore set is selected.
To build from already trusted local inputs, use:

```sh
python3 tools/build-pbsx-iso.py \
  --base-iso /path/to/proxmox-backup-server_4.2-1.iso \
  --packages-dir /path/to/pbsx-packages \
  --dependencies-dir /path/to/extra-dependencies \
  --output artifacts/iso/pbsx-offline-amd64.iso
```

The local PBS-X package directory must contain the matching three executable
packages and `pbsx-repository`. Local input authenticity is the operator's
responsibility. The official base checksum is checked in both modes.

Every build checks offline dependency closure for BIOS, UEFI and UEFI Secure Boot
package selections, retains the upstream BIOS/UEFI boot equipment, then extracts
and hashes every package from the final ISO. It emits an ISO checksum, boot
report and manifest containing package hashes, versions and source provenance.
The image retains upstream licensing notices and the upstream installer/kernel;
its menu identifies the fork as PBS-X. The media identifier remains compatible
with the upstream installer.

The GitHub **PBS-X offline ISO** workflow runs regression checks on relevant
pushes/PRs. Dispatch it manually to build and retain an ISO artifact for 14 days.
It uses a Debian 13 container with current Debian archive trust roots. CI fetches
the four mirrored release inputs using its read-only GitHub token and verifies
their sizes, SHA256 hashes and package identities against the committed
`packaging/iso/release-inputs.json`. These hashes were verified against the signed
APT feed before mirroring. This avoids Cloudflare's browser challenge for GitHub
runners without weakening package verification or changing the installed APT
source. New CI input versions require updating the pinned manifest after release
verification; arbitrary `--version` values are rejected.

It uses published packages and does not build or publish a new server release.
It does not publish to the APT server or include unattended answers, passwords,
enrollment tokens, personal SSH keys or repository signing secrets.

## Verification and installation

Obtain the image and checksum from a trusted release location. In their directory:

```sh
sha256sum --check pbsx-offline-amd64.iso.sha256
```

Attach the ISO to a VM, or write it as a raw image to USB using your normal image
writer. Boot it and choose the graphical or terminal installer. Keep backup-data
disks out of the installation disk selection. This image installs a dedicated
PBS-X machine; it is not an installer to run on an existing PVE hypervisor.

A checksum detects corruption; it does not authenticate an untrusted download.
The base ISO checksum and repository signatures authenticate build inputs, not
the final image itself. Hardware USB boot, storage-controller compatibility and
recovery on your actual server still require validation.

See the [offline install validation report](pbsx-offline-iso-validation.md) for
the BIOS/Secure Boot and later APT reconnection tests and their limits.
