#!/bin/bash
# Exercise the real APT resolver with a local synthetic repository. No system
# repositories, package status, preferences or installed packages are changed.
set -euo pipefail
project=$(cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d /tmp/pbsx-apt-policy.XXXXXX)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/repo" "$work/lists/partial" "$work/cache/archives/partial" "$work/empty"
arch=$(dpkg --print-architecture)
for version in 4.2.9-1+pbsx1 4.2.9-1+pbsx2 99.0-1; do
    cat >> "$work/repo/Packages" <<EOF
Package: proxmox-backup-server
Version: $version
Architecture: $arch
Maintainer: PBS-X test <pbsx@localhost>
Filename: fake-$version.deb
Size: 1
SHA256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
Description: Disposable package selection fixture, never installed

EOF
done
cat >> "$work/repo/Packages" <<EOF
Package: pbsx-unrelated-policy-test
Version: 1.0
Architecture: $arch
Maintainer: PBS-X test <pbsx@localhost>
Filename: fake-unrelated.deb
Size: 1
SHA256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
Description: Unrelated package remains available

EOF
printf 'deb [trusted=yes] file:%s/repo ./\n' "$work" > "$work/sources.list"
touch "$work/status"
options=(-o "Dir::Etc::sourcelist=$work/sources.list" -o "Dir::Etc::sourceparts=$work/empty"
    -o "Dir::Etc::preferences=$project/etc/pbsx.pref" -o "Dir::Etc::preferencesparts=$work/empty"
    -o "Dir::State::lists=$work/lists" -o "Dir::State::status=$work/status"
    -o "Dir::Cache=$work/cache" -o "APT::Sandbox::User=$(id -un)"
    -o Acquire::Languages=none -o Debug::NoLocking=1)
apt-get "${options[@]}" update > "$work/update.log" 2>&1 || { cat "$work/update.log"; exit 1; }
policy=$(apt-cache "${options[@]}" policy proxmox-backup-server)
grep -q 'Candidate: 4.2.9-1+pbsx2' <<< "$policy"
grep -q '99.0-1 -1' <<< "$policy"
apt-cache "${options[@]}" policy pbsx-unrelated-policy-test | grep -q 'Candidate: 1.0'
# A newer installed fork must not be downgraded to an older repository build.
cat > "$work/status" <<EOF
Package: proxmox-backup-server
Status: install ok installed
Architecture: $arch
Version: 4.2.9-1+pbsx3
Maintainer: PBS-X test <pbsx@localhost>
Description: Installed fork fixture

EOF
apt-cache "${options[@]}" policy proxmox-backup-server | grep -q 'Candidate: 4.2.9-1+pbsx3'
printf 'PASS: fork updates selected, newer upstream rejected, unrelated updates allowed, no automatic downgrade\n'
