"""Offline ISO input validation and package metadata helpers (no mounts required)."""
import datetime
import functools
import getpass
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
SPEC = json.loads((ROOT/'packaging/iso/base.json').read_text())
CORE = ('proxmox-backup-server', 'proxmox-backup-client', 'proxmox-backup-file-restore')


def run(args, **kwargs):
    return subprocess.run([str(x) for x in args], check=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, **kwargs)


def sha256(path):
    digest = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            digest.update(block)
    return digest.hexdigest()


def records(text):
    result = []
    for block in re.split(r'\n\s*\n', text.strip()):
        fields = {}
        key = None
        for line in block.splitlines():
            if line.startswith((' ', '\t')) and key:
                fields[key] += '\n'+line
            elif ':' in line:
                key, value = line.split(':', 1)
                fields[key] = value.strip()
            elif line:
                raise ValueError('malformed Debian control record')
        if fields:
            result.append(fields)
    return result


def serialize(fields):
    return '\n'.join(key+': '+value for key, value in fields.items())+'\n\n'


def package(path):
    fields = records(run(['dpkg-deb', '-f', path]).stdout)[0]
    if not re.fullmatch(r'[a-z0-9][a-z0-9+.-]+', fields['Package']):
        raise ValueError('unsafe package name')
    if fields['Architecture'] not in ('amd64', 'all'):
        raise ValueError('ISO requires amd64/all packages')
    if not re.fullmatch(r'[0-9][A-Za-z0-9.+:~_-]*', fields['Version']):
        raise ValueError('unsafe package version')
    return fields


def inventory(directory):
    result = {}
    for path in sorted(Path(directory).glob('*.deb')):
        fields = package(path)
        name = fields['Package']
        if name in result:
            raise ValueError('multiple package versions in installer pool: '+name)
        result[name] = (path, fields)
    return result


def overlay_packages(directory, additions):
    before = inventory(directory)
    for source in additions:
        fields = package(source)
        name = fields['Package']
        if name in before:
            before[name][0].unlink()
        filename = f"{name}_{fields['Version'].replace(':', '_')}_{fields['Architecture']}.deb"
        destination = Path(directory)/filename
        shutil.copy2(source, destination)
        before[name] = (destination, fields)
    return before


def download(url, path, limit=2 << 30):
    if not url.startswith('https://'):
        raise ValueError('ISO inputs require HTTPS')
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    partial = path.with_name(path.name+'.part')
    subprocess.run(['curl', '--fail', '--silent', '--show-error', '--location', '--retry', '2',
                    '--proto', '=https', '--proto-redir', '=https', '--connect-timeout', '20',
                    '--max-time', '1800', '--max-filesize', str(limit), '--output', str(partial), url], check=True)
    partial.replace(path)


def verify_key(path, fingerprint, work):
    info = run(['gpg', '--homedir', work, '--batch', '--show-keys', '--with-colons', path]).stdout
    primary = next(line.split(':')[9] for line in info.splitlines() if line.startswith('fpr:'))
    if primary != fingerprint:
        raise ValueError('unexpected repository signing identity')


def newest(values):
    def compare(a, b):
        if subprocess.run(['dpkg', '--compare-versions', a, 'lt', b]).returncode == 0:
            return -1
        return 0 if a == b else 1
    return sorted(values, key=functools.cmp_to_key(compare))[-1]


def fetch_pbsx(destination, version=None):
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='pbsx-iso-trust-') as name:
        work = Path(name)
        key = ROOT/'packaging/pbsx-repository/archive-key.asc'
        verify_key(key, SPEC['repository_fingerprint'], work)
        run(['gpg', '--homedir', work, '--batch', '--dearmor', '--output', work/'keyring.gpg', key])
        base = SPEC['repository']
        download(base+'/dists/trixie/InRelease', work/'InRelease', 1 << 20)
        run(['gpgv', '--keyring', work/'keyring.gpg', '--output', work/'Release', work/'InRelease'])
        release = records((work/'Release').read_text())[0]
        if 'Valid-Until' not in release:
            raise ValueError('repository metadata has no expiration')
        from email.utils import parsedate_to_datetime
        now = datetime.datetime.now(datetime.timezone.utc)
        if parsedate_to_datetime(release['Valid-Until']) <= now:
            raise ValueError('repository metadata expired')
        if parsedate_to_datetime(release['Date']) > now+datetime.timedelta(minutes=5):
            raise ValueError('repository metadata is dated in the future')
        download(base+'/dists/trixie/main/binary-amd64/Packages.gz', work/'Packages.gz', 16 << 20)
        index = (work/'Packages.gz').read_bytes()
        expected = [line.split() for line in release['SHA256'].splitlines()
                    if line.split() and line.split()[-1] == 'main/binary-amd64/Packages.gz']
        if expected != [[hashlib.sha256(index).hexdigest(), str(len(index)), 'main/binary-amd64/Packages.gz']]:
            raise ValueError('package index does not match signed metadata')
        available = records(gzip.decompress(index).decode())
        if version is None:
            version = newest([x['Version'] for x in available if x['Package'] == CORE[0] and '+pbsx' in x['Version']])
        selected = []
        for name in (*CORE, 'pbsx-repository'):
            matches = [x for x in available if x['Package'] == name and (name == 'pbsx-repository' or x['Version'] == version)]
            if not matches:
                raise ValueError('published ISO package is missing: '+name)
            latest = newest([x['Version'] for x in matches])
            item = next(x for x in matches if x['Version'] == latest)
            relative = PurePosixPath(item['Filename'])
            if not item['Filename'].startswith('pool/') or '..' in relative.parts or '\\' in str(relative) or relative.is_absolute():
                raise ValueError('unsafe package download path')
            target = destination/relative.name
            download(base+'/'+str(relative), target, int(item['Size']))
            if target.stat().st_size != int(item['Size']) or sha256(target) != item['SHA256']:
                raise ValueError('package does not match signed repository index: '+name)
            actual = package(target)
            if (actual['Package'], actual['Version']) != (name, item['Version']):
                raise ValueError('downloaded package identity mismatch')
            selected.append(target)
        shutil.copy2(work/'InRelease', destination/'repository.InRelease')
        shutil.copy2(work/'Release', destination/'repository.Release')
        shutil.copy2(work/'Packages.gz', destination/'repository.Packages.gz')
        return selected


def final_status(base_status, packages, firmware='bios', secure_boot=False):
    status = {item['Package']: item for item in records(base_status)}
    for name, (_path, fields) in packages.items():
        if name == 'grub-pc' and firmware != 'bios':
            continue
        if name in ('grub-efi-amd64', 'grub-efi-arm64') and firmware != 'uefi':
            continue
        if name.startswith('proxmox-grub') and firmware != 'uefi':
            continue
        if name == 'proxmox-secure-boot-support' and not secure_boot:
            continue
        if name == 'proxmox-first-boot':
            continue
        status[name] = {**fields, 'Status': 'install ok installed'}
    return ''.join(serialize(item) for item in status.values())


def apt_config(work, status, sources='', preferences=''):
    work = Path(work)
    for name in ('empty', 'lists/partial', 'cache/archives/partial'):
        (work/name).mkdir(parents=True, exist_ok=True)
    (work/'status').write_text(status)
    (work/'sources.list').write_text(sources)
    (work/'preferences').write_text(preferences)
    config = {
        'Dir::Etc::main': str(work/'empty/no-main'), 'Dir::Etc::parts': str(work/'empty'),
        'Dir::Etc::sourcelist': str(work/'sources.list'), 'Dir::Etc::sourceparts': str(work/'empty'),
        'Dir::Etc::preferences': str(work/'preferences'), 'Dir::Etc::preferencesparts': str(work/'empty'),
        'Dir::State::status': str(work/'status'), 'Dir::State::lists': str(work/'lists'),
        'Dir::Cache': str(work/'cache'), 'APT::Architecture': 'amd64',
        'APT::Sandbox::User': getpass.getuser(), 'Acquire::Languages': 'none',
        'APT::Install-Recommends': 'false', 'APT::Install-Suggests': 'false',
    }
    path = work/'apt.conf'
    path.write_text('\n'.join(key+' '+json.dumps(value)+';' for key, value in config.items())+'\n')
    return {**os.environ, 'APT_CONFIG': str(path), 'LC_ALL': 'C'}


def check_closure(base_status, packages):
    for firmware, secure in (('bios', False), ('uefi', False), ('uefi', True)):
        with tempfile.TemporaryDirectory(prefix='pbsx-iso-check-') as name:
            env = apt_config(Path(name), final_status(base_status, packages, firmware, secure))
            result = subprocess.run(['apt-get', '-s', 'check'], env=env, text=True, capture_output=True)
            if result.returncode:
                raise ValueError(f'offline dependency closure failed ({firmware}, secure={secure}):\n'+result.stdout+result.stderr)


def resolve_dependencies(base_status, packages, destination, extra=()):
    """Download only missing runtime packages into an isolated Debian APT state."""
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='pbsx-iso-resolve-') as name:
        work = Path(name)
        proxmox_key = work/'proxmox.gpg'
        download(SPEC['proxmox_key_url'], proxmox_key, 1 << 20)
        verify_key(proxmox_key, SPEC['proxmox_key_fingerprint'], work)
        debian_key = Path('/usr/share/keyrings/debian-archive-keyring.gpg')
        if not debian_key.is_file():
            raise ValueError('install debian-archive-keyring on the build machine')
        sources = (
            f'deb [signed-by={debian_key}] http://deb.debian.org/debian trixie main contrib non-free-firmware\n'
            f'deb [signed-by={debian_key}] http://deb.debian.org/debian trixie-updates main contrib non-free-firmware\n'
            f'deb [signed-by={debian_key}] http://security.debian.org/debian-security trixie-security main contrib non-free-firmware\n'
            f'deb [signed-by={proxmox_key}] http://download.proxmox.com/debian/pbs trixie pbs-no-subscription\n'
        )
        env = apt_config(work, final_status(base_status, packages), sources, (ROOT/'etc/pbsx.pref').read_text())
        # The resolver never runs dpkg, system APT hooks, or changes the host's
        # sources/status. Signed Debian/Proxmox metadata authenticates downloads.
        update = run(['apt-get', 'update'], env=env)
        (destination/'apt-update.log').write_text(update.stdout+update.stderr)
        result = run(['apt-get', '--download-only', '--no-remove', '--assume-yes', '--fix-broken', 'install', *extra], env=env)
        (destination/'apt-download.log').write_text(result.stdout+result.stderr)
        downloaded = []
        for path in (work/'cache/archives').glob('*.deb'):
            target = destination/path.name
            shutil.copy2(path, target)
            downloaded.append(target)
    return sorted(downloaded)


def build_defaults_package(destination, manifest):
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='pbsx-iso-defaults-') as name:
        root = Path(name)/'root'
        for path in ('DEBIAN', 'usr/lib/pbsx-offline-installer', 'usr/share/doc/pbsx-offline-installer'):
            (root/path).mkdir(parents=True, mode=0o755)
        root.chmod(0o755)
        (root/'DEBIAN/control').write_text('''Package: pbsx-offline-installer
Version: 1.0.0
Section: admin
Priority: optional
Architecture: all
Maintainer: PBS-X developers <apt@pbsx.rikka.ca>
Depends: pbsx-repository (>= 1.0.2), proxmox-backup-server, python3
Description: PBS-X offline installation defaults and image provenance
 Configures fresh installer targets for signed PBS-X and community system
 updates. Does not reset administrator repository choices on later upgrades.
''')
        postinst = root/'DEBIAN/postinst'
        postinst.write_text('''#!/bin/sh
set -e
if [ "$1" = configure ]; then
    /usr/lib/pbsx-offline-installer/configure-target.py
fi
''')
        postinst.chmod(0o755)
        script = root/'usr/lib/pbsx-offline-installer/configure-target.py'
        shutil.copyfile(ROOT/'packaging/iso/configure-target.py', script)
        script.chmod(0o755)
        (root/'usr/share/doc/pbsx-offline-installer/image.json').write_text(json.dumps(manifest, indent=2)+'\n')
        target = destination/'pbsx-offline-installer_1.0.0_all.deb'
        run(['dpkg-deb', '--root-owner-group', '--threads-max=1', '--build', root, target])
        return target
