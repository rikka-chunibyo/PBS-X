#!/usr/bin/env python3
"""Build a matching offline ISO, then publish stable APT and ISO together."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tempfile
import time
import uuid

import pbsx_iso as iso
import pbsx_iso_publication as publication

ROOT = Path(__file__).resolve().parents[1]


def run(args, **kwargs):
    return subprocess.run([str(x) for x in args], check=True, text=True, capture_output=True, **kwargs).stdout


def github(args):
    return json.loads(run(['gh', *args]))


def release_info(repository, tag):
    view = subprocess.run(['gh', 'release', 'view', tag, '--repo', repository, '--json', 'apiUrl'], text=True, capture_output=True)
    if view.returncode:
        if 'release not found' in view.stderr.lower():
            return None
        raise ValueError('cannot inspect ISO input release: '+view.stderr)
    return github(['api', json.loads(view.stdout)['apiUrl']])


def ensure_assets(repository, tag, files):
    for path in files:
        info = release_info(repository, tag)
        if info is None:
            raise ValueError('ISO release disappeared during asset publication')
        asset = next((a for a in info['assets'] if a['name'] == path.name), None)
        if asset and asset['state'] == 'starter' and asset['size'] == 0 and asset.get('digest') in (None, '', 'sha256:'+hashlib.sha256(b'').hexdigest()):
            # A failed zero-byte upload has no immutable published content.
            fresh = release_info(repository, tag)
            current = next((a for a in fresh['assets'] if a['id'] == asset['id']), None)
            if current != asset:
                raise ValueError('incomplete asset changed during retry')
            run(['gh', 'api', '--method', 'DELETE', 'repos/'+repository+'/releases/assets/'+str(asset['id'])])
            asset = None
        if asset:
            if asset['state'] != 'uploaded' or asset['size'] != path.stat().st_size or asset.get('digest') != 'sha256:'+publication.digest(path):
                raise ValueError('refusing to replace completed GitHub asset: '+path.name)
        else:
            run(['gh', 'release', 'upload', tag, path, '--repo', repository])
        verified = next(a for a in release_info(repository, tag)['assets'] if a['name'] == path.name)
        if verified['state'] != 'uploaded' or verified['size'] != path.stat().st_size or verified.get('digest') != 'sha256:'+publication.digest(path):
            raise ValueError('GitHub upload digest mismatch: '+path.name)


def checked_run(info, repository, commit):
    if (info.get('conclusion') != 'success' or info.get('path') != '.github/workflows/iso.yml'
            or info.get('event') != 'workflow_dispatch' or info.get('head_sha') != commit
            or info.get('head_repository', {}).get('full_name') != repository):
        raise ValueError('expected a successful ISO build for this exact repository/source')
    return info


def build_run(repository, commit, ref, tag, version, existing=None):
    if existing:
        return checked_run(github(['api', 'repos/'+repository+'/actions/runs/'+str(existing)]), repository, commit)
    request_id = uuid.uuid4().hex
    run(['gh', 'workflow', 'run', 'iso.yml', '--repo', repository, '--ref', ref, '--json'],
        input=json.dumps({'release_tag': tag, 'version': version, 'request_id': request_id}))
    deadline = time.monotonic()+75*60
    launch_deadline = time.monotonic()+120
    found = None
    last = None
    while time.monotonic() < deadline:
        if found is None:
            if time.monotonic() > launch_deadline:
                raise ValueError('dispatched ISO workflow did not appear; stable APT was not changed')
            runs = github(['api', 'repos/'+repository+'/actions/workflows/iso.yml/runs?event=workflow_dispatch&per_page=30'])['workflow_runs']
            matches = [r for r in runs if request_id in r['display_title'] and r['head_sha'] == commit]
            if len(matches) > 1:
                raise ValueError('ambiguous ISO workflow correlation')
            if matches:
                found = matches[0]['id']
                print('ISO build:', matches[0]['html_url'], flush=True)
        if found is not None:
            info = github(['api', 'repos/'+repository+'/actions/runs/'+str(found)])
            status = (info['status'], info['conclusion'])
            if status != last:
                print('ISO workflow:', *status, flush=True)
                last = status
            if info['status'] == 'completed':
                return checked_run(info, repository, commit)
        time.sleep(10)
    raise ValueError('ISO build timed out; stable APT was not changed')


def download_iso_assets(repository, tag, commit, destination):
    info = release_info(repository, tag)
    if info is None or info.get('target_commitish') != commit or info.get('draft'):
        raise ValueError('ISO asset release does not match the successful build source')
    destination.mkdir(parents=True, exist_ok=True)
    for name in publication.PUBLIC_FILES[:4]:
        asset = next((a for a in info['assets'] if a['name'] == name), None)
        maximum = 4*1024**3 if name == publication.IMAGE else 1024*1024
        if (not asset or asset.get('state') != 'uploaded' or not 0 < asset.get('size', 0) <= maximum
                or not re.fullmatch(r'sha256:[a-f0-9]{64}', asset.get('digest', ''))):
            raise ValueError('missing or invalid mirrored ISO asset: '+name)
        run(['gh', 'release', 'download', tag, '--repo', repository, '--pattern', name, '--dir', destination])
        path = destination/name
        publication.regular(path)
        if path.stat().st_size != asset['size'] or 'sha256:'+publication.digest(path) != asset['digest']:
            raise ValueError('mirrored ISO download hash mismatch: '+name)


def prepare(args):
    if os.path.lexists(args.output):
        raise ValueError('output already exists; retry with publish --output on a prepared bundle')
    if run(['git', 'status', '--porcelain'], cwd=ROOT).strip():
        raise ValueError('commit the release workflow source before building')
    repository = github(['repo', 'view', '--json', 'nameWithOwner'])['nameWithOwner']
    commit = run(['git', 'rev-parse', 'HEAD'], cwd=ROOT).strip()
    remote = github(['api', 'repos/'+repository+'/commits/'+args.ref])['sha']
    if remote != commit:
        raise ValueError('push this exact workflow source to --ref before building')
    run(['bash', ROOT/'tools/pbsx-apt-repository.sh', '--check-release', args.bundle])
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.pbsx-server-', dir=args.output.parent) as name:
        work = Path(name)
        pool = work/'packages'
        iso.fetch_pbsx(pool)
        iso.overlay_packages(pool, sorted(args.bundle.glob('*.deb')))
        descriptors = [publication.descriptor(path) for path in sorted(pool.glob('*.deb'))]
        selected = publication.package_set(descriptors)
        version = selected[publication.CORE[0]]['version']
        repo_version = selected['pbsx-repository']['version']
        tag = args.tag or re.sub(r'[^A-Za-z0-9.+_-]', '-', 'pbsx-iso-'+version+'-repo-'+repo_version+'-'+commit[:10])
        if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.+_-]{0,159}', tag):
            raise ValueError('invalid ISO release tag')
        stage = work/'stage'
        stage.mkdir()
        for path in pool.glob('*.deb'):
            shutil.copy2(path, stage/path.name)
        inputs = {'repository': repository, 'tag': tag, 'version': version, 'packages': descriptors}
        (stage/'iso-inputs.json').write_text(json.dumps(inputs, indent=2)+'\n')
        info = release_info(repository, tag)
        if info is None:
            notes = work/'input-notes.md'
            notes.write_text('Hash-pinned inputs for the automatic PBS-X '+version+' offline ISO build.\n\nThe installer will be added after successful verification. Stable APT and the public latest ISO are unchanged until the combined publisher completes.\n')
            run(['gh', 'release', 'create', tag, '--repo', repository, '--target', commit, '--prerelease',
                 '--title', 'PBS-X '+version+' ISO build inputs', '--notes-file', notes])
        elif info['target_commitish'] != commit or info['draft']:
            raise ValueError('input release has a different source or is not readable by hosted CI')
        print('Mirroring the exact ISO package inputs...', flush=True)
        ensure_assets(repository, tag, [stage/d['filename'] for d in descriptors]+[stage/'iso-inputs.json'])
        info = build_run(repository, commit, args.ref, tag, version, args.run_id)
        artifact = work/'artifact'
        try:
            run(['gh', 'run', 'download', str(info['id']), '--repo', repository,
                 '--name', 'pbsx-offline-iso-amd64', '--dir', artifact])
        except subprocess.CalledProcessError:
            # The dispatched job mirrors these exact files before its optional
            # artifact upload. Use the verified release transport if the Actions
            # quota prevented that redundant upload; retain all source/hash gates.
            print('Collecting checked ISO from its immutable GitHub release mirror...', flush=True)
            if artifact.exists():
                shutil.rmtree(artifact)
            download_iso_assets(repository, tag, commit, artifact)
        images = list(artifact.rglob(publication.IMAGE))
        if len(images) != 1:
            raise ValueError('ambiguous ISO artifact layout')
        for filename in publication.PUBLIC_FILES[:4]:
            publication.regular(images[0].parent/filename)
            shutil.copy2(images[0].parent/filename, stage/filename)
        manifest = publication.read_json(stage/'pbsx-offline-amd64.manifest.json')
        metadata = {'schema': 1, 'release_id': tag, 'version': version, 'repository_version': repo_version,
                    'builder_commit': commit, 'ci_run_id': info['id'], 'created_at': manifest['created_at'],
                    'image_size': (stage/publication.IMAGE).stat().st_size,
                    'image_sha256': publication.digest(stage/publication.IMAGE)}
        (stage/'iso-publication.json').write_text(json.dumps(metadata, indent=2)+'\n')
        publication.check_bundle(stage)
        with (stage/'SHA256SUMS').open('w') as checksums:
            for path in sorted(stage.iterdir()):
                if path.name != 'SHA256SUMS':
                    checksums.write(publication.digest(path)+'  '+path.name+'\n')
        os.rename(stage, args.output)
    print('Prepared matching APT/ISO release:', args.output, flush=True)


def install_helpers(host, options, staging):
    run(['scp', *options, ROOT/'tools/pbsx_iso_publication.py', ROOT/'tools/pbsx-apt-repository.sh', host+':'+staging+'/'])
    script = '''import hashlib,os,shutil,socket,sys,tempfile
from pathlib import Path
assert os.geteuid()==0 and socket.gethostname()=='pbs-x-apt'
stage=Path(sys.argv[1]);assert stage.resolve().parent==Path('/srv/pbs-x-apt/incoming/server')
backup=Path('/var/lib/pbs-x-apt/release/backups')/('iso-helpers-'+stage.name)
backup.mkdir(mode=0o700,exist_ok=True)
for source,target in [(stage/'pbsx_iso_publication.py',Path('/usr/local/libexec/pbsx-iso-publication.py')),(stage/'pbsx-apt-repository.sh',Path('/usr/local/sbin/pbsx-apt-repository'))]:
    assert source.is_file() and not source.is_symlink() and not target.is_symlink()
    if target.exists() and source.read_bytes()==target.read_bytes():continue
    if target.exists():
        saved=backup/(target.name+'.'+hashlib.sha256(target.read_bytes()).hexdigest()[:16])
        if not saved.exists():shutil.copy2(target,saved);saved.chmod(0o600)
    target.parent.mkdir(parents=True,exist_ok=True)
    fd,name=tempfile.mkstemp(prefix='.pbsx-',dir=target.parent)
    try:
        with os.fdopen(fd,'wb') as out:
            out.write(source.read_bytes());out.flush();os.fchmod(out.fileno(),0o755);os.fsync(out.fileno())
        os.replace(name,target)
    finally:Path(name).unlink(missing_ok=True)
print('Verified publication helpers installed; previous versions retained privately')
'''
    print(run(['ssh', *options, host, shlex.join(['python3', '-', staging])], input=script).strip(), flush=True)


def publish(args):
    if run(['git', 'status', '--porcelain'], cwd=ROOT).strip():
        raise ValueError('commit publication helper changes before deploying them')
    data, selected = publication.check_bundle(args.output)
    inputs = publication.read_json(args.output/'iso-inputs.json')
    repository = github(['repo', 'view', '--json', 'nameWithOwner'])['nameWithOwner']
    if inputs['repository'] != repository or inputs['tag'] != data['release_id']:
        raise ValueError('publication belongs to a different repository/release')
    run(['git', 'diff', '--exit-code', data['builder_commit'], 'HEAD', '--',
         'tools/pbsx_iso_publication.py', 'tools/pbsx-apt-repository.sh', 'tools/publish-pbsx-server.py'], cwd=ROOT)
    checked_run(github(['api', 'repos/'+repository+'/actions/runs/'+str(data['ci_run_id'])]), repository, data['builder_commit'])
    if not re.fullmatch(r'[A-Za-z0-9_.@-]+', args.host) or args.host.startswith('-'):
        raise ValueError('invalid SSH destination')
    files = [args.output/i['filename'] for i in selected.values()]
    files += [args.output/name for name in (*publication.PUBLIC_FILES, 'SHA256SUMS')]
    if {p.name for p in args.output.iterdir()} != {p.name for p in files}:
        raise ValueError('unexpected files in prepared publication directory')
    run(['sha256sum', '--check', '--strict', 'SHA256SUMS'], cwd=args.output)
    ensure_assets(repository, data['release_id'], files)
    options = ['-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', '-o', 'StrictHostKeyChecking=yes']
    if args.identity:
        options += ['-o', 'IdentitiesOnly=yes', '-i', str(args.identity.resolve())]
    if args.known_hosts:
        options += ['-o', 'UserKnownHostsFile='+str(args.known_hosts.resolve())]
    staging = '/srv/pbs-x-apt/incoming/server/'+data['release_id']
    run(['ssh', *options, args.host, shlex.join(['install', '-d', '-m', '0700', staging])])
    print('Staging verified matching APT packages and ISO...', flush=True)
    run(['scp', *options, *files, args.host+':'+staging+'/'])
    install_helpers(args.host, options, staging)
    print(run(['ssh', *options, args.host, shlex.join(['/usr/local/sbin/pbsx-apt-repository', 'publish', 'trixie', staging])]).strip(), flush=True)
    with tempfile.TemporaryDirectory(prefix='pbsx-iso-notes-') as directory:
        notes = Path(directory)/'notes.md'
        notes.write_text('PBS-X '+data['version']+' offline installer (repository support '+data['repository_version']+').\n\nNormal APT and the current ISO contain the same verified PBS-X package set. Install without Internet; the configured signed repository supplies later updates.\n\nDownloads: https://pbsx.rikka.ca/iso/latest/\n\nVerified native image build: https://github.com/'+repository+'/actions/runs/'+str(data['ci_run_id'])+'\n\nSHA256SUMS, ISO manifest and iso-publication.json record integrity and exact provenance. Previous versioned images remain available.\n')
        run(['gh', 'release', 'edit', data['release_id'], '--repo', repository,
             '--title', 'PBS-X '+data['version']+' offline installer', '--notes-file', notes])
    # The immutable public image and verified GitHub asset are the retained
    # copies. Drop only this consumed private transfer cache, not old releases.
    run(['ssh', *options, args.host, shlex.join(['rm', '--', staging+'/'+publication.IMAGE])])
    print('Published stable APT and ISO: https://pbsx.rikka.ca/iso/latest/', flush=True)
    print('GitHub: https://github.com/'+repository+'/releases/tag/'+data['release_id'], flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    for command in ('prepare', 'release', 'publish'):
        sub = commands.add_parser(command)
        sub.add_argument('--output', type=Path, required=True)
        if command != 'publish':
            sub.add_argument('--bundle', type=Path, required=True, help='validated core trio and/or repository support package bundle')
            sub.add_argument('--ref', default='pbs-x')
            sub.add_argument('--tag')
            sub.add_argument('--run-id', type=int, help='reuse a successful ISO run from this exact source')
        if command != 'prepare':
            sub.add_argument('--host', required=True)
            sub.add_argument('--identity', type=Path)
            sub.add_argument('--known-hosts', type=Path)
    args = parser.parse_args()
    args.output = args.output.resolve()
    try:
        if args.command != 'publish':
            args.bundle = args.bundle.resolve()
            prepare(args)
        if args.command != 'prepare':
            publish(args)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        detail = error.stderr if isinstance(error, subprocess.CalledProcessError) else str(error)
        parser.exit(1, 'Server/ISO publication failed: '+str(detail)+'\n')
