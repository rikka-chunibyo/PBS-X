#!/usr/bin/env python3
"""Install a trusted PBS-X Linux preview bundle. Checksums are not signatures."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import stat
import subprocess
import tempfile
import time

NAMES = {'pbsx-agent', 'pbsx-agentctl', 'pbsx-desktop', 'install.py'}
TARGET = Path('/opt/pbsx-agent')


def validate(bundle):
    manifest = bundle/'manifest.json'
    if manifest.is_symlink() or manifest.stat().st_size > 65536:
        raise ValueError('unsafe manifest')
    data = json.loads(manifest.read_text())
    if data.get('schema') != 1 or data.get('os') != 'linux' or data.get('arch') != 'amd64':
        raise ValueError('expected a Linux amd64 bundle')
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?', data.get('version', '')):
        raise ValueError('invalid bundle version')
    files = data.get('files', {})
    if not {'pbsx-agent', 'pbsx-agentctl', 'install.py'} <= files.keys() or files.keys() - NAMES:
        raise ValueError('unexpected bundle file list')
    for name, expected in files.items():
        source = bundle/name
        if source.is_symlink() or not source.is_file() or source.stat().st_size != expected['size']:
            raise ValueError('missing, unsafe or truncated file: '+name)
        digest = hashlib.sha256()
        with source.open('rb') as stream:
            header = stream.read(20)
            digest.update(header)
            for block in iter(lambda: stream.read(1024*1024), b''):
                digest.update(block)
        if digest.hexdigest() != expected['sha256']:
            raise ValueError('checksum mismatch: '+name)
        if name.startswith('pbsx-') and (header[:6] != b'\x7fELF\x02\x01' or header[18:20] != b'\x3e\x00'):
            raise ValueError('not an amd64 Linux executable: '+name)
    return data


def root_directory(path):
    for parent in reversed([path, *path.parents]):
        if not parent.exists():
            parent.mkdir(mode=0o755)
            parent.chmod(0o755)  # Installer umask is intentionally 077.
        info = parent.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError('unsafe installation directory: '+str(parent))


def command(*args, check=True):
    return subprocess.run(args, check=check, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)


def atomic_copy(source, destination, mode=0o755):
    descriptor, name = tempfile.mkstemp(prefix='.pbsx-', dir=destination.parent)
    try:
        with os.fdopen(descriptor, 'wb') as out, source.open('rb') as payload:
            shutil.copyfileobj(payload, out)
            out.flush()
            os.fchmod(out.fileno(), mode)
            os.fsync(out.fileno())
        os.replace(name, destination)
    finally:
        Path(name).unlink(missing_ok=True)


def install(bundle, headless):
    if os.geteuid() != 0:
        raise ValueError('use sudo to install; --check needs no privileges')
    if platform.system() != 'Linux' or platform.machine() not in ('x86_64', 'amd64'):
        raise ValueError('this bundle requires Linux amd64')
    if shutil.which('systemctl') is None:
        raise ValueError('this preview installer requires systemd')
    os.umask(0o077)
    # Stage before verifying so the invoking user cannot change the payload
    # between checksum verification and privileged installation.
    with tempfile.TemporaryDirectory(prefix='pbsx-install-') as name:
        staging = Path(name)
        for item in ['manifest.json', *NAMES]:
            if (bundle/item).exists():
                shutil.copyfile(bundle/item, staging/item)
        data = validate(staging)
        if not headless and 'pbsx-desktop' not in data['files']:
            raise ValueError('headless bundle: pass --headless')
        root_directory(TARGET)
        root_directory(Path('/usr/local/bin'))
        root_directory(Path('/usr/local/share/applications'))
        names = ['pbsx-agent', 'pbsx-agentctl'] + ([] if headless else ['pbsx-desktop'])
        for name in names:
            target = TARGET/name
            if target.is_symlink() or (target.exists() and not target.is_file()):
                raise ValueError('unsafe existing application file: '+str(target))
        for name in names[1:]:
            link = Path('/usr/local/bin')/name
            if os.path.lexists(link) and not (link.is_symlink() and os.readlink(link) == str(TARGET/name)):
                raise ValueError('existing command is not managed by this installer: '+str(link))
        public = Path('/var/lib/pbsx-agent-status/status.json')
        if public.exists():
            status = json.loads(public.read_text())
            if status.get('operation') and -5 <= time.time()-status.get('updated_at', 0) <= 15:
                raise ValueError('backup/restore is active; install when idle')
        installed = command('systemctl', 'cat', 'pbsx-agent.service', check=False).returncode == 0
        running = command('systemctl', 'is-active', '--quiet', 'pbsx-agent.service', check=False).returncode == 0
        if installed:
            unit = command('systemctl', 'show', '--property=ExecStart', '--value', 'pbsx-agent.service').stdout
            if f'path={TARGET}/pbsx-agent ' not in unit:
                raise ValueError('existing service uses another installation path; keep its current installer')
        root_directory(Path('/var/backups'))
        previous = Path(tempfile.mkdtemp(prefix='pbsx-agent-install-', dir='/var/backups'))
        old = []
        for name in names:
            if (TARGET/name).exists():
                shutil.copy2(TARGET/name, previous/name)
                old.append(name)
        replaced = []
        try:
            if installed:
                command('systemctl', 'stop', 'pbsx-agent.service')
            for name in names:
                atomic_copy(staging/name, TARGET/name)
                replaced.append(name)
            agent = str(TARGET/'pbsx-agent')
            if not installed:
                command(agent, 'install')
            if running:
                command(agent, 'start')
        except BaseException:
            # Keep the root-only recovery copy even if restoration itself fails.
            command('systemctl', 'stop', 'pbsx-agent.service', check=False)
            if not installed and 'pbsx-agent' in replaced:
                command(str(TARGET/'pbsx-agent'), 'uninstall', check=False)
            for name in replaced:
                if name in old:
                    atomic_copy(previous/name, TARGET/name)
                else:
                    (TARGET/name).unlink(missing_ok=True)
            if running:
                command('systemctl', 'start', 'pbsx-agent.service')
            print('Previous executables retained in', previous)
            raise
        for name in names[1:]:
            link = Path('/usr/local/bin')/name
            if not os.path.lexists(link):
                link.symlink_to(TARGET/name)
        if not headless:
            entry = staging/'pbsx-desktop.desktop'
            entry.write_text(
                '[Desktop Entry]\nType=Application\nName=PBS-X Agent\n'
                'Comment=Manage the PBS-X backup agent\nExec=/opt/pbsx-agent/pbsx-desktop\n'
                'Icon=folder\nTerminal=false\nCategories=System;Utility;\n')
            atomic_copy(entry, Path('/usr/local/share/applications/pbsx-desktop.desktop'), 0o644)
        if old:
            print('Previous executables retained in', previous)
        else:
            previous.rmdir()
        print('Installed PBS-X', data['version'])
        print('Open PBS-X Agent or use: sudo pbsx-agentctl enroll --stdin')
        print('Enrollment and storage credentials were preserved. Automatic updates remain opt-in.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='verify only; do not install')
    parser.add_argument('--headless', action='store_true', help='install the service and CLI only')
    args = parser.parse_args()
    try:
        bundle = Path(__file__).resolve().parent
        if args.check:
            print('Verified Linux bundle', validate(bundle)['version'])
        else:
            install(bundle, args.headless)
    except Exception as error:
        parser.exit(1, f'PBS-X install failed: {error}\n')
