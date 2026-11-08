#!/bin/bash
# Run on a Debian 13 amd64 builder with the repository's build dependencies.
# The build runs outside the checkout and never removes its build/reports files.
set -euo pipefail
shopt -s nullglob
project=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd -- "$project"
. /etc/os-release
[[ $ID == debian && $VERSION_ID == 13 ]] || { echo 'Build on Debian 13' >&2; exit 1; }
command -v git-lfs >/dev/null || { echo 'Install git-lfs and materialize the source with git lfs pull' >&2; exit 1; }
[[ -z $(git lfs ls-files | awk '$2 == "-" { print }') ]] || { echo 'Git LFS pointer files remain; run git lfs pull' >&2; exit 1; }
git diff --quiet && git diff --cached --quiet || { echo 'Commit source changes before a release build' >&2; exit 1; }
[[ -z $(git ls-files --others --exclude-standard) ]] || { echo 'Untracked source files exist; commit or ignore them before building' >&2; exit 1; }
version=$(dpkg-parsechangelog -SVersion)
arch=$(dpkg --print-architecture)
[[ $version == *+pbsx* && $arch == amd64 ]] || exit 1
jobs=${PBSX_BUILD_JOBS:-2}
[[ $jobs =~ ^[1-9][0-9]*$ ]] || exit 1
output=${1:-"$project/artifacts/releases/pbsx-$version-$arch"}
[[ ! -e $output ]] || { echo 'Output directory already exists; choose a new path' >&2; exit 1; }
work=$(mktemp -d "${TMPDIR:-/tmp}/pbsx-release.XXXXXX")
echo "Build directory and logs: $work"
mkdir "$work/source"
# Read the working files, including materialized Git LFS content. git archive
# alone would export pointer files from this repository's older LFS commits.
git ls-files -z | tar --null -T - -cf - | tar -xf - -C "$work/source"
git rev-parse HEAD > "$work/source/.repoid"
rm -f "$work/source/.cargo/config.toml"
(
    cd "$work/source"
    dpkg-checkbuilddeps -Pnodoc
    env CARGO_BUILD_JOBS="$jobs" DPKG_DEB_THREADS_MAX="${DPKG_DEB_THREADS_MAX:-1}" DEB_BUILD_PROFILES=nodoc \
        DEB_BUILD_OPTIONS="${DEB_BUILD_OPTIONS:-parallel=$jobs}" \
        dpkg-buildpackage -b -us -uc > "$work/package-build.log" 2>&1
)
mkdir -p -- "$output"
output=$(realpath -- "$output")
for name in proxmox-backup-server proxmox-backup-client proxmox-backup-file-restore; do
    cp -- "$work/${name}_${version}_${arch}.deb" "$output/"
done
cp -- "$project/tools/install-pbsx.sh" "$output/"
for file in "$work/"*.buildinfo "$work/"*.changes; do cp -- "$file" "$output/"; done
if [[ -n ${PBSX_AGENT_BINARY:-} ]]; then
    install -m 0755 "$PBSX_AGENT_BINARY" "$output/pbsx-host-agent-linux-amd64"
fi
{
    printf 'PBS-X package version: %s\nArchitecture: %s\nSource commit: ' "$version" "$arch"
    git rev-parse HEAD
    printf 'Build options: %s\n' "${DEB_BUILD_OPTIONS:-parallel=$jobs}"
    dpkg-query -W rustc cargo
} > "$output/BUILD-INFO.txt"
(
    cd -- "$output"
    files=(*.deb *.buildinfo *.changes install-pbsx.sh BUILD-INFO.txt)
    if [[ -f pbsx-host-agent-linux-amd64 ]]; then files+=(pbsx-host-agent-linux-amd64); fi
    sha256sum -- "${files[@]}" > SHA256SUMS
)
echo "Package bundle: $output"
