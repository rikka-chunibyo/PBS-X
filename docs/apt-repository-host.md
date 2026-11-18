# PBS-X APT repository host

Provisioned on 2026-11-08 for repository hosting. The public signed feed is now
`https://pbsx.rikka.ca/apt`; see the [public installation guide](pbsx-apt-installation.md).
The earlier [lab rehearsal](apt-repository-validation.md) remains as validation
history. The user routes public HTTPS through Nginx Proxy Manager to HTTP
backend `10.0.1.37:80`; the container has no active TLS listener.

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

Installed tools: nginx, reprepro, GnuPG, curl, CA certificates, Python 3 and
Debian package tools. `xorriso` was added for independent verification of
matching offline images during combined stable publication; `flock` serializes
publication and refresh. See [server/ISO publishing](pbsx-server-publishing.md).

- Static document root: `/srv/pbs-x-apt/public`.
- Private administration state: `/var/lib/pbs-x-apt`, mode `0700`.
- Private package staging: `/srv/pbs-x-apt/incoming`, mode `0700`.
- Local administration notes: `/root/README-pbs-x-apt.txt`.

The public feed uses suite `trixie`, component `main`, and release signing key
`85177E09BEAB50C669C0D68125C3A26BDAB0FCB4`. `pbsx-repository` installs the
client's signing key, source, package policy and protected APT transaction hooks.
VM115 uses this public source with normal system TLS trust.

Release signing/admin state is under `/var/lib/pbs-x-apt/release` (mode `0700`),
outside the web root. Public keys, packages and signed indexes live under the
static root. `pbsx-apt-refresh.timer` refreshes signed metadata daily. Encrypted
signing-key recovery was tested and a protected second copy was approved by the
user. No secret recovery material is published.

The old lab state remains under `/var/lib/pbs-x-apt/lab`, with its source disabled
on VM115 and its TLS site disabled on the repository container. Deliberately
corrupt fixtures and the version-only upgrade were withdrawn. See the public
guide for release publishing, key lifetime and remaining validation limits.
