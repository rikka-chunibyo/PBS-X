#!/bin/bash
# Admin commands for the PBS-X repository container. TLS terminates at NPM.
set -euo pipefail

validate_release() {
    local directory file name version architecture core_version= core_count=0
    local -A seen=()
    directory=$(realpath "$1")
    (cd "$directory"; sha256sum --check --strict SHA256SUMS)
    packages=()
    shopt -s nullglob
    for file in "$directory/"*.deb; do
        name=$(dpkg-deb -f "$file" Package)
        version=$(dpkg-deb -f "$file" Version)
        architecture=$(dpkg-deb -f "$file" Architecture)
        [[ ! ${seen[$name]+present} ]] || { echo "Duplicate release package: $name" >&2; return 1; }
        seen[$name]=1
        case $name in
            proxmox-backup-server|proxmox-backup-client|proxmox-backup-file-restore)
                [[ $version == *+pbsx* && $architecture == amd64 ]] || { echo "Invalid PBS-X executable package: $name" >&2; return 1; }
                if [[ -z $core_version ]]; then core_version=$version; fi
                [[ $version == "$core_version" ]] || { echo 'PBS-X core package versions must match' >&2; return 1; }
                core_count=$((core_count+1)) ;;
            pbsx-repository)
                [[ $architecture == all ]] || { echo 'Repository support package must use Architecture: all' >&2; return 1; } ;;
            *) echo "Not an allowed PBS-X release package: $name" >&2; return 1 ;;
        esac
        (cd "$directory"; grep -Fxq -- "$(sha256sum -- "$(basename "$file")")" SHA256SUMS) || {
            echo "Release package is not covered by SHA256SUMS: $file" >&2; return 1;
        }
        packages+=("$file")
    done
    ((${#packages[@]})) || { echo 'Release contains no packages' >&2; return 1; }
    [[ $core_count == 0 || $core_count == 3 ]] || {
        echo 'Publish all three matching PBS-X core packages together' >&2; return 1;
    }
}

# Read-only validation is also usable on the build machine, without repository
# signing access or privileges. Publication repeats it before touching reprepro.
if [[ ${1:-} == --check-release ]]; then
    [[ $# == 2 ]] || exit 2
    validate_release "$2"
    echo 'Verified PBS-X release package set'
    exit 0
fi
[[ $EUID == 0 && $(hostname) == pbs-x-apt ]] || exit 1
state=/var/lib/pbs-x-apt/release
public=/srv/pbs-x-apt/public/apt
export GNUPGHOME=$state/gnupg
iso_publisher=/usr/local/libexec/pbsx-iso-publication.py
# Serialize package exports, metadata refresh and ISO pointer updates. The
# expensive hosted build happens before this short repository transaction.
if [[ ${1:-} == publish || ${1:-} == refresh ]]; then
    exec 9>"$state/publication.lock"
    flock -x 9
fi
repo() {
    # A root shell or backup wrapper may use umask 077. Only archive payloads
    # and indexes belong here; signer/admin state stays under its 0700 parent.
    (umask 022; reprepro --keepunreferencedfiles --basedir "$state/repository" --outdir "$public" "$@")
    find "$public" -type d -exec chmod 0755 {} +
    find "$public" -type f -exec chmod 0644 {} +
}
publish_release() {
    local channel=$1 directory=$2
    validate_release "$directory"
    if [[ $channel == trixie ]]; then
        [[ -f $iso_publisher && -f $directory/iso-publication.json ]] || {
            echo 'Stable APT updates require a matching ISO. Use tools/publish-pbsx-server.py release.' >&2
            exit 1
        }
        # Verify and durably stage the complete image before changing APT.
        python3 "$iso_publisher" prepare --bundle "$directory"
    fi
    repo --export=never includedeb "$channel" "${packages[@]}"
    repo export "$channel"
    repo check "$channel"
    if [[ $channel == trixie ]]; then
        python3 "$iso_publisher" activate --bundle "$directory"
    fi
}
case ${1:-} in
    init)
        [[ ! -e $state ]] || { echo 'Release state already exists; refusing to replace signing keys'; exit 1; }
        install -d -m 0700 "$state" "$GNUPGHOME" "$state/repository/conf"
        install -d -m 0755 "$public"
        gpg --batch --pinentry-mode loopback --passphrase '' --quick-generate-key \
            'PBS-X archive signing <apt@pbsx.rikka.ca>' ed25519 cert 2y
        fingerprint=$(gpg --batch --with-colons --list-keys | awk -F: '$1=="fpr" {print $10; exit}')
        gpg --batch --pinentry-mode loopback --passphrase '' --quick-add-key "$fingerprint" ed25519 sign 1y
        printf '%s\n' "$fingerprint" > "$state/signing-fingerprint"
        gpg --batch --armor --export-options export-minimal --export "$fingerprint" > "$public/archive-key.asc"
        chmod 0644 "$public/archive-key.asc"
        for channel in trixie trixie-testing; do
            cat >> "$state/repository/conf/distributions" <<EOF
Origin: PBS-X
Label: PBS-X
Suite: $channel
Codename: $channel
Architectures: amd64
Components: main
Description: PBS-X packages for Debian 13
SignWith: $fingerprint
ValidFor: 14d

EOF
        done
        # The user terminates HTTPS at Nginx Proxy Manager. Remove our old lab TLS listener.
        rm -f /etc/nginx/sites-enabled/pbs-x-apt-lab-tls
        cat > /etc/nginx/sites-available/pbs-x-apt <<'EOF'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    root /srv/pbs-x-apt/public;
    index index.html;
    autoindex off;
    add_header Cache-Control "no-cache, no-transform" always;
    location /iso/ {
        # Large uncached images must stream, with origin byte-range support.
        add_header Cache-Control "no-store, no-transform" always;
        add_header X-Accel-Buffering "no" always;
        send_timeout 3600s;
        try_files $uri =404;
    }
    location / { try_files $uri $uri/ =404; }
    location ~ /\. { deny all; }
}
EOF
        nginx -t
        systemctl restart nginx
        echo "Archive primary fingerprint: $fingerprint"
        ;;
    publish)
        [[ $# == 3 && ( $2 == trixie || $2 == trixie-testing ) ]]
        publish_release "$2" "$3"
        ;;
    refresh)
        if [[ -f $state/iso-publication-pending && ! -L $state/iso-publication-pending ]]; then
            # A crash may have interrupted includedeb before its complete set
            # was exported. Finish the validated transaction before renewing
            # indexes, rather than exposing a partial package family.
            publish_release trixie "$(cat "$state/iso-publication-pending")"
        fi
        repo --export=force export
        repo check
        ;;
    *) echo 'Usage: script init | publish CHANNEL DIRECTORY | refresh' >&2; exit 2 ;;
esac
