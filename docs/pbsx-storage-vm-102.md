# PBS-X VM102 on storage

Provisioned on 2026-11-18 at the user's request.

- Hypervisor: `storage`, `10.0.1.144`.
- VM: `102`, name `pbsx`, hostname `pbsx.home.arpa`.
- Web UI: `https://10.0.1.39:8007/`.
- Network: DHCP on `ens18`, MAC `BC:24:11:8B:B4:CC`, bridge `vmbr0`.
  Reserve the MAC in DHCP if the current address must remain fixed.
- CPU/RAM: four host-type vCPUs, 4 GiB RAM, balloon minimum 2 GiB.
- Firmware: UEFI, Secure Boot enabled and verified after updating.
- Boot disk: 32 GiB, `local-lvm:vm-102-disk-1`; EFI variables are on
  `local-lvm:vm-102-disk-0`.
- Backup disk: 500 GiB, `mediapool:vm-102-disk-0`, serial `PBSXDATA102`.
- Autostart and deletion protection enabled.

`mediapool` originally allowed container root disks only. Its content list now
allows `rootdir,images`, retaining its existing container support and other
settings. No existing guests, pool topology or host packages were changed.

## Datastore

The new backup disk was checked by VM UUID, serial, size, empty signature list
and absence of mounted filesystems before formatting. It uses ext4, label
`PBSX_BACKUPS`, mounted at `/mnt/datastore/backups` with `relatime`.
The filesystem UUID is `c764a867-5740-4d62-ba11-1c10adb34e24` and is used in
`/etc/fstab`. The mount is required at boot. PBS service drop-ins also require
this mount, preventing the datastore directory from silently using boot-disk
space if the data volume is unavailable.

Datastore name: `backups`. Garbage collection is scheduled daily. No automatic
pruning policy was imposed. The data disk has `backup=0` for Proxmox VM backups
to avoid recursively copying the backup datastore through vzdump. This does
not disable PBS backups stored inside it. The boot disk remains included in
normal VM backups. Discard and weekly fstrim are enabled.

## Access

Root SSH access accepts the existing development key and the owner's public
keys fetched from `https://sshid.io/chunibyo`. The new guest's SSH host key was
obtained through the trusted hypervisor guest agent before direct SSH access.
No private SSH key was copied into the VM.

The two private password prompts timed out. Root's password remains locked;
there is no default web password. To enable the `root@pam` web login, connect
with an authorized SSH key and choose a password interactively:

```sh
ssh -t root@10.0.1.39 passwd
```

## Installation and verification

Installed from the checksum-verified current PBS-X `4.2.9-1+pbsx2` ISO with
repository support `1.0.3`. A private unattended derivative supplied key-only
bootstrap, DHCP installation settings and the guest agent. The 500 GiB disk
was attached after OS installation. The temporary derivative was detached and
removed after verification; the VM boots from its installed disk.

All signed Debian/Proxmox OS updates were installed while preserving the three
PBS-X executable packages. The VM then rebooted successfully into
`7.0.14-29-pve`, with Secure Boot still enabled. Package audit is clean and the
final upgrade simulation reported no pending updates. Its installed signed
PBS-X APT source remains `https://pbsx.rikka.ca/apt`.

Verified:

- PBS services and the guest agent are active after reboot.
- The 500 GiB datastore mount persists and is independent of the boot disk.
- External HTTPS login-realm API succeeds using the guest certificate obtained
  through pinned SSH as the trust anchor.
- A real backup/restore through the PBS API reproduced all three fixture files
  with matching SHA256 hashes.
- The temporary test group and datastore-scoped token were removed; token and
  ACL listings were empty afterward.
- Existing containers `100` (`jellyfin`) and `101` (`staging`) remain running.

Provisioning scripts and logs are under ignored
`artifacts/iso/storage144-vm102/`. They contain no plaintext administrator
password. No hypervisor or development VM was restarted; only the newly created
guest was rebooted. Host configuration changes were limited to the new VM and
enabling VM-disk content on the requested storage pool.
