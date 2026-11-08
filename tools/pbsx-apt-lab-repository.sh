#!/bin/bash
# Disposable LAN repository on LXC116 only. Never use its keys for production.
set -euo pipefail
[[ $EUID == 0 && $(hostname) == pbs-x-apt ]] || exit 1
state=/var/lib/pbs-x-apt/lab
public=/srv/pbs-x-apt/public/lab
export GNUPGHOME=$state/gnupg
repo() { reprepro --basedir "$state/repository" --outdir "$public" "$@"; }
case ${1:-} in
    init)
        [[ ! -e $state ]] || { echo 'Lab already initialized; refusing to replace keys'; exit 1; }
        install -d -m 0700 "$state" "$GNUPGHOME" "$state/repository/conf" "$state/tls"
        install -d -m 0755 "$public"
        gpg --batch --pinentry-mode loopback --passphrase '' --quick-generate-key \
            'PBS-X LAB ONLY <apt-lab@pbs-x.invalid>' ed25519 sign 30d
        fingerprint=$(gpg --batch --with-colons --list-keys | awk -F: '$1=="fpr" {print $10; exit}')
        printf '%s\n' "$fingerprint" > "$state/signing-fingerprint"
        gpg --batch --armor --export-options export-minimal --export "$fingerprint" > "$public/archive-key.asc"
        chmod 0644 "$public/archive-key.asc"
        for codename in trixie trixie-baseline; do
            cat >> "$state/repository/conf/distributions" <<EOF
Origin: PBS-X-Lab
Label: PBS-X LAB ONLY
Suite: pbsx-lab-$codename
Codename: $codename
Architectures: amd64
Components: main
Description: Disposable PBS-X APT tests; not a production release feed
SignWith: $fingerprint
ValidFor: 7d

EOF
        done
        openssl req -x509 -newkey rsa:3072 -noenc -sha256 -days 30 \
            -subj '/CN=PBS-X LAB ONLY CA' \
            -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign' \
            -keyout "$state/tls/ca.key" -out "$state/tls/ca.crt" 2> "$state/tls/generation.log"
        openssl req -new -newkey rsa:3072 -noenc -subj '/CN=pbs-x-apt' \
            -keyout "$state/tls/server.key" -out "$state/tls/server.csr" 2>> "$state/tls/generation.log"
        cat > "$state/tls/extensions" <<'EOF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=IP:10.0.1.37,DNS:pbs-x-apt
EOF
        openssl x509 -req -in "$state/tls/server.csr" -CA "$state/tls/ca.crt" \
            -CAkey "$state/tls/ca.key" -CAcreateserial -days 30 -sha256 \
            -extfile "$state/tls/extensions" -out "$state/tls/server.crt"
        chmod 0600 "$state/tls/"*.key
        cat > /etc/nginx/sites-available/pbs-x-apt-lab-tls <<EOF
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name pbs-x-apt 10.0.1.37;
    ssl_certificate $state/tls/server.crt;
    ssl_certificate_key $state/tls/server.key;
    ssl_protocols TLSv1.2 TLSv1.3;
    root /srv/pbs-x-apt/public;
    autoindex off;
    location / { try_files \$uri \$uri/ =404; }
    location ~ /\. { deny all; }
}
EOF
        ln -s /etc/nginx/sites-available/pbs-x-apt-lab-tls /etc/nginx/sites-enabled/
        nginx -t
        systemctl reload nginx
        echo "Lab signing fingerprint: $fingerprint"
        ;;
    publish)
        [[ $# == 3 && ( $2 == trixie || $2 == trixie-baseline ) ]]
        channel=$2
        bundle=$(realpath "$3")
        (cd "$bundle"; sha256sum --check --strict SHA256SUMS)
        packages=()
        version=
        for name in proxmox-backup-server proxmox-backup-client proxmox-backup-file-restore; do
            files=("$bundle/${name}_"*.deb)
            [[ ${#files[@]} == 1 ]]
            file=${files[0]}
            (cd "$bundle"; grep -Fxq -- "$(sha256sum -- "$(basename "$file")")" SHA256SUMS)
            [[ $(dpkg-deb -f "$file" Package) == "$name" && $(dpkg-deb -f "$file" Architecture) == amd64 ]]
            current=$(dpkg-deb -f "$file" Version)
            [[ $current == 4.2.9-1+pbsx1 || $current == 4.2.9-1+pbsx2 ]]
            if [[ -z $version ]]; then version=$current; else [[ $current == "$version" ]]; fi
            packages+=("$file")
        done
        repo --export=never includedeb "$channel" "${packages[@]}"
        repo export "$channel"
        repo check "$channel"
        echo "Published $version to LAB $channel"
        ;;
    negatives)
        python3 - <<'PY'
import pathlib, shutil, subprocess
public = pathlib.Path('/srv/pbs-x-apt/public')
negative = public / 'lab-negative'
assert not negative.exists(), 'negative fixture already exists'
for kind in ('tampered', 'unsigned', 'corrupt-package', 'expired'):
    shutil.copytree(public/'lab/dists', negative/kind/'dists')
    root = negative/kind/'dists/trixie'
    if kind == 'tampered':
        path = root/'InRelease'
        text = path.read_text()
        assert 'Disposable PBS-X' in text
        path.write_text(text.replace('Disposable PBS-X', 'Altered PBS-X', 1))
    elif kind == 'unsigned':
        for name in ('InRelease', 'Release.gpg'): (root/name).unlink()
    elif kind == 'expired':
        path = root/'Release'
        lines = path.read_text().splitlines()
        lines = ['Date: Fri, 01 Jan 2021 00:00:00 UTC' if x.startswith('Date:') else
                 'Valid-Until: Sat, 02 Jan 2021 00:00:00 UTC' if x.startswith('Valid-Until:') else x for x in lines]
        path.write_text('\n'.join(lines)+'\n')
        subprocess.run(['gpg','--batch','--yes','--clearsign','-o',str(root/'InRelease'),str(path)],check=True)
        subprocess.run(['gpg','--batch','--yes','--armor','--detach-sign','-o',str(root/'Release.gpg'),str(path)],check=True)
    else:
        package = next((public/'lab/pool').rglob('proxmox-backup-client_*.deb'))
        target = negative/kind/package.relative_to(public/'lab')
        target.parent.mkdir(parents=True)
        shutil.copy2(package,target)
        with target.open('r+b') as f:
            f.seek(100); old=f.read(1); f.seek(100); f.write(bytes([old[0]^1]))
print('Created isolated metadata, expiry and package corruption fixtures')
PY
        ;;
    reset)
        # Validate/retain the baseline before withdrawing the version-only fixture.
        "$0" publish trixie-baseline /srv/pbs-x-apt/incoming/baseline
        repo --export=never remove trixie proxmox-backup-server proxmox-backup-client proxmox-backup-file-restore
        "$0" publish trixie /srv/pbs-x-apt/incoming/baseline
        ;;
    cleanup-negatives)
        rm -rf -- /srv/pbs-x-apt/public/lab-negative
        ;;
    *) echo 'Usage: script init | publish CHANNEL BUNDLE | negatives | reset | cleanup-negatives' >&2; exit 2 ;;
esac
