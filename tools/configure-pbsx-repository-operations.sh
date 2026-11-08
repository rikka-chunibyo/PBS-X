#!/bin/bash
set -euo pipefail
[[ $(hostname) == pbs-x-apt && $EUID == 0 ]]
umask 077
state=/var/lib/pbs-x-apt/release
export GNUPGHOME=$state/gnupg
fingerprint=$(cat "$state/signing-fingerprint")
if [[ ! -e $state/archive-signing-recovery.gpg ]]; then
    openssl rand -base64 48 > "$state/archive-recovery.pass"
    gpg --batch --export-secret-keys "$fingerprint" |
        gpg --batch --pinentry-mode loopback --passphrase-file "$state/archive-recovery.pass" \
            --cipher-algo AES256 --symmetric --output "$state/archive-signing-recovery.gpg"
    chmod 0600 "$state/archive-recovery.pass" "$state/archive-signing-recovery.gpg"
fi
cat > /etc/systemd/system/pbsx-apt-refresh.service <<'EOF'
[Unit]
Description=Refresh signed PBS-X repository metadata
After=network.target

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/pbsx-apt-repository refresh
EOF
cat > /etc/systemd/system/pbsx-apt-refresh.timer <<'EOF'
[Unit]
Description=Refresh PBS-X repository metadata daily

[Timer]
OnCalendar=daily
RandomizedDelaySec=5min
Persistent=true

[Install]
WantedBy=timers.target
EOF
chmod 0644 /etc/systemd/system/pbsx-apt-refresh.service /etc/systemd/system/pbsx-apt-refresh.timer
systemctl daemon-reload
systemctl enable --now pbsx-apt-refresh.timer
systemctl start pbsx-apt-refresh.service
systemctl is-enabled pbsx-apt-refresh.timer
systemctl is-active pbsx-apt-refresh.timer
stat -c '%a %n' "$state/archive-recovery.pass" "$state/archive-signing-recovery.gpg"
grep '^Date:' /srv/pbs-x-apt/public/apt/dists/trixie/Release
