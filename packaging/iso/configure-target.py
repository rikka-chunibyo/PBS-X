#!/usr/bin/python3
"""Set fresh-ISO system repository defaults once; never reset later user choices."""
import argparse
import os
from pathlib import Path
import re
import subprocess

SYSTEM_SOURCE = '''Types: deb
URIs: http://download.proxmox.com/debian/pbs
Suites: trixie
Components: pbs-no-subscription
Signed-By: /usr/share/keyrings/proxmox-archive-keyring.gpg
'''


def configure(root):
    root = root.resolve()
    marker = root/'var/lib/pbsx-offline-installer/repository-configured'
    if marker.exists():
        return
    # This package configures a new installer target, not arbitrary existing PBS
    # installations. The upstream installer removes this flag after configuring
    # the package set. Normal upgrades/reinstalls therefore do not reset choices.
    if not (root/'proxmox_install_mode').is_file():
        return
    directory = root/'etc/apt/sources.list.d'
    if directory.is_symlink():
        raise ValueError('repository directory must not be a symlink')
    directory.mkdir(parents=True, exist_ok=True)
    enabled_community = False
    for path in sorted(directory.iterdir()):
        if path.suffix not in ('.sources', '.list'):
            continue
        if path.is_symlink() or not path.is_file():
            raise ValueError('unsafe repository source file')
        original = path.read_text()
        if path.suffix == '.sources':
            blocks = re.split(r'\n\s*\n', original.strip())
            result = []
            for block in blocks:
                active = not re.search(r'^Enabled:\s*no\s*$', block, re.I | re.M)
                if 'pbs-no-subscription' in block and active:
                    enabled_community = True
                if re.search(r'^URIs:.*enterprise\.proxmox\.com/debian/pbs(?:\s|$)', block, re.I | re.M):
                    if re.search(r'^Enabled:', block, re.I | re.M):
                        block = re.sub(r'^Enabled:.*$', 'Enabled: no', block, flags=re.I | re.M)
                    else:
                        block += '\nEnabled: no'
                result.append(block)
            updated = '\n\n'.join(result)+'\n'
        else:
            result = []
            for line in original.splitlines():
                if line.lstrip().startswith('deb ') and 'pbs-no-subscription' in line:
                    enabled_community = True
                if line.lstrip().startswith(('deb ', 'deb-src ')) and 'enterprise.proxmox.com/debian/pbs' in line:
                    line = '# Disabled by PBS-X offline installer: '+line
                result.append(line)
            updated = '\n'.join(result)+'\n'
        if updated != original:
            path.write_text(updated)
    if not enabled_community:
        source = directory/'pbsx-system.sources'
        if source.exists() and source.read_text() != SYSTEM_SOURCE:
            raise ValueError('custom PBS-X system source already exists')
        source.write_text(SYSTEM_SOURCE)
        source.chmod(0o644)
    marker.parent.mkdir(parents=True, exist_ok=True)
    marker.write_text('Defaults applied during offline installation; later changes are administrator-owned.\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path('/'), help='alternate target root for isolated checks')
    args = parser.parse_args()
    if args.root.resolve() == Path('/'):
        if os.geteuid() != 0:
            parser.error('requires root')
        result = subprocess.run(['dpkg-query', '-W', '-f=${db:Status-Status}', 'pve-manager'], capture_output=True, text=True)
        if result.returncode == 0 and result.stdout == 'installed':
            parser.error('PBS-X requires a dedicated PBS system, not a PVE host')
    configure(args.root)
