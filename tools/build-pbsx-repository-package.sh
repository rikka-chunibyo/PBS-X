#!/bin/bash
# Build the small repository/keyring/protection package; no Rust rebuild needed.
set -euo pipefail
[[ $# == 2 ]] || { echo 'Usage: script ARCHIVE_PUBLIC_KEY OUTPUT_DIRECTORY' >&2; exit 2; }
project=$(cd -- "$(dirname -- "$0")/.." && pwd)
version=$(cat "$project/packaging/pbsx-repository/VERSION")
dpkg --validate-version "$version"
key=$(realpath "$1")
mkdir -p -- "$2"
output=$(realpath "$2")
work=$(mktemp -d /tmp/pbsx-repository-package.XXXXXX)
trap 'rm -rf -- "$work"' EXIT
root=$work/root
install -d -m 0755 "$root/DEBIAN" "$root/usr/share/keyrings" "$root/usr/lib/pbsx-repository" \
    "$root/usr/lib/systemd/system" "$root/etc/apt/sources.list.d" "$root/etc/apt/preferences.d" "$root/etc/apt/apt.conf.d"
gpg --batch --yes --dearmor --output "$root/usr/share/keyrings/pbsx-archive-keyring.gpg" "$key"
chmod 0644 "$root/usr/share/keyrings/pbsx-archive-keyring.gpg"
install -m 0755 "$project/packaging/pbsx-repository/apt-guard.py" "$root/usr/lib/pbsx-repository/apt-guard.py"
ln -s apt-guard.py "$root/usr/lib/pbsx-repository/pre-install"
ln -s apt-guard.py "$root/usr/lib/pbsx-repository/post-install"
install -m 0644 "$project/packaging/pbsx-repository/80pbsx-protection" "$root/etc/apt/apt.conf.d/"
install -m 0644 "$project/packaging/pbsx-repository/"*.service "$project/packaging/pbsx-repository/"*.timer "$root/usr/lib/systemd/system/"
cat > "$root/etc/apt/sources.list.d/pbsx.sources" <<'EOF'
Types: deb
URIs: https://pbsx.rikka.ca/apt
Suites: trixie
Components: main
Architectures: amd64
Signed-By: /usr/share/keyrings/pbsx-archive-keyring.gpg
EOF
cat "$project/etc/pbsx.pref" > "$root/etc/apt/preferences.d/pbsx-repository.pref"
cat >> "$root/etc/apt/preferences.d/pbsx-repository.pref" <<'EOF'

Package: pbsx-repository
Pin: release o=PBS-X
Pin-Priority: 990

Package: *
Pin: release o=PBS-X
Pin-Priority: -1
EOF
cat > "$root/DEBIAN/control" <<EOF
Package: pbsx-repository
Version: $version
Section: admin
Priority: optional
Architecture: all
Maintainer: PBS-X developers <apt@pbsx.rikka.ca>
Depends: python3, ca-certificates, systemd, tar
Description: PBS-X repository trust and protected APT updates
 Installs the PBS-X source, signing key and package policy, with server state
 backups, active-work checks and service recovery around APT transactions.
EOF
cat > "$root/DEBIAN/conffiles" <<'EOF'
/etc/apt/sources.list.d/pbsx.sources
/etc/apt/preferences.d/pbsx-repository.pref
/etc/apt/apt.conf.d/80pbsx-protection
EOF
cat > "$root/DEBIAN/preinst" <<'EOF'
#!/bin/sh
set -e
. /etc/os-release
if [ "$ID" != debian ] || [ "$VERSION_ID" != 13 ]; then
    echo 'PBS-X repository package requires Debian 13' >&2; exit 1
fi
if [ "$(dpkg-query -W -f='${db:Status-Status}' pve-manager 2>/dev/null || true)" = installed ]; then
    echo 'Use a dedicated PBS server, not a Proxmox VE hypervisor' >&2; exit 1
fi
EOF
cat > "$root/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = configure ]; then
    systemctl daemon-reload
    systemctl enable --now pbsx-apt-recovery.timer
fi
EOF
cat > "$root/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = remove ]; then
    /usr/lib/pbsx-repository/post-install
    systemctl disable --now pbsx-apt-recovery.timer || true
fi
EOF
cat > "$root/DEBIAN/postrm" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = remove ] || [ "$1" = purge ]; then systemctl daemon-reload || true; fi
EOF
chmod 0755 "$root/DEBIAN/"{preinst,postinst,prerm,postrm}
(cd "$root"; find etc usr -type f -print0 | sort -z | xargs -0 md5sum > DEBIAN/md5sums)
dpkg-deb --root-owner-group --threads-max=1 --build "$root" "$output/pbsx-repository_${version}_all.deb"
