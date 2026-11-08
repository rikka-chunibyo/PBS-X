# Signed APT repository rehearsal — 2026-11-08

Development branch: `pbsx-apt-repository-tests`.

This report records the initial lab stage. The subsequent
[public bootstrap and APT protection](pbsx-apt-installation.md) implement the
missing safeguards described below and use the public domain through NPM.

## Scope

Used Ubuntu LXC116 (`pbs-x-apt`, `10.0.1.37`) to serve a signed lab repository
over HTTPS, and the existing disposable Debian 13 PBS-X VM115 (`10.0.1.35`)
as the real APT client. No hypervisor services, dev VM or unrelated guests were
changed. The stopped PBS-X build/test VMs remained stopped.

The baseline was the existing `4.2.9-1+pbsx1` server, client and file-restore
packages. The upgrade was the previous **version-only test repack**
`4.2.9-1+pbsx2`, with the same executable payload. This validates transport,
trust, version selection and package lifecycle; it does not establish
compatibility with a future configuration schema.

## Repository and trust

- URL: `https://10.0.1.37/lab/`; component `main`, architecture `amd64`.
- Suites: `trixie` for the current test and `trixie-baseline` for rollback.
- Origin: `PBS-X-Lab`. Both suites are explicitly labeled lab-only.
- Short-lived OpenPGP signing key, fingerprint:
  `B1EC40874E33D6C7480F92ED64EAADDC651D6F98`.
- Private lab TLS CA and server certificate, with the container IP/hostname in
  the certificate SAN. Certificates and signing key expire after 30 days;
  signed repository metadata has a seven-day validity period.
- Public CA/signing certificates were copied over verified SSH. VM115 used
  `Signed-By` plus a host-specific APT TLS CA option. Repository TLS certificate
  and hostname checks remained enabled; no insecure/trusted bypass was used.
- Signing state, TLS private keys and reprepro database/configuration are outside
  the web root in root-only directories. These are test keys, not production
  key-management or recovery arrangements.

## Passed checks

1. APT accepted valid signed metadata over verified HTTPS, selected all three
   fork packages and downloaded a client package matching the original artifact.
2. APT rejected an untrusted TLS certificate and an unrelated signing key.
3. APT rejected modified signed metadata and an unsigned repository.
4. APT rejected correctly signed but expired metadata.
5. APT rejected a modified package using the signed index checksum.
6. TLS rejected a hostname absent from the certificate SAN.
7. Ordinary system `apt-get update` worked with the lab source and its scoped
   trust settings, alongside the existing Debian/Proxmox sources.
8. Reinstalled all three baseline packages through the repository. PBS services
   stayed healthy and a real native ZFS snapshot backup/restore passed.
9. Published the version-only upgrade. Ordinary `apt upgrade` simulation selected
   all three new fork packages; available upstream executable versions remained
   rejected by the existing APT policy. A targeted real upgrade passed.
10. Withdrew the newer fixture. Ordinary APT did not downgrade the installed
    newer fork. Explicit `--allow-downgrades` rollback to the retained baseline
    passed through the same signed HTTPS feed.
11. Native backup/restore passed after both upgrade and rollback, using the
    existing enrolled agent and historical backup ownership. Restored contents,
    nanosecond timestamps, special modes, ownership, ACLs, capabilities, links
    and sparse allocation matched the fixture.
12. Restarted only the repository container. Ordinary APT update still accepted
    its signed HTTPS feed afterward, with no package or source trust bypass.

## Important upgrade limitation

Before each actual package operation, the test explicitly checked for idle
services and took a protected, coherent manual archive of server configuration
and the PBS-X credential registry under `/var/backups/pbsx-apt-lab-*`.
These private archives remain on VM115; they are not repository artifacts.

Those precautions were performed by the test harness. **The current Debian
packages do not automatically provide the bundle installer's state backup and
active-task checks for plain `apt upgrade`.** These safeguards still need to
be implemented for the production APT workflow. Do not describe this rehearsal
as proving unattended upgrades or automatic recovery from an interrupted upgrade.

## Tools and final state

- `tools/pbsx-apt-lab-repository.sh` is guarded to the repository container and
  creates/publishes only the known lab versions. `init` refuses to replace keys.
- `tools/test-pbsx-apt-vm.py` is guarded to VM115. It requires that VM's existing
  native fixture (`/home/rikka/pbsx-install-e2e.py`) and bootstrap public certs.
  It is a stateful lab driver, not a general client installer.
- APT logs and isolated trust-test state remain under
  `/var/tmp/pbsx-apt-lab-tests` on VM115. A copy of APT logs is retained locally
  under ignored `artifacts/apt-repository-validation/`.
- The repository is returned to baseline `4.2.9-1+pbsx1`; the version-only
  upgrade and deliberate corruption fixtures are withdrawn.
- VM115 is returned to that baseline, with the example native job disabled and
  the lab APT source disabled. Its public bootstrap certs remain for inspection;
  the host-specific APT TLS configuration is removed.

Production publication still needs a full public hostname/TLS arrangement,
release signing/key recovery and rotation, release promotion/retention, and the
upgrade protections above. The supported production installation procedure
remains the [bundle workflow](pbsx-installation.md).

References: [repository signing](https://manpages.debian.org/trixie/apt/apt-secure.8.en.html),
[reprepro](https://manpages.debian.org/trixie/reprepro/reprepro.1.en.html),
[APT host-specific CA option correction](https://bugs.debian.org/1112123).
