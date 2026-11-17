# Offline ISO validation, 2026-11-16

Image: `pbsx_4.2.9-1+pbsx1-offline-amd64.iso`.
SHA256: `9d83ac4a9e708de05715c5be1fd1975d1a9d85145c98a4b2ad42a28228e0f470`.
Builder commit: `87708c0b5b4ae930bd9baa7bacb73e321307fcda`, clean tree.
PBS-X executable packages: `4.2.9-1+pbsx1`; repository package: `1.0.3`.

## Installation tests

Dedicated disposable VMs 117 (BIOS) and 118 (UEFI, pre-enrolled Secure Boot keys)
used new 32 GiB disks, 4 GiB RAM and disconnected virtual network links.
The hypervisor and development VM100 were not restarted. Existing guests were
not changed. Both installations completed from the media, then booted from their
installed disks after the test media was removed.

A private derivative added an unattended test answer, random administrator
password hash, guest agent and a first-boot probe. None is in the distributed
interactive ISO. The guest agent allowed checks with the NIC still disconnected.

Both guests passed:

- Correct PBS-X server/client/file-restore and repository package versions.
- Empty `dpkg --audit` output.
- Running `proxmox-backup` and `proxmox-backup-proxy` services.
- Successful local HTTPS login-realm API response.
- Signed PBS-X source at `https://pbsx.rikka.ca/apt` and expected package pins.
- Community system repository, with the subscription-only PBS source disabled.
- Enabled and active APT recovery timer.
- Network interface `NO-CARRIER`, with no Internet access during installation.

The installed UEFI guest's SecureBoot EFI variable was 1. The initial probe
incorrectly expected the protected version API to allow anonymous access; it
correctly returned 401. The probe was changed to the public login-realm API and
passed both directly and through its first-boot service.

## Reconnection and updates

VM117 was then temporarily connected and obtained DHCP. `apt-get update` with
`APT::Update::Error-Mode=any` succeeded against Debian, Proxmox and PBS-X sources
with normal TLS trust and signature checking. `apt-get --simulate upgrade`
succeeded. Candidates for all three executable packages remained
`4.2.9-1+pbsx1`, while upstream 4.2.10/4.2.11 replacements had priority -1;
`pbsx-repository` selected 1.0.3. No upgrade was installed during this check.
The NIC was disconnected again and both test VMs were shut down afterward.

## Packaging and hosting fixes

The install test exposed `pbsx-repository` 1.0.2 trying to contact systemd inside
the installation chroot. Version 1.0.3 enables the recovery timer with Debian's
helper and starts it only on a booted system. The builder now rejects older
support packages. The corrected support package was published to the signed feed.

A publication under `umask 077` exposed unreadable archive indexes. The publisher
now sets public archive permissions explicitly. A real refresh under that umask
was checked: public metadata 0644, signer/admin directories 0700, and successful
HTTPS feed signature/package validation. The repository database, published
indexes and previous publisher were backed up before deployment.

Cloudflare separately returns a browser challenge to GitHub-hosted runners
(`HTTP 403`, `cf-mitigated: challenge`). CI uses authenticated GitHub release
mirrors of the same four verified packages, with committed size/hash/identity
pins. The installed system's APT source is unchanged. A large local HTTP/2 ISO
download through the proxy was interrupted. The static ISO location now sends
`Cache-Control: no-store, no-transform` and `X-Accel-Buffering: no`, without
changing the shared proxy. Public HTTPS range checks returned 206 and matched
the first and last 64 KiB against the verified local ISO. The GitHub release is
an alternate download for users with repository access.
Its completed ISO asset is 1,992,949,760 bytes; GitHub's server-computed SHA256
matches the VM-tested image checksum above.

The complete CI image build, checksum verification and artifact upload passed in
[run 44003946462](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44003946462),
using the pinned mirrored packages and signed Debian/Proxmox dependencies.
Eleven ISO regression tests and eleven APT protection tests passed. The CI-built
ISO has its own timestamp/provenance and checksum; the checksum at the top of
this report identifies the separately published, VM-tested image.

## Limits

The tests cover ext4 OS installation on virtual disks. Physical USB boot,
R710 storage-controller behavior, OS installation on ZFS, physical disk failure
and bare-metal recovery were not tested by this ISO validation. Existing backup
and restore reports remain separate from installer tests.
