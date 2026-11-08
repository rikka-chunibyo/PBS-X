#!/bin/bash
# Install a locally built, checksum-verified PBS-X package bundle on Debian 13.
set -euo pipefail
shopt -s nullglob
allow_downgrade=false
if [[ ${1:-} == --allow-downgrade ]]; then allow_downgrade=true; shift; fi
if [[ $# != 1 || $EUID != 0 ]]; then
    echo 'Usage: sudo install-pbsx.sh [--allow-downgrade] BUNDLE_DIRECTORY' >&2
    exit 2
fi
. /etc/os-release
[[ $ID == debian && $VERSION_ID == 13 ]] || { echo 'Debian 13 is required' >&2; exit 1; }
[[ $(dpkg --print-architecture) == amd64 ]] || { echo 'This bundle requires amd64' >&2; exit 1; }
if [[ $(dpkg-query -W -f='${db:Status-Status}' pve-manager 2>/dev/null || true) == installed ]]; then
    echo 'Install PBS-X in a dedicated server/VM, not on the Proxmox hypervisor' >&2
    exit 1
fi
bundle=$(realpath -- "$1")
cd -- "$bundle"
sha256sum --check --strict SHA256SUMS
packages=()
version=
server=
for name in proxmox-backup-server proxmox-backup-client proxmox-backup-file-restore; do
    files=("${name}_"*.deb)
    [[ ${#files[@]} == 1 ]] || { echo "Expected exactly one $name package" >&2; exit 1; }
    file=${files[0]}
    grep -Fxq -- "$(sha256sum -- "$file")" SHA256SUMS || { echo "Package is not covered by SHA256SUMS: $file" >&2; exit 1; }
    [[ $(dpkg-deb --field "$file" Package) == "$name" ]] || exit 1
    package_version=$(dpkg-deb --field "$file" Version)
    [[ $package_version == *+pbsx* && $(dpkg-deb --field "$file" Architecture) == amd64 ]] || exit 1
    if [[ -z $version ]]; then version=$package_version; else [[ $version == "$package_version" ]] || { echo 'Mixed package versions' >&2; exit 1; }; fi
    installed=$(dpkg-query -W -f='${Version}' "$name" 2>/dev/null || true)
    if [[ -n $installed ]] && dpkg --compare-versions "$installed" gt "$version" && ! $allow_downgrade; then
        echo "Refusing to downgrade $name from $installed to $version; use --allow-downgrade for a planned rollback" >&2
        exit 1
    fi
    packages+=("$bundle/$file")
    if [[ $name == proxmox-backup-server ]]; then server=$file; fi
done
dpkg-deb --fsys-tarfile "$server" | tar -xOf - ./etc/apt/preferences.d/pbsx.pref | grep -Fxq 'Pin: version /.*[+]pbsx.*/'
command -v python3 >/dev/null
command -v curl >/dev/null
enterprise_source=/etc/apt/sources.list.d/pbs-enterprise.sources
enterprise_existed=false
if [[ -e $enterprise_source ]]; then enterprise_existed=true; fi
if systemctl is-active --quiet proxmox-backup.service; then
    proxmox-backup-debug api get /nodes/localhost/tasks --running true --output-format json |
        python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)==[] else "PBS tasks are active; retry when idle")'
fi
# Resolve/download dependencies before service downtime. APT repositories and
# their trusted signing keys must already be configured by the base install.
apt_options=(-y -o Dpkg::Options::=--force-confold)
if $allow_downgrade; then apt_options+=(--allow-downgrades); fi
DEBIAN_FRONTEND=noninteractive apt-get "${apt_options[@]}" --download-only install "${packages[@]}"
backup=$(mktemp -d /var/backups/pbsx-install-$(date -u +%Y%m%d-%H%M%S).XXXXXX)
chmod 0700 "$backup"
dpkg-query -W -f='${Package}\t${Version}\n' > "$backup/installed-packages.tsv"
paths=()
for path in etc/proxmox-backup var/lib/proxmox-backup/pbsx-agent-credentials var/lib/proxmox-backup/host-status var/lib/proxmox-backup/pbsx-host-runs var/lib/proxmox-backup/pbsx-enrollment; do
    if [[ -e /$path ]]; then paths+=("$path"); fi
done
restart=false
if systemctl is-active --quiet proxmox-backup.service proxmox-backup-proxy.service; then restart=true; fi
finish() {
    result=$?
    if ((result)) && $restart; then systemctl start proxmox-backup.service proxmox-backup-proxy.service || true; fi
    if ((result)); then echo "Install failed; protected pre-install state: $backup" >&2; fi
    exit "$result"
}
trap finish EXIT
if $restart; then
    # Recheck after downloads, then stop the scheduler/control plane before
    # taking a coherent state copy and replacing the binaries.
    proxmox-backup-debug api get /nodes/localhost/tasks --running true --output-format json |
        python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)==[] else "PBS tasks became active; installation cancelled")'
    systemctl stop proxmox-backup-proxy.service proxmox-backup.service
fi
if ((${#paths[@]})); then tar --acls --xattrs -C / -cpf "$backup/server-state.tar" -- "${paths[@]}"; fi
DEBIAN_FRONTEND=noninteractive apt-get "${apt_options[@]}" install "${packages[@]}"
# A plain Debian base already has a user-selected PBS repository before this
# install. Do not enable an additional subscription-only source merely because
# the inherited server package supplies its default conffile. Existing ISO or
# administrator-configured enterprise sources retain their original settings.
if ! $enterprise_existed && [[ -f $enterprise_source ]]; then
    python3 - "$enterprise_source" <<'PY'
from pathlib import Path
import sys
p = Path(sys.argv[1])
lines = [line for line in p.read_text().splitlines() if not line.lower().startswith('enabled:')]
p.write_text('\n'.join(lines).rstrip() + '\nEnabled: false\n')
PY
fi
for unit in proxmox-backup.service proxmox-backup-proxy.service; do systemctl is-active --quiet "$unit"; done
curl --insecure --fail --silent --show-error --retry 10 --retry-connrefused --retry-delay 1 --max-time 5 https://127.0.0.1:8007/ >/dev/null
echo "PBS-X $version installed. Protected pre-install state: $backup"
echo 'Open https://SERVER-IP:8007. Keep this bundle for package rollback.'
