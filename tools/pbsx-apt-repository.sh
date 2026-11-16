#!/bin/bash
# Admin commands for the PBS-X repository container. TLS terminates at NPM.
set -euo pipefail
[[ $EUID == 0 && $(hostname) == pbs-x-apt ]] || exit 1
state=/var/lib/pbs-x-apt/release
public=/srv/pbs-x-apt/public/apt
export GNUPGHOME=$state/gnupg
repo() {
    # A root shell or backup wrapper may use umask 077. Only archive payloads
    # and indexes belong here; signer/admin state stays under its 0700 parent.
    (umask 022; reprepro --keepunreferencedfiles --basedir "$state/repository" --outdir "$public" "$@")
    find "$public" -type d -exec chmod 0755 {} +
    find "$public" -type f -exec chmod 0644 {} +
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
        channel=$2
        directory=$(realpath "$3")
        (cd "$directory"; sha256sum --check --strict SHA256SUMS)
        packages=()
        shopt -s nullglob
        for file in "$directory/"*.deb; do
            name=$(dpkg-deb -f "$file" Package)
            version=$(dpkg-deb -f "$file" Version)
            case $name in
                proxmox-backup-server|proxmox-backup-client|proxmox-backup-file-restore)
                    [[ $version == *+pbsx* && $(dpkg-deb -f "$file" Architecture) == amd64 ]] ;;
                pbsx-repository) [[ $(dpkg-deb -f "$file" Architecture) == all ]] ;;
                *) echo "Not an allowed PBS-X release package: $name" >&2; exit 1 ;;
            esac
            (cd "$directory"; grep -Fxq -- "$(sha256sum -- "$(basename "$file")")" SHA256SUMS)
            packages+=("$file")
        done
        ((${#packages[@]}))
        repo --export=never includedeb "$channel" "${packages[@]}"
        repo export "$channel"
        repo check "$channel"
        ;;
    refresh)
        repo --export=force export
        repo check
        ;;
    *) echo 'Usage: script init | publish CHANNEL DIRECTORY | refresh' >&2; exit 2 ;;
esac
