# Combined APT and offline ISO publication validation

Validated on 2026-11-18 against the PBS-X repository container. The current
installer contains server/client/file-restore `4.2.9-1+pbsx2` and
`pbsx-repository` `1.0.3`.

- [Current ISO](https://pbsx.rikka.ca/iso/latest/pbsx-offline-amd64.iso)
- [Checksum](https://pbsx.rikka.ca/iso/latest/pbsx-offline-amd64.iso.sha256)
- [Exact build provenance](https://pbsx.rikka.ca/iso/latest/iso-publication.json)
- [Immutable GitHub release](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-iso-4.2.9-1+pbsx2-repo-1.0.3-d4e22c0a7b)
- [Successful hosted build](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44243247189)

Builder source: `d4e22c0a7b4fb53891fd279483a8cb10775ee43e`, clean checkout.
Image size: 1,993,015,296 bytes. SHA256:
`e36449a7b11cf9a69f7c16a8f116db0b6cf14113d7540d27e9ccc532aae0f692`.

## Automated checks

All 72 local tests passed: 20 combined-publication tests, 14 ISO tests,
11 APT-publication tests, 16 client-publication tests and 11 installed APT-guard
tests. The 20 new cases cover corrupt or mismatched inputs, support-only
updates, provenance, unsafe paths, immutable releases, interrupted activation
and retries, pending-transaction conflicts, and checked workflow/release assets.

The hosted build fetched the exact mirrored four-package input set, verified
the official base image and signed runtime dependencies, checked offline
dependency closure, and extracted/hashed package bytes from the final image.
The maintainer and repository independently inspected the actual ISO's embedded
manifest and BIOS/UEFI boot entries before activating it.

The dispatched build uploaded verified ISO assets directly to its exact input
release. GitHub's server-computed image digest matched the prepared bytes.
The maintainer collected the successful run and completed HTTPS/GitHub and
stable APT publication. An earlier complete run at builder `a19dc7d904` also
published successfully; its immutable image remains available.

## Live repository and download checks

Stable publication of a core-only bundle without a matching ISO was rejected
before changing the public signed index or creating a pending marker. The same
bundle remained publishable to `trixie-testing` without changing stable APT.

Public HTTPS verification checked:

- Exact equality of checksum, manifest, boot report, inputs and provenance with
  the prepared release.
- The public InRelease signature using the tracked archive key, its package
  index hash, and all four package identities/hashes against the ISO inputs.
- Root and current application download pages linking to the latest installer.
- HTTP 206 responses for the first and last 64 KiB, matching the local image.
- A complete streamed image download matching the size and SHA256 above.

A normal metadata refresh retained the current image's link, inode and digest.
For recovery validation, the consumed private transfer image was restored from
the current public image, and the real preparation command recorded its durable
pending marker. The real metadata-refresh command then replayed the entire
prepared package publication and completed activation. The marker cleared,
the public image stayed identical, and all four stable package hashes still
matched. The temporary private transfer image was removed afterward. This was
an idempotent recovery test of the current release, without stopping a process
or intentionally exposing a mixed package set. Published application payload
checksums also remained valid.

## Scope and limits

This validation used only the PBS-X repository container. It did not start or
restart the development VM, hypervisor or installation-test VMs. Existing
versioned public images and client payloads were retained. Helper deployments
kept private rollback copies. The hosted build used an ephemeral GitHub write
token; SSH deployment credentials and APT signing keys stayed outside CI.

This image received package, media and publication checks; it was not installed
in a new VM during this validation. Earlier BIOS/UEFI Secure Boot offline install
checks apply to the historical `+pbsx1` image and are documented separately in
[the installer validation report](pbsx-offline-iso-validation.md). Physical USB,
controller compatibility and bare-metal recovery remain hardware tests.

The latest installer matches the current PBS-X package set. Subsequent
Debian/Proxmox security updates can still become available. See
[publishing and recovery instructions](pbsx-server-publishing.md) for the
maintainer command used for future stable releases.
