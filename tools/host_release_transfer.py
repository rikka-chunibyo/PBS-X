"""Checked native CI release transport, independent of Actions artifact storage."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile
import zipfile

PREFIX = 'pbsx-client-build-'
LIMIT = 128 * 1024 * 1024


def run(args):
    return subprocess.check_output([str(a) for a in args], text=True)


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as source:
        for block in iter(lambda: source.read(1024*1024), b''):
            h.update(block)
    return h.hexdigest()


def validate_tag(tag):
    if not re.fullmatch(r'pbsx-client-build-[A-Za-z0-9._-]{1,100}', tag):
        raise ValueError('expected a dedicated native build release tag')


def names(system):
    if system not in {'linux', 'windows'}:
        raise ValueError('unsupported native bundle platform')
    suffix = '.exe' if system == 'windows' else ''
    return {n+suffix for n in ('pbsx-agent', 'pbsx-agentctl', 'pbsx-desktop')} | {'install.ps1' if system == 'windows' else 'install.py', 'manifest.json'}


def asset_names(system):
    names(system)
    base = 'host-app-'+system+'-preview'
    return base+'.zip', base+'.provenance.json'


def release(repository, tag):
    validate_tag(tag)
    return json.loads(run(['gh', 'api', 'repos/'+repository+'/releases/tags/'+tag]))


def checked_release(info, commit, writing=False):
    if info.get('target_commitish') != commit or (writing and info.get('draft') is not True):
        raise ValueError('native build release has different source or is no longer a draft')


def upload(repository, tag, path, commit):
    info = release(repository, tag)
    checked_release(info, commit, writing=True)
    existing = next((a for a in info['assets'] if a['name'] == path.name), None)
    expected = 'sha256:'+digest(path)
    if existing:
        if existing.get('state') != 'uploaded' or existing.get('digest') != expected or existing.get('size') != path.stat().st_size:
            raise ValueError('refusing to replace a conflicting native build asset')
    else:
        subprocess.run(['gh', 'release', 'upload', tag, str(path), '--repo', repository], check=True)
    current = next(a for a in release(repository, tag)['assets'] if a['name'] == path.name)
    if current.get('state') != 'uploaded' or current.get('digest') != expected or current.get('size') != path.stat().st_size:
        raise ValueError('native build upload hash mismatch')


def mirror(bundle, system, tag):
    env = os.environ
    if env.get('GITHUB_EVENT_NAME') != 'workflow_dispatch':
        raise ValueError('native release transport requires an explicit workflow dispatch')
    validate_tag(tag)
    repository, commit = env['GITHUB_REPOSITORY'], env['GITHUB_SHA']
    if not re.fullmatch(r'[a-f0-9]{40}', commit):
        raise ValueError('invalid source commit')
    checked_release(release(repository, tag), commit, writing=True)
    bundle = Path(bundle)
    manifest = json.loads((bundle/'manifest.json').read_text())
    wanted = names(system)
    if (manifest.get('schema') != 1 or manifest.get('source_commit') != commit
            or manifest.get('dirty') not in (None, False) or manifest.get('os') != system
            or manifest.get('arch') != 'amd64' or set(manifest.get('files', {})) != wanted-{'manifest.json'}):
        raise ValueError('native bundle manifest does not identify this build')
    for name in wanted:
        path = bundle/name
        if path.is_symlink() or not path.is_file():
            raise ValueError('native payload is not a regular file')
        if name != 'manifest.json':
            expected = manifest['files'][name]
            if expected['size'] != path.stat().st_size or expected['sha256'] != digest(path):
                raise ValueError('native payload differs from its manifest')
    with tempfile.TemporaryDirectory(prefix='pbsx-native-release-') as work:
        archive, provenance = (Path(work)/name for name in asset_names(system))
        with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=6) as output:
            for name in sorted(wanted):
                info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
                info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = (stat.S_IFREG | 0o644) << 16
                output.writestr(info, (bundle/name).read_bytes())
        provenance.write_text(json.dumps({'schema': 1, 'repository': repository,
            'commit': commit, 'run_id': int(env['GITHUB_RUN_ID']),
            'run_attempt': int(env['GITHUB_RUN_ATTEMPT']), 'system': system,
            'version': manifest['version'], 'sha256': digest(archive), 'size': archive.stat().st_size}, sort_keys=True)+'\n')
        upload(repository, tag, archive, commit)
        upload(repository, tag, provenance, commit)
    print('Mirrored verified native build:', system, tag)


def checked_extract(archive, destination, system):
    destination = Path(destination)
    if destination.exists():
        raise ValueError('native bundle extraction destination already exists')
    wanted = names(system)
    with zipfile.ZipFile(archive) as source:
        entries = source.infolist()
        if len(entries) != len(wanted) or {i.filename for i in entries} != wanted:
            raise ValueError('unexpected native release archive entries')
        if sum(i.file_size for i in entries) > LIMIT:
            raise ValueError('native release archive exceeds limit')
        if any(stat.S_ISLNK(i.external_attr >> 16) for i in entries):
            raise ValueError('native release archive contains a symlink')
        destination.mkdir(parents=True)
        for info in entries:
            with source.open(info) as input, (destination/info.filename).open('xb') as output:
                while block := input.read(65536):
                    output.write(block)


def collect(repository, tag, run_info, system, destination):
    if run_info.get('event') != 'workflow_dispatch':
        raise ValueError('release assets require a dispatched native build')
    info = release(repository, tag)
    checked_release(info, run_info['head_sha'])
    archive_name, proof_name = asset_names(system)
    selected = {}
    for name, maximum in [(archive_name, LIMIT), (proof_name, 16384)]:
        asset = next((a for a in info['assets'] if a['name'] == name), None)
        if not asset or asset.get('state') != 'uploaded' or not 0 < asset.get('size', 0) <= maximum or not re.fullmatch(r'sha256:[a-f0-9]{64}', asset.get('digest', '')):
            raise ValueError('missing or invalid native release asset')
        selected[name] = asset
    with tempfile.TemporaryDirectory(prefix='pbsx-native-collect-') as directory:
        root = Path(directory)
        for name, asset in selected.items():
            subprocess.run(['gh', 'release', 'download', tag, '--repo', repository, '--pattern', name, '--dir', str(root)], check=True)
            path = root/name
            if path.stat().st_size != asset['size'] or 'sha256:'+digest(path) != asset['digest']:
                raise ValueError('downloaded native release asset hash mismatch')
        proof = json.loads((root/proof_name).read_text())
        expected = {'schema': 1, 'repository': repository, 'commit': run_info['head_sha'],
            'run_id': run_info['id'], 'run_attempt': run_info['run_attempt'], 'system': system,
            'sha256': digest(root/archive_name), 'size': (root/archive_name).stat().st_size}
        if any(proof.get(k) != v for k, v in expected.items()):
            raise ValueError('native release provenance does not match this successful CI run')
        checked_extract(root/archive_name, destination, system)
        manifest = json.loads((Path(destination)/'manifest.json').read_text())
        if manifest.get('version') != proof.get('version'):
            raise ValueError('native release version differs from CI provenance')
