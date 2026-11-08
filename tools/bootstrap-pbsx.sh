#!/bin/bash
# Install PBS-X on an existing PBS 4 / Debian 13 amd64 server.
# Datastores are preserved. Future updates use apt update && apt upgrade.
set -euo pipefail
repository=https://pbsx.rikka.ca/apt
fingerprint=85177E09BEAB50C669C0D68125C3A26BDAB0FCB4
check=false
reinstall=false
while (($#)); do
    case $1 in
        --check) check=true; shift ;;
        --reinstall) reinstall=true; shift ;;
        --repository-url) [[ $# -ge 2 ]] || exit 2; repository=$2; shift 2 ;;
        --help|-h) echo 'Usage: install-pbs-x.sh [--check] [--reinstall] [--repository-url HTTPS_URL]'; exit 0 ;;
        *) echo "Unknown option: $1" >&2; exit 2 ;;
    esac
done
[[ $EUID == 0 ]] || { echo 'Run this installer as root (or with sudo).' >&2; exit 1; }
. /etc/os-release
[[ $ID == debian && $VERSION_ID == 13 && $(dpkg --print-architecture) == amd64 ]] || {
    echo 'Requires a dedicated PBS 4 server on Debian 13 amd64.' >&2; exit 1;
}
if [[ $(dpkg-query -W -f='${db:Status-Status}' pve-manager 2>/dev/null || true) == installed ]]; then
    echo 'Do not install PBS-X on a Proxmox VE hypervisor. Use a dedicated PBS server or VM.' >&2; exit 1
fi
[[ $(dpkg-query -W -f='${db:Status-Status}' proxmox-backup-server 2>/dev/null || true) == installed ]] || {
    echo 'Install the official PBS 4 ISO first, then run this script inside that PBS system.' >&2; exit 1;
}
[[ $(dpkg-query -W -f='${Version}' proxmox-backup-server) == 4.* ]] || {
    echo 'Upgrade the base system to PBS 4 before installing this PBS-X release.' >&2; exit 1;
}
[[ -d /run/systemd/system ]] || { echo 'A running systemd PBS server is required.' >&2; exit 1; }
for command in curl gpg gpgv python3; do
    if ! command -v "$command" >/dev/null; then
        if $check; then echo "Missing dependency: $command (check mode makes no changes)" >&2; exit 1; fi
        DEBIAN_FRONTEND=noninteractive apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get -y --no-install-recommends install curl gnupg gpgv python3 ca-certificates
        break
    fi
done
repository=$(python3 - "$repository" <<'PY'
import sys,urllib.parse
value=sys.argv[1].rstrip('/')
url=urllib.parse.urlsplit(value)
if (url.scheme!='https' or not url.hostname or url.username or url.password or url.query or url.fragment
    or any(c.isspace() or c in '"\\' or ord(c)<32 for c in value)):
    sys.exit('Repository URL must be a plain HTTPS URL without credentials, query or fragment')
print(value)
PY
)
work=$(mktemp -d /tmp/pbsx-bootstrap.XXXXXX)
trap 'rm -rf -- "$work"' EXIT
# Only public keys, metadata and logs are placed here; APT's sandbox needs access.
chmod 0755 "$work"
install -d -m 0755 "$work/empty" "$work/lists/partial" "$work/cache/archives/partial"
install -d -m 0700 "$work/gnupg"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
    --connect-timeout 15 --max-time 60 "$repository/archive-key.asc" -o "$work/archive-key.asc"
actual=$(gpg --homedir "$work/gnupg" --batch --with-colons --show-keys "$work/archive-key.asc" |
    awk -F: '$1=="pub" {primary=1;next} $1=="fpr" && primary {print $10;primary=0}')
[[ $actual == "$fingerprint" ]] || { echo 'Repository signing key does not match the pinned PBS-X identity.' >&2; exit 1; }
gpg --homedir "$work/gnupg" --batch --dearmor --output "$work/archive-keyring.gpg" "$work/archive-key.asc"
chmod 0644 "$work/archive-keyring.gpg"
cat > "$work/source.sources" <<EOF
Types: deb
URIs: $repository
Suites: trixie
Components: main
Architectures: amd64
Signed-By: $work/archive-keyring.gpg
EOF
cat > "$work/preferences" <<'EOF'
Package: proxmox-backup-server proxmox-backup-client proxmox-backup-client-static proxmox-backup-file-restore
Pin: version /.*[+]pbsx.*/
Pin-Priority: 990

Package: proxmox-backup-server proxmox-backup-client proxmox-backup-client-static proxmox-backup-file-restore
Pin: version *
Pin-Priority: -1
EOF
options=(-o "Dir::Etc::sourcelist=$work/source.sources" -o "Dir::Etc::sourceparts=$work/empty"
    -o "Dir::Etc::preferences=$work/preferences" -o "Dir::Etc::preferencesparts=$work/empty"
    -o "Dir::State::lists=$work/lists" -o "Dir::Cache=$work/cache"
    -o APT::Update::Error-Mode=any -o Acquire::Languages=none)
apt-get "${options[@]}" update
packages=(proxmox-backup-server proxmox-backup-client proxmox-backup-file-restore)
for name in "${packages[@]}"; do
    candidate=$(apt-cache "${options[@]}" policy "$name" | awk '/Candidate:/ {print $2}')
    [[ $candidate == *+pbsx* ]] || { echo "No compatible PBS-X candidate for $name; the base may be newer than this fork." >&2; exit 1; }
    installed=$(dpkg-query -W -f='${Version}' "$name" 2>/dev/null || true)
    if [[ -n $installed ]] && dpkg --compare-versions "$installed" gt "$candidate"; then
        echo "Refusing to downgrade $name from $installed to $candidate." >&2; exit 1
    fi
done
if $check; then
    echo "Checks passed: supported PBS base, pinned signing key and authenticated APT feed at $repository"
    exit 0
fi
backup=$(mktemp -d /var/backups/pbsx-bootstrap-$(date -u +%Y%m%d-%H%M%S).XXXXXX)
chmod 0700 "$backup"
for path in /usr/share/keyrings/pbsx-archive-keyring.gpg /etc/apt/sources.list.d/pbsx.sources; do
    if [[ -e $path ]]; then cp -a --parents -- "$path" "$backup/"; fi
done
export DEBIAN_FRONTEND=noninteractive
# Bootstrap from the temporary authenticated source. The package installs the
# permanent source and protection hooks together, so there is no interval with
# a persistent PBS-X source but no upgrade protection.
apt-get "${options[@]}" -y -o Dpkg::Options::=--force-confold install pbsx-repository
if [[ $repository != https://pbsx.rikka.ca/apt ]]; then
    python3 - "$repository" > "$work/final.sources" <<'PY'
from pathlib import Path
import sys
for line in Path('/etc/apt/sources.list.d/pbsx.sources').read_text().splitlines():
    print('URIs: '+sys.argv[1] if line.startswith('URIs:') else line)
PY
    install -m 0644 "$work/final.sources" /etc/apt/sources.list.d/pbsx.sources
fi
if ! apt-get -o APT::Update::Error-Mode=any update; then
    echo 'Configure the appropriate official PBS repositories (enterprise with a subscription, or pbs-no-subscription) and rerun.' >&2
    exit 1
fi
flags=()
if $reinstall; then flags+=(--reinstall); fi
apt-get -y -o Dpkg::Options::=--force-confold "${flags[@]}" install "${packages[@]}"
for unit in proxmox-backup.service proxmox-backup-proxy.service; do systemctl is-active --quiet "$unit"; done
curl --insecure --fail --silent --show-error --retry 10 --retry-connrefused --retry-delay 1 \
    --max-time 5 https://127.0.0.1:8007/ >/dev/null
echo 'PBS-X installed. Open https://YOUR-PBS-IP:8007 and use your existing PBS login.'
echo 'Future updates: apt update && apt upgrade'
echo "Previous repository settings: $backup"
