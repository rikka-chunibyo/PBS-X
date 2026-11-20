# Publishing matching APT packages and offline ISOs

Normal stable releases use one maintainer command. It builds the ISO before
changing stable APT, then activates both from the same four package hashes.
The permanent installer link is
**https://pbsx.rikka.ca/iso/latest/pbsx-offline-amd64.iso**. Its sibling checksum
and manifest describe that exact image. Previous images retain versioned URLs.

The maintainer machine needs Python 3, Git, GitHub CLI with repository access,
OpenSSH, curl, GnuPG, Debian package tools and `xorriso` for independent image
inspection. The existing repository container needs Python 3, `dpkg-deb`,
`xorriso`, reprepro, GnuPG and `flock`. Install these dependencies before the
first combined publication. Image construction happens on the hosted runner;
the container only verifies and serves the completed image.

From the trusted development checkout, after building/testing a package bundle
and committing/pushing the publishing source:

```sh
python3 tools/publish-pbsx-server.py release \
  --bundle artifacts/releases/pbsx-VERSION-amd64 \
  --output artifacts/server-releases/UNIQUE_RELEASE_DIRECTORY \
  --host root@10.0.1.37 \
  --identity /home/rikka/.ssh/id_ed25519 \
  --known-hosts artifacts/iso/private-test/repository-known-hosts
```

The bundle must have `SHA256SUMS` and either the complete matching core trio,
a `pbsx-repository` update, or both. The publisher verifies the bundle and
downloads the current stable four-package set through the signed feed. It
overlays the candidate packages to form the exact next installer inputs.
Repository-support-only changes therefore rebuild the ISO too.
Published package versions are immutable: any change to package bytes requires
a newer Debian version. Preparation rejects rebuilt bytes under the same or an
equivalent version and rejects downgrades against stable APT, even before the
first ISO has been published for that repository.

The publisher mirrors those four verified packages and `iso-inputs.json` into
a GitHub prerelease, then dispatches `.github/workflows/iso.yml` with its exact
tag, version and a unique request ID. The initial prerelease is labelled as
build inputs; it does not announce a completed installer. CI checks have read-only
access. The dispatched image-build job gets an ephemeral repository write token
to mirror checked ISO assets to the release identifying that exact builder
commit. It has no SSH deployment or signing secret. This mirror avoids public
edge browser challenges and a redundant 2 GB upload through the maintainer's
Internet connection.

CI verifies the mirrored hashes/identities, pinned upstream ISO, signed runtime
dependencies, offline dependency closure, final embedded package bytes and
BIOS/UEFI boot entries. The maintainer publisher waits for a successful run from
the exact pushed source, downloads the artifact (or the checked GitHub release
mirror if a quota prevented the optional artifact upload), and checks its checksum,
embedded manifest, version, package hashes and clean build provenance.

Only then does publication upload and verify GitHub assets and transfer the
bundle over the existing trusted SSH connection. Updated publication helpers
are backed up privately and installed atomically when their source changes.
Signing keys remain on the repository container.

## Stable activation and retries

The repository helper refuses stable publication without a verified matching
ISO, including support-package-only updates. It prepares the complete public
image directory before touching APT. After exporting/checking the signed index,
it verifies that the live four-package set still matches and atomically switches
`iso/latest`. Repository publication and metadata refresh share a file lock.

The website and generated current app-download page link to the latest image.
Application payloads and previous ISO bytes are not changed. Public files remain
readable even when administration uses `umask 077`. The consumed private image
transfer copy is removed after successful publication; public and GitHub copies
remain available.

For separate build review, use `prepare` with the bundle/output arguments, then:

```sh
python3 tools/publish-pbsx-server.py publish \
  --output artifacts/server-releases/EXISTING_PREPARED_DIRECTORY \
  --host root@10.0.1.37 \
  --known-hosts artifacts/iso/private-test/repository-known-hosts
```

Use that `publish` command to retry an interrupted upload/activation. It reuses
the exact prepared bytes and rejects conflicting completed GitHub assets or
immutable public images. A failed build leaves stable APT and the latest ISO
unchanged. If activation fails after an APT export, the prior complete ISO link
remains readable and the repository records the staging path in
`/var/lib/pbs-x-apt/release/iso-publication-pending`; retry the prepared publication
to finish the link switch. Do not reconstruct different bytes under the same
release identity. `--run-id` on `prepare` can collect a successful ISO run from
the same source when recovering from an interrupted build collection.
If a hosted build fails after completing some immutable ISO uploads, start a
fresh build with a unique `--tag`; completed image bytes are never overwritten.
An unfinished transaction must be completed before a different stable release
can start; its durable pending marker cannot be overwritten by a newer caller.
The existing metadata-refresh service also finishes a pending, fully staged
transaction before exporting refreshed indexes. It revalidates/reincludes the
whole package set first, so interruption partway through repository inclusion
cannot be turned into a mixed package family by a later signature refresh.

The raw repository command remains available for signed `trixie-testing`
validation. Its `trixie` path enforces ISO preflight, so the former core-only
stable publication command cannot silently leave the installer behind. Daily
`refresh` only renews signatures and does not rebuild an unchanged package set.

The ISO incorporates the current PBS-X release and runtime dependencies resolved
at build time. Later Debian/Proxmox security updates can still become available;
normal installed-system APT updates remain enabled.
