#!/usr/bin/env python3
"""Validate and atomically activate an ISO paired with a stable APT publication."""
import argparse
import datetime
import gzip
import hashlib
import html
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import tempfile
import uuid

CORE = ('proxmox-backup-server', 'proxmox-backup-client', 'proxmox-backup-file-restore')
PACKAGES = (*CORE, 'pbsx-repository')
IMAGE = 'pbsx-offline-amd64.iso'
PUBLIC_FILES = (IMAGE, IMAGE+'.sha256', 'pbsx-offline-amd64.manifest.json',
                'pbsx-offline-amd64.boot.txt', 'iso-inputs.json', 'iso-publication.json')
LATEST_URL = '/iso/latest/'+IMAGE


def run(args):
    return subprocess.run([str(x) for x in args], check=True, text=True, capture_output=True).stdout


def digest(path):
    value = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            value.update(block)
    return value.hexdigest()


def regular(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError('expected regular file: '+str(path))


def read_json(path):
    regular(path)
    if path.stat().st_size > 256 << 10:
        raise ValueError('oversized publication metadata')
    value = json.loads(path.read_text())
    if not isinstance(value, dict):
        raise ValueError('publication metadata must be an object')
    return value


def records(text):
    result = []
    for block in re.split(r'\n\s*\n', text.strip()):
        item = {}
        for line in block.splitlines():
            if line and not line[0].isspace():
                key, value = line.split(':', 1)
                item[key] = value.strip()
        if item:
            result.append(item)
    return result


def descriptor(path):
    regular(path)
    fields = records(run(['dpkg-deb', '-f', path]))[0]
    name = fields['Package']
    if name not in PACKAGES or fields['Architecture'] != ('all' if name == 'pbsx-repository' else 'amd64'):
        raise ValueError('unexpected ISO input package or architecture')
    return {'name': name, 'version': fields['Version'], 'architecture': fields['Architecture'],
            'filename': path.name, 'sha256': digest(path), 'size': path.stat().st_size}


def package_set(entries):
    if not isinstance(entries, list):
        raise ValueError('ISO inputs must be a package list')
    result = {}
    for item in entries:
        if not isinstance(item, dict):
            raise ValueError('invalid ISO package entry')
        name = item.get('name')
        if name not in PACKAGES or name in result:
            raise ValueError('ISO inputs require exactly four distinct release packages')
        if not re.fullmatch(r'[0-9][A-Za-z0-9.+:~_-]*', item.get('version', '')):
            raise ValueError('invalid package version')
        if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.+_-]*\.deb', item.get('filename', '')):
            raise ValueError('unsafe package filename')
        if not re.fullmatch(r'[a-f0-9]{64}', item.get('sha256', '')) or type(item.get('size')) is not int or not 0 < item['size'] <= 1 << 30:
            raise ValueError('invalid package digest/size')
        architecture = 'all' if name == 'pbsx-repository' else 'amd64'
        if item.get('architecture', architecture) != architecture:
            raise ValueError('wrong package architecture')
        result[name] = {**item, 'architecture': architecture}
    if set(result) != set(PACKAGES):
        raise ValueError('ISO inputs require exactly four distinct release packages')
    versions = {result[name]['version'] for name in CORE}
    if len(versions) != 1 or '+pbsx' not in next(iter(versions)):
        raise ValueError('ISO core package versions must match')
    if subprocess.run(['dpkg', '--compare-versions', result['pbsx-repository']['version'], 'ge', '1.0.3']).returncode:
        raise ValueError('ISO requires repository support >= 1.0.3')
    return result


def timestamp(value):
    if not isinstance(value, str):
        raise ValueError('invalid ISO creation time')
    parsed = datetime.datetime.fromisoformat(value)
    if parsed.tzinfo is None:
        raise ValueError('ISO creation time must include a timezone')
    return parsed


def inspect_image(path):
    with tempfile.TemporaryDirectory(prefix='pbsx-iso-inspect-') as name:
        target = Path(name)/'manifest.json'
        run(['xorriso', '-abort_on', 'FAILURE', '-osirrox', 'on', '-indev', path,
             '-extract', '/PBS-X-ISO.json', target])
        embedded = read_json(target)
    boot = run(['xorriso', '-indev', path, '-report_el_torito', 'plain'])
    if not all(re.search(r'El Torito boot img\s*:\s*'+number+r'\s+'+kind, boot)
               for number, kind in (('1', 'BIOS'), ('2', 'UEFI'))):
        raise ValueError('ISO lost BIOS/UEFI boot entries')
    return embedded


def check_metadata(data, expected):
    if data.get('schema') != 1 or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.+_-]{0,159}', data.get('release_id', '')):
        raise ValueError('invalid ISO publication identity')
    if not re.fullmatch(r'[a-f0-9]{40}', data.get('builder_commit', '')) or type(data.get('ci_run_id')) is not int or data['ci_run_id'] <= 0:
        raise ValueError('invalid checked build provenance')
    version = expected[CORE[0]]['version']
    if data.get('version') != version or data.get('repository_version') != expected['pbsx-repository']['version']:
        raise ValueError('ISO version differs from staged APT packages')
    if timestamp(data['created_at']) > datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(minutes=5):
        raise ValueError('ISO creation time is in the future')


def check_bundle(bundle, inspector=inspect_image):
    inputs = read_json(bundle/'iso-inputs.json')
    expected = package_set(inputs['packages'])
    actual = package_set([descriptor(p) for p in sorted(bundle.glob('*.deb'))])
    if actual != expected:
        raise ValueError('ISO inputs differ from the staged APT packages')
    data = read_json(bundle/'iso-publication.json')
    check_metadata(data, expected)
    if inputs.get('tag') != data['release_id'] or not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', inputs.get('repository', '')):
        raise ValueError('ISO input release identity mismatch')
    version = expected[CORE[0]]['version']
    if inputs.get('version') != version:
        raise ValueError('ISO input version differs from staged APT packages')
    for name in PUBLIC_FILES:
        regular(bundle/name)
    image = bundle/IMAGE
    if type(data.get('image_size')) is not int or not 0 < data['image_size'] <= 8 << 30 or image.stat().st_size != data['image_size'] or digest(image) != data.get('image_sha256'):
        raise ValueError('ISO image digest/size mismatch')
    if (bundle/(IMAGE+'.sha256')).read_text() != data['image_sha256']+'  '+IMAGE+'\n':
        raise ValueError('ISO checksum filename/digest mismatch')
    manifest = read_json(bundle/'pbsx-offline-amd64.manifest.json')
    if manifest.get('version') != version or manifest.get('builder_commit') != data['builder_commit'] or manifest.get('builder_dirty') is not False or manifest.get('network_required_to_install') is not False or set(manifest.get('firmware', [])) != {'bios', 'uefi'}:
        raise ValueError('ISO manifest version/build/offline properties mismatch')
    if manifest.get('created_at') != data['created_at']:
        raise ValueError('ISO publication creation time mismatch')
    selected = package_set([p for p in manifest['packages'] if p['name'] in PACKAGES])
    # Older build manifests omit architecture; package_set supplies its fixed value.
    if selected != expected:
        raise ValueError('ISO manifest packages differ from staged APT packages')
    if inspector(image) != manifest:
        raise ValueError('embedded ISO manifest differs from published sidecar')
    return data, expected


def atomic_text(path, content, mode=0o644):
    fd, filename = tempfile.mkstemp(prefix='.pbsx-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as stream:
            stream.write(content)
            stream.flush()
            os.fchmod(stream.fileno(), mode)
            os.fsync(stream.fileno())
        os.replace(filename, path)
        sync_directory(path.parent)
    finally:
        Path(filename).unlink(missing_ok=True)


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def check_pending(path, bundle):
    if os.path.lexists(path):
        regular(path)
        if path.read_text() != str(bundle.resolve())+'\n':
            raise ValueError('finish the pending APT/ISO publication before starting another')


def public_dirs(public):
    for path in (public, public/'iso', public/'iso/releases'):
        if path.is_symlink():
            raise ValueError('unsafe ISO publication directory')
        path.mkdir(exist_ok=True, mode=0o755)
        path.chmod(0o755)
    return public/'iso'


def current_release(directory):
    latest = directory/'latest'
    if not os.path.lexists(latest):
        return None
    if not latest.is_symlink():
        raise ValueError('latest ISO is not a managed link')
    target = latest.readlink()
    if len(target.parts) != 2 or target.parts[0] != 'releases' or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.+_-]{0,159}', target.parts[1]) or (directory/target).is_symlink():
        raise ValueError('latest ISO points outside a managed release')
    return read_json(directory/target/'iso-publication.json')


def navigation_pages(public):
    pages = [public/'index.html']
    for parent in (public/'downloads', public/'downloads/releases'):
        if parent.is_symlink():
            raise ValueError('unsafe app navigation directory')
    apps = public/'downloads/latest'
    if os.path.lexists(apps):
        if not apps.is_symlink():
            raise ValueError('unsafe app navigation link')
        target = apps.readlink()
        if len(target.parts) != 2 or target.parts[0] != 'releases' or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}', target.parts[1]) or (apps.parent/target).is_symlink():
            raise ValueError('unsafe app navigation link')
        pages.append(apps/'index.html')
    for page in pages:
        if os.path.lexists(page):
            regular(page)
    return pages


def prepare(bundle, public, inspector=inspect_image):
    data, expected = check_bundle(bundle, inspector)
    navigation_pages(public)
    # reprepro does not replace an existing version with rebuilt bytes. Reject
    # that conflict (and downgrades) before recording a pending transaction or
    # changing APT, including the first ISO added to an existing repository.
    check_package_upgrade(expected, stable_packages(public, complete=False))
    directory = public_dirs(public)
    current = current_release(directory)
    if current:
        previous_inputs = read_json(directory/'latest/iso-inputs.json')
        previous = package_set(previous_inputs['packages'])
        check_package_upgrade(expected, previous)
        if expected == previous and timestamp(data['created_at']) < timestamp(current['created_at']):
            raise ValueError('refusing an older ISO build for the same packages')
    destination = directory/'releases'/data['release_id']
    if os.path.lexists(destination):
        if destination.is_symlink() or read_json(destination/'iso-publication.json') != data:
            raise ValueError('refusing to replace an immutable ISO release')
        for name in PUBLIC_FILES:
            regular(destination/name)
            if digest(destination/name) != digest(bundle/name):
                raise ValueError('existing ISO release bytes differ')
    else:
        with tempfile.TemporaryDirectory(prefix='.stage-', dir=directory/'releases') as name:
            work = Path(name)
            for filename in PUBLIC_FILES:
                shutil.copyfile(bundle/filename, work/filename)
                (work/filename).chmod(0o644)
                with (work/filename).open('rb') as stream:
                    os.fsync(stream.fileno())
            if digest(work/IMAGE) != data['image_sha256']:
                raise ValueError('ISO changed during public staging')
            (work/'index.html').write_text('<!doctype html><meta charset="utf-8"><title>PBS-X offline ISO</title><h1>PBS-X '+html.escape(data['version'])+'</h1><p><a href="'+IMAGE+'">Download offline ISO</a> · <a href="'+IMAGE+'.sha256">SHA256</a> · <a href="pbsx-offline-amd64.manifest.json">Build details</a></p>\n')
            (work/'index.html').chmod(0o644)
            work.chmod(0o755)
            sync_directory(work)
            os.rename(work, destination)
            sync_directory(destination.parent)
    return data


def check_package_upgrade(expected, previous):
    for name in expected.keys() & previous.keys():
        new, old = expected[name], previous[name]
        for relation in ('lt', 'eq'):
            result = subprocess.run(['dpkg', '--compare-versions', new['version'], relation, old['version']], capture_output=True)
            if result.returncode not in (0, 1):
                raise ValueError('cannot compare published package versions: '+name)
            if result.returncode == 0:
                if relation == 'lt':
                    raise ValueError('refusing an older ISO package version: '+name)
                if new != old:
                    raise ValueError('same package version has different bytes or identity; increase its version: '+name)
                break


def stable_packages(public, complete=True):
    index = public/'apt/dists/trixie/main/binary-amd64/Packages.gz'
    if not complete and not os.path.lexists(index):
        return {}
    regular(index)
    selected = {}
    for item in records(gzip.decompress(index.read_bytes()).decode()):
        if item.get('Package') in PACKAGES:
            name = item['Package']
            if name in selected:
                raise ValueError('multiple stable versions of an ISO input package')
            selected[name] = {'name': name, 'version': item['Version'], 'architecture': item['Architecture'],
                              'filename': Path(item['Filename']).name, 'size': int(item['Size']), 'sha256': item['SHA256']}
    return package_set(list(selected.values())) if complete else selected


def activate(bundle, public):
    data = read_json(bundle/'iso-publication.json')
    expected = package_set(read_json(bundle/'iso-inputs.json')['packages'])
    check_metadata(data, expected)
    if stable_packages(public) != expected:
        raise ValueError('stable APT changed; refusing to activate an ISO with different packages')
    directory = public_dirs(public)
    destination = directory/'releases'/data['release_id']
    if destination.is_symlink() or read_json(destination/'iso-publication.json') != data:
        raise ValueError('ISO must be completely prepared before APT activation')
    pages = navigation_pages(public)
    # prepare and activate are serialized under the APT publisher's file lock.
    current = current_release(directory)
    if current and current['release_id'] != data['release_id'] and timestamp(current['created_at']) > timestamp(data['created_at']):
        old = package_set(read_json(directory/'latest/iso-inputs.json')['packages'])
        if old == expected:
            raise ValueError('refusing to roll back a newer ISO build')
    link = directory/('.latest-'+uuid.uuid4().hex)
    try:
        link.symlink_to('releases/'+data['release_id'])
        os.replace(link, directory/'latest')
        sync_directory(directory)
    finally:
        link.unlink(missing_ok=True)
    atomic_text(directory/'index.html', '<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=latest/"><title>PBS-X offline ISO</title><a href="latest/">Current PBS-X offline ISO</a>\n')
    # Only generated navigation HTML changes; previous ISO/app payloads stay immutable.
    for page in pages:
        if page.exists():
            regular(page)
            old = page.read_text()
            new = re.sub(r'href="/iso/[^"?#]+\.iso"', 'href="'+LATEST_URL+'"', old)
            if new != old:
                atomic_text(page, new)
    return data


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['check', 'prepare', 'activate'])
    parser.add_argument('--bundle', type=Path, required=True)
    parser.add_argument('--public', type=Path, default=Path('/srv/pbs-x-apt/public'))
    args = parser.parse_args()
    try:
        if args.command == 'check':
            check_bundle(args.bundle)
        else:
            allowed = Path('/srv/pbs-x-apt/incoming/server')
            if os.geteuid() != 0 or socket.gethostname() != 'pbs-x-apt' or args.bundle.is_symlink() or args.bundle.resolve().parent != allowed or args.public != Path('/srv/pbs-x-apt/public'):
                raise ValueError('ISO activation requires a private repository-host server staging directory')
            pending = Path('/var/lib/pbs-x-apt/release/iso-publication-pending')
            if args.command == 'prepare':
                check_pending(pending, args.bundle)
                result = prepare(args.bundle, args.public)
                atomic_text(pending, str(args.bundle.resolve())+'\n', 0o600)
            else:
                result = activate(args.bundle, args.public)
                pending.unlink(missing_ok=True)
                sync_directory(pending.parent)
            print('ISO '+args.command+': '+result['version']+'; '+result['release_id'])
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        parser.exit(1, 'ISO publication failed: '+str(error)+'\n')
