# Resumable backup release — 2026-11-20

Published server version: **`4.2.9-1+pbsx3`**, Debian 13 amd64, with the
matching server, client and file-restore package trio. Repository support remains
`1.0.3`. Android **`0.5.12-preview`** (code 21) requires this server update;
update the server through normal APT before installing the APK.

- Signed update feed: https://pbsx.rikka.ca/apt, suite `trixie`.
- [Current offline installer](https://pbsx.rikka.ca/iso/latest/pbsx-offline-amd64.iso).
- [Server packages, installer and provenance](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-iso-4.2.9-1+pbsx3-repo-1.0.3-3ab63535ae).
- [Android and Windows/Linux clients](https://github.com/rikka-chunibyo/PBS-X/releases/tag/pbsx-clients-2026.11.20-preview.1), also at https://pbsx.rikka.ca/downloads/.

The server supplies the authenticated checkpoint, cancellation, continuation
and paused-snapshot retention behavior described in
[the Android development report](android-development-2026-11-19.md). Existing
ordinary PBS clients remain supported. Jobs still share snapshots when their
namespace and Backup ID match; resuming an interrupted run retains its original
snapshot rather than starting another one.

## Build and lab validation

The clean package source was
`e2517ed9fe6cbef2b8b8a7c47b750c0d4ddf526e`. The normal Debian package build
used Rust/Cargo `1.94.1`, one build worker and materialized Git LFS inputs. Its
workspace tests passed **177 cases**, with zero failures and the two existing
ignored tests (`catar_simple` and `worker_task_abort`) still ignored.

On dedicated lab VM 104, the public bootstrap first upgraded the stock server
to the prior stable `+pbsx2` release. The candidate trio then installed through
normal APT with the existing repository protection hooks. Datastore, user and
ACL configuration bytes were preserved, both services and the recovery timer
were active, the recovery marker cleared, and `dpkg --audit` was clean.

All four live integration cases passed against the actual packaged server:

- Resume the original snapshot without reopening completed files, then restore
  and compare exact bytes.
- Recover after a forced connection loss and garbage collection while paused.
- Reject the wrong continuation identity and ensure cancellation removes only
  its own incomplete snapshot, protecting completed snapshots.
- Resume an encrypted Android-core backup in a fresh client, verify its final
  candidate and exact restore, and reject tampering.

The packaged ordinary PBS CLI also passed backup/restore before and after the
upgrade: exact hashes, sparse-file contents, empty files/directories, Unicode,
symlink targets and hardlink identity. These checks verify sparse contents,
not preservation of sparse allocation on every restore filesystem.

A fresh lab APT metadata refresh after publication selected `+pbsx3` for all
three packages from the public signed feed. The synthetic datastore, scoped
user/token, both ACLs and private fixture data were removed afterward. Only
private validation logs were retained. Both lab VMs were returned to stopped;
the development VM remained running, and the phone's backup server was untouched.

The builder needed a separate disposable 64 GiB scratch disk, excluded from VM
backups. A temporary 4 GiB guest swap file bounded build memory pressure and was
disabled and removed after the successful build. No host memory or ARC settings
were changed.

## Matching ISO and publication

[ISO workflow 44701218093](https://github.com/rikka-chunibyo/PBS-X/actions/runs/44701218093)
passed at builder source `3ab63535aeef429727c359fc31cd60dc918b5259`. It verified
the pinned upstream media, signed runtime dependencies, offline dependency
closure, embedded package hashes and BIOS/UEFI boot entries. Image size is
1,993,080,832 bytes; SHA256:
`41bd14cfeb740088e031761ca6f07a15004c0f60c0905977ffa1da6e2df4bec5`.

The publisher independently checked the image and exact four-package input set,
then activated stable APT and the matching current ISO. Previous versioned
images and client releases remain available. Public checks verified the archive
signature, signed index hash, all four package identities/hashes, metadata,
website links and first/last HTTP 206 ranges against the prepared image.
A complete public HTTPS download of all 1,993,080,832 image bytes matched the
SHA256 above. Each of the four public APT package downloads also matched its
size and SHA256 in the authenticated index and exact ISO inputs. All three
applications and both client metadata files matched their prepared hashes from
both public HTTPS and GitHub; the current download page and metadata select the
new release and state Android's minimum server version.

GitHub Actions artifact storage continued rejecting uploads after old artifact
cleanup. Explicit dispatched builds now mirror mandatory, verified assets into
GitHub releases. The native transport stays draft, tied to the exact successful
run/attempt, source and per-asset checksums; ISO assets use their exact input
release and source. Normal CI remains read-only. The optional Actions artifact
copy can fail without blocking checked release collection. No billing changes,
signing-key transfer or removal of source/hash/test gates was required.

All **93 publication/repository regression cases** passed locally: release
transport 6, source export 5, client publication 18, ISO publication 28, ISO
inputs 14, APT publication 11 and installed APT guard 11.

## Remaining hardware checks

This ISO received media/package/publication checks, not a new full BIOS/UEFI
installation in a fresh VM. Earlier offline-install validation remains linked
from [the ISO report](pbsx-offline-iso-validation.md).

Physical Samsung Shizuku restores and charging/Wi-Fi/foreground pause-resume
checks remain necessary. Provider exports restore as files; importing them into
live contacts, SMS/MMS or calendar databases remains unavailable. Credential
rotation during a paused run still requires a deliberate recovery flow.
