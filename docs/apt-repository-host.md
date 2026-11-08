# PBS-X APT repository host

Provisioned on 2026-11-08 for repository hosting. A signed HTTPS **lab** feed
was subsequently tested against VM115; see the
[APT validation report](apt-repository-validation.md). It is not a production
release feed.

## Container

- Proxmox host: `10.0.1.17`; LXC ID: `116`.
- Hostname: `pbs-x-apt`; Ubuntu 26.04 LTS, amd64, unprivileged.
- Resources: 2 cores, 1 GiB RAM, no swap, 16 GiB root filesystem on `storage`.
- Network: DHCP on `vmbr0`; initially `10.0.1.37`.
- MAC address: `BC:24:11:DE:DB:DD`; reserve its DHCP lease before using the
  address as a permanent reverse-proxy target.
- Starts with the hypervisor. Nesting is enabled for the modern systemd
  container environment; no host bind mounts or device passthrough are used.

Ubuntu is used for hosting tools. PBS-X server packages still target Debian 13;
the existing Debian build VM remains the package builder.

## SSH access

Connect as `root` with a public key. Password and keyboard-interactive
authentication are disabled. Authorized keys are the two fetched from
`https://sshid.io/chunibyo` plus this development server's
`/home/rikka/.ssh/id_ed25519.pub`. No private SSH key was copied.

Authorized public-key fingerprints:

```text
ECDSA     SHA256:F19o+xzM/kvZTvR867xRS7idsevud0fv7Ispntkr90k
ECDSA-SK  SHA256:q/WJLDbRyXIC3fpDHFTbVCYNn7MCsbVIYbmFdAVLcn4
ED25519   SHA256:5HnWq9Htajn9Ux80kdvzpZ02NSqtvydk5POap51k1+M
```

The container's Ed25519 SSH host fingerprint is:

```text
SHA256:LvUHV6sBbDGKyDd3EVfN9aTw5VKedAkvxmNHGsqD07M
```

## Repository preparation

Installed tools: nginx, reprepro, GnuPG, curl and CA certificates.

- Static document root: `/srv/pbs-x-apt/public`.
- Private administration state: `/var/lib/pbs-x-apt`, mode `0700`.
- Private package staging: `/srv/pbs-x-apt/incoming`, mode `0700`.
- Local administration notes: `/root/README-pbs-x-apt.txt`.

The lab feed is `https://10.0.1.37/lab/`, with `trixie` and `trixie-baseline`
suites and the `main` component. Its temporary signing key and private TLS CA
expire after 30 days; repository metadata expires after seven days. Public
certificates were bootstrapped to VM115 over SSH. Production client trust and
public DNS/TLS have not been configured.

Lab signing/admin state is under `/var/lib/pbs-x-apt/lab` (mode `0700`), outside
the web root. Only the lab public key, package pool and signed indexes are
published. The deliberately corrupt security-test fixtures were removed, and
VM115's lab APT source is disabled after the rehearsal.

Next work is to choose the full public hostname, route public HTTPS, implement
production signing and key recovery/rotation, publish testing/stable releases, and carry the bundle
installer's state backups and active-task handling into the APT upgrade path.
The current supported installation/update method remains the
[release bundle installer](pbsx-installation.md).
