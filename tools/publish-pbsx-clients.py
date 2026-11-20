#!/usr/bin/env python3
"""Package checked client builds and publish immutable GitHub/HTTPS downloads."""
import argparse
import datetime
import fcntl
import hashlib
import html
import io
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import socket
import stat
import subprocess
import tarfile
import tempfile
import zipfile
import uuid

ROOT = Path(__file__).resolve().parents[1]
ASSETS = {'pbsx-linux-amd64.tar.gz', 'pbsx-windows-amd64.zip', 'pbsx-android.apk'}
PUBLIC_FILES = ASSETS | {'release.json', 'SHA256SUMS'}
SEMVER = r'[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?'


def run(args, **kwargs):
    return subprocess.run([str(x) for x in args], check=True, text=True, capture_output=True, **kwargs).stdout


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1024*1024), b''):
            h.update(block)
    return h.hexdigest()


def regular(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError('expected regular file: '+str(path))


def bundle(path, system):
    regular(path/'manifest.json')
    if (path/'manifest.json').stat().st_size > 65536:
        raise ValueError('oversized bundle manifest')
    data = json.loads((path/'manifest.json').read_text())
    suffix = '.exe' if system == 'windows' else ''
    names = {name+suffix for name in ('pbsx-agent', 'pbsx-agentctl', 'pbsx-desktop')}
    names.add('install.ps1' if system == 'windows' else 'install.py')
    if data.get('schema') != 1 or data.get('os') != system or data.get('arch') != 'amd64' or set(data.get('files', {})) != names:
        raise ValueError('expected complete '+system+' desktop/CLI/agent bundle')
    if not re.fullmatch(SEMVER, data.get('version', '')) or not re.fullmatch(r'[a-f0-9]{40}', data.get('source_commit', '')) or data.get('dirty') not in (False, None):
        raise ValueError('invalid or dirty bundle provenance')
    for name, expected in data['files'].items():
        file = path/name
        regular(file)
        if file.stat().st_size != expected['size'] or digest(file) != expected['sha256']:
            raise ValueError('bundle checksum mismatch: '+name)
        if not name.startswith('pbsx-'):
            continue
        with file.open('rb') as stream:
            header = stream.read(64)
            if system == 'linux':
                valid = header[:6] == b'\x7fELF\x02\x01' and header[18:20] == b'\x3e\x00'
            else:
                valid = header[:2] == b'MZ' and len(header) == 64
                if valid:
                    stream.seek(int.from_bytes(header[60:64], 'little'))
                    valid = stream.read(6) == b'PE\x00\x00\x64\x86'
            if not valid:
                raise ValueError('wrong executable platform: '+name)
    return data


def archive_bundle(source, data, target):
    system = data['os']
    folder = 'pbsx-'+system+'-amd64'
    if system == 'linux':
        instructions = 'Extract the archive. Run python3 install.py --check, then sudo python3 install.py.\nFor a headless system use sudo python3 install.py --headless.\nThe static pbsx-agentctl CLI works without a desktop.\n'
    else:
        instructions = 'Extract the ZIP. Open a 64-bit Administrator PowerShell in this folder.\nRun powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\\install.ps1\nThe desktop is unsigned in this preview; automatic updates are not enabled.\n'
    readme = ('PBS-X '+data['version']+' preview\n\n'+instructions+
              '\nEnroll using a code from the PBS-X server. Existing managed credentials are retained on upgrades.\nDesktop job management remains in the server web UI.\n').encode()
    files = {name:(source/name).read_bytes() for name in (*sorted(data['files']), 'manifest.json')}
    files['README.txt'] = readme
    if system == 'linux':
        with tarfile.open(target, 'w:gz') as archive:
            for name, content in files.items():
                member = tarfile.TarInfo(folder+'/'+name)
                member.size = len(content)
                member.mode = 0o755 if name.startswith('pbsx-') else 0o644
                archive.addfile(member, io.BytesIO(content))
    else:
        with zipfile.ZipFile(target, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
            for name, content in files.items():
                member = zipfile.ZipInfo(folder+'/'+name)
                member.external_attr = (stat.S_IFREG | 0o644) << 16
                member.compress_type = zipfile.ZIP_DEFLATED
                archive.writestr(member, content)


def validate_publication(path):
    regular(path/'release.json')
    if (path/'release.json').stat().st_size > 65536:
        raise ValueError('oversized release metadata')
    data = json.loads((path/'release.json').read_text())
    if data.get('schema') != 1 or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}', data.get('tag', '')):
        raise ValueError('invalid release tag/schema')
    if set(data.get('files', {})) != ASSETS:
        raise ValueError('unexpected public assets')
    for key in ('host_version', 'android_version'):
        if not re.fullmatch(SEMVER, data.get(key, '')):
            raise ValueError('invalid client version')
    release_time(data)
    for name, expected in data['files'].items():
        file = path/name
        regular(file)
        if not isinstance(expected.get('size'), int) or not 0 < expected['size'] <= 1024**3 or not re.fullmatch(r'[a-f0-9]{64}', expected.get('sha256', '')):
            raise ValueError('invalid asset length/hash')
        if file.stat().st_size != expected['size'] or digest(file) != expected['sha256']:
            raise ValueError('release checksum mismatch: '+name)
    regular(path/'SHA256SUMS')
    expected = ''.join(digest(path/name)+'  '+name+'\n' for name in sorted(ASSETS | {'release.json'}))
    if (path/'SHA256SUMS').read_text() != expected:
        raise ValueError('release checksum index mismatch')
    return data


def release_time(data):
    try:
        timestamp = datetime.datetime.fromisoformat(data['created_at'])
        if timestamp.tzinfo is None or timestamp.utcoffset() is None:
            raise ValueError('release timestamp must include a timezone')
        return timestamp
    except (KeyError, TypeError, ValueError):
        raise ValueError('invalid release creation timestamp') from None


def validate_android_build(info, apk):
    # Kept local to the prepare path: the remote activator is standalone.
    from android_source import source_snapshot
    current = source_snapshot(ROOT)
    if info.get('schema') != 2 or info.get('dirty') is not False or current['dirty'] or info.get('source_inputs') != current['source_inputs']:
        raise ValueError('Android build is dirty or stale; run pbsx-android/build.sh')
    required = {'go-tests', 'android-unit-tests', 'lint', 'apk-signature', '16k-alignment', 'non-debuggable'}
    certificate = (ROOT/'packaging/client-downloads/android-preview-certificate.sha256').read_text().strip()
    if info.get('variant') != 'release' or info.get('debuggable') is not False or set(info.get('checks', [])) != required or info.get('certificate_sha256') != certificate or info.get('sha256') != digest(apk) or info.get('size') != apk.stat().st_size:
        raise ValueError('Android build verification does not match the APK')


def prepare(args, linux, windows, run_info):
    if args.output.exists():
        raise ValueError('output already exists; releases are immutable')
    if run(['git', 'status', '--porcelain'], cwd=ROOT).strip():
        raise ValueError('commit the publishing source before preparing a release')
    for system, data in (('linux', linux), ('windows', windows)):
        if data['source_commit'] != run_info['head_sha']:
            raise ValueError(system+' bundle does not match the checked CI run')
    if linux['version'] != windows['version']:
        raise ValueError('Windows and Linux versions differ')
    run(['git', 'diff', '--exit-code', run_info['head_sha'], 'HEAD', '--', 'pbsx-host-agent', 'packaging/host-app', 'tools/build-host-app.py'], cwd=ROOT)
    info = json.loads((ROOT/'pbsx-android/artifacts/build-info.json').read_text())
    apk = ROOT/'pbsx-android/artifacts/pbsx-android-preview.apk'
    validate_android_build(info, apk)
    certificate = info['certificate_sha256']
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.pbsx-clients-', dir=args.output.parent) as name:
        work = Path(name)
        archive_bundle(args.linux_bundle, linux, work/'pbsx-linux-amd64.tar.gz')
        archive_bundle(args.windows_bundle, windows, work/'pbsx-windows-amd64.zip')
        shutil.copyfile(apk, work/'pbsx-android.apk')
        metadata = {
            'schema':1, 'tag':args.tag, 'created_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'repository':run_info['repository']['full_name'],
            'publisher_commit':run(['git', 'rev-parse', 'HEAD'], cwd=ROOT).strip(),
            'host_version':linux['version'], 'host_source_commit':linux['source_commit'],
            'android_version':info['version'], 'android_version_code':info['version_code'],
            'android_source_commit':info['source_commit'], 'android_certificate_sha256':certificate,
            'android_source_inputs':info['source_inputs'],
            'ci_run_id':run_info['id'], 'ci_run_url':run_info['html_url'],
            'files':{name:{'sha256':digest(work/name),'size':(work/name).stat().st_size} for name in sorted(ASSETS)},
        }
        (work/'release.json').write_text(json.dumps(metadata, indent=2)+'\n')
        (work/'SHA256SUMS').write_text(''.join(digest(work/name)+'  '+name+'\n' for name in sorted(ASSETS | {'release.json'})))
        validate_publication(work)
        os.rename(work, args.output)
    return validate_publication(args.output)


def page(data):
    tag = html.escape(data['tag'], quote=True)
    prefix = '/downloads/releases/'+tag+'/'
    rows = [('Windows desktop + CLI', 'pbsx-windows-amd64.zip', data['host_version'], 'Windows amd64'),
            ('Linux desktop + CLI', 'pbsx-linux-amd64.tar.gz', data['host_version'], 'Linux amd64, systemd'),
            ('Android APK', 'pbsx-android.apk', data['android_version'], 'Android 8+, ARM64 and x86-64')]
    body = ''.join('<tr><td>'+html.escape(label)+'</td><td>'+html.escape(version)+'</td><td>'+html.escape(platform)+'</td><td><a href="'+prefix+name+'">Download</a></td></tr>' for label,name,version,platform in rows)
    return '''<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>PBS-X app downloads</title>
<style>body{font:16px system-ui,sans-serif;background:#15181e;color:#e7eaf0;max-width:960px;margin:3rem auto;padding:0 1rem;line-height:1.6}a{color:#8dc5ff}table{border-collapse:collapse;width:100%;margin:2rem 0}td,th{text-align:left;padding:.8rem;border-bottom:1px solid #424854}code{background:#242a35;padding:.15rem .35rem}small{color:#b7bdc9}</style>
<h1>PBS-X app downloads</h1><p>Preview builds for backing up your devices to PBS-X.</p><table><tr><th>App</th><th>Version</th><th>Platform</th><th>File</th></tr>'''+body+'''</table>
<p>Windows: extract the ZIP and run the included installer from an Administrator PowerShell. Linux: extract, then run <code>sudo python3 install.py</code>; add <code>--headless</code> for CLI-only installation. Android: open the APK and allow installation from your browser or file manager.</p>
<p>The desktop companion enrolls and controls the background agent. Configure desktop backup jobs in the PBS-X server web UI. Android manages jobs in the app. Existing managed credentials are retained during updates.</p>
<p>These are preview builds. Windows executables are unsigned; Android retains its existing development signing certificate. Automatic client updates are not enabled.</p>
<p><a href="'''+prefix+'''SHA256SUMS">Checksums</a> · <a href="'''+prefix+'''release.json">Build details</a> · <a href="/iso/latest/pbsx-offline-amd64.iso">Server ISO</a> · <a href="/">APT repository</a></p><small>Release: '''+tag+'''</small></html>\n'''


def atomic_text(path, text, mode=0o644):
    descriptor, name = tempfile.mkstemp(prefix='.pbsx-', dir=path.parent)
    try:
        with os.fdopen(descriptor, 'w') as stream:
            stream.write(text)
            stream.flush()
            os.fchmod(stream.fileno(), mode)
            os.fsync(stream.fileno())
        os.replace(name, path)
        sync_directory(path.parent)
    finally:
        Path(name).unlink(missing_ok=True)


def sync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def activate(staging, public, private, allow_rollback=False):
    data = validate_publication(staging)
    private.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (private/'client-publish.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        downloads = public/'downloads'
        releases = downloads/'releases'
        if downloads.is_symlink() or releases.is_symlink():
            raise ValueError('unsafe download directory')
        releases.mkdir(parents=True, exist_ok=True, mode=0o755)
        downloads.chmod(0o755)
        releases.chmod(0o755)
        latest = downloads/'latest'
        promote = True
        if os.path.lexists(latest):
            if not latest.is_symlink():
                raise ValueError('latest is not a managed symlink')
            target = latest.readlink()
            if len(target.parts) != 2 or target.parts[0] != 'releases' or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}', target.parts[1]) or (downloads/target).is_symlink():
                raise ValueError('latest points outside a managed release')
            current = validate_publication(latest)
            promote = current['tag'] != data['tag'] and (allow_rollback or release_time(data) > release_time(current))
        destination = releases/data['tag']
        if destination.exists() or destination.is_symlink():
            if destination.is_symlink() or (destination/'release.json').read_bytes() != (staging/'release.json').read_bytes():
                raise ValueError('refusing to replace an immutable release')
            validate_publication(destination)
        else:
            with tempfile.TemporaryDirectory(prefix='.stage-', dir=releases) as name:
                work = Path(name)
                for filename in PUBLIC_FILES:
                    shutil.copyfile(staging/filename, work/filename)
                    (work/filename).chmod(0o644)
                validate_publication(work)
                (work/'index.html').write_text(page(data))
                (work/'index.html').chmod(0o644)
                work.chmod(0o755)
                # A successful activation must not expose copied bytes that
                # still exist only in the page cache when power is lost.
                for filename in PUBLIC_FILES | {'index.html'}:
                    with (work/filename).open('rb') as stream:
                        os.fsync(stream.fileno())
                sync_directory(work)
                os.rename(work, destination)
        # Repeat these barriers on retries, including a previous failure after
        # the immutable directory rename but before the latest pointer switch.
        sync_directory(releases)
        sync_directory(downloads)
        sync_directory(public)
        if promote:
            link = downloads/('.latest-'+uuid.uuid4().hex)
            try:
                link.symlink_to('releases/'+data['tag'])
                os.replace(link, latest)
            finally:
                link.unlink(missing_ok=True)
        sync_directory(downloads)
        atomic_text(downloads/'index.html', '<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=latest/"><title>PBS-X apps</title><a href="latest/">PBS-X app downloads</a>\n')
        index = public/'index.html'
        if index.exists():
            regular(index)
            old = index.read_text()
            if 'href="/downloads/"' not in old:
                backups = private/'client-publish-backups'
                backups.mkdir(mode=0o700, exist_ok=True)
                backup = backups/(data['tag']+'.index.html')
                if not backup.exists():
                    backup.write_text(old)
                    backup.chmod(0o600)
                atomic_text(index, old+'\n<p><a href="/downloads/">Windows, Linux and Android apps</a> · <a href="/iso/latest/pbsx-offline-amd64.iso">PBS-X offline ISO</a></p>\n')
    return data


def release_info(repository, tag):
    # Unpublished draft tags do not resolve through GET /releases/tags/TAG.
    # gh handles draft discovery; use its numeric release ID for asset queries.
    view = subprocess.run(['gh', 'release', 'view', tag, '--repo', repository, '--json', 'apiUrl'], text=True, capture_output=True)
    if view.returncode:
        if 'not found' in view.stderr.lower():
            return None
        raise ValueError('cannot read release: '+view.stderr)
    identifier = json.loads(view.stdout)['apiUrl'].rsplit('/', 1)[-1]
    if not identifier.isdecimal():
        raise ValueError('invalid GitHub release identifier')
    return json.loads(run(['gh', 'api', 'repos/'+repository+'/releases/'+identifier]))


def asset_matches(asset, path):
    return asset.get('state') == 'uploaded' and asset.get('size') == path.stat().st_size and asset.get('digest') == 'sha256:'+digest(path)


def recover_draft_asset(repository, tag, name, path):
    """Recheck state before deleting only a failed, empty draft upload."""
    release = release_info(repository, tag)
    if release is None:
        raise ValueError('GitHub release disappeared while retrying the upload')
    asset = next((item for item in release['assets'] if item['name'] == name), None)
    if asset is None:
        return False
    if asset_matches(asset, path):
        return True
    empty_digests = (None, '', 'sha256:'+hashlib.sha256(b'').hexdigest())
    if not release['draft'] or asset.get('state') != 'starter' or asset.get('size') != 0 or asset.get('digest') not in empty_digests:
        raise ValueError('existing GitHub asset differs: '+name)
    identifier = asset.get('id')
    if type(identifier) is not int or identifier <= 0:
        raise ValueError('invalid incomplete GitHub asset identifier')
    run(['gh', 'api', '--method', 'DELETE', 'repos/'+repository+'/releases/assets/'+str(identifier)])
    print('Removed incomplete draft upload:', name, flush=True)
    return False


def publish(args):
    data = validate_publication(args.output)
    if not re.fullmatch(r'[A-Za-z0-9_.@-]+', args.host) or args.host.startswith('-'):
        raise ValueError('invalid SSH destination')
    repository = run(['gh', 'repo', 'view', '--json', 'nameWithOwner', '--jq', '.nameWithOwner'], cwd=ROOT).strip()
    if data['repository'] != repository:
        raise ValueError('release belongs to a different repository')
    release = release_info(repository, data['tag'])
    if release is None:
        notes = args.output.parent/(data['tag']+'.notes.md')
        notes.write_text('PBS-X client previews: Windows/Linux '+data['host_version']+', Android '+data['android_version']+'.\n\nDesktop bundles include the background agent, native window, CLI, manifest and installer. Android retains its established preview signing certificate. Installation instructions and public downloads: https://pbsx.rikka.ca/downloads/ .\n\nVerified native build: '+data['ci_run_url']+'\n\nSHA256SUMS and release.json record file integrity and source provenance. These previews do not enable automatic client updates.\n')
        run(['gh', 'release', 'create', data['tag'], '--repo', repository, '--target', data['publisher_commit'], '--draft', '--prerelease', '--title', 'PBS-X client previews — '+data['host_version'], '--notes-file', notes])
        release = release_info(repository, data['tag'])
    if release['draft'] and release['target_commitish'] != data['publisher_commit']:
        if release['assets']:
            raise ValueError('draft already contains assets from another source; retry the original publication')
        run(['gh', 'release', 'edit', data['tag'], '--repo', repository, '--target', data['publisher_commit']])
    assets = {asset['name']:asset for asset in release['assets']}
    print('Uploading and verifying GitHub release assets...', flush=True)
    for name in sorted(PUBLIC_FILES):
        file = args.output/name
        if name in assets:
            asset = assets[name]
            if asset_matches(asset, file):
                continue
            if recover_draft_asset(repository, data['tag'], name, file):
                continue
        run(['gh', 'release', 'upload', data['tag'], file, '--repo', repository])
    assets = {asset['name']:asset for asset in release_info(repository, data['tag'])['assets']}
    for name in PUBLIC_FILES:
        if assets[name].get('digest') != 'sha256:'+digest(args.output/name):
            raise ValueError('GitHub upload digest mismatch: '+name)
    options = ['-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', '-o', 'StrictHostKeyChecking=yes']
    if args.identity:
        options += ['-o', 'IdentitiesOnly=yes', '-i', str(args.identity.resolve())]
    if args.known_hosts:
        options += ['-o', 'UserKnownHostsFile='+str(args.known_hosts.resolve())]
    staging = '/srv/pbs-x-apt/incoming/clients/'+data['tag']
    print('Staging verified public downloads...', flush=True)
    run(['ssh', *options, args.host, shlex.join(['install', '-d', '-m', '0700', staging])])
    run(['scp', *options, *[str(args.output/name) for name in sorted(PUBLIC_FILES)], __file__, args.host+':'+staging+'/'])
    activation = ['python3', staging+'/'+Path(__file__).name, 'activate', '--staging', staging]
    if args.allow_rollback:
        activation.append('--allow-rollback')
    print(run(['ssh', *options, args.host, shlex.join(activation)]).strip())
    run(['gh', 'release', 'edit', data['tag'], '--repo', repository, '--draft=false'])
    print('Published:', 'https://pbsx.rikka.ca/downloads/')
    print('GitHub:', 'https://github.com/'+repository+'/releases/tag/'+data['tag'])


def collect(args):
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}', args.tag):
        raise ValueError('invalid release tag')
    repository = run(['gh', 'repo', 'view', '--json', 'nameWithOwner', '--jq', '.nameWithOwner'], cwd=ROOT).strip()
    info = json.loads(run(['gh', 'api', 'repos/'+repository+'/actions/runs/'+str(args.run_id)]))
    if info.get('conclusion') != 'success' or info.get('path') != '.github/workflows/host-app.yml' or info.get('head_repository', {}).get('full_name') != repository:
        raise ValueError('expected a successful native host-app workflow in this repository')
    if args.build_android:
        env = dict(os.environ)
        java = Path('/usr/lib/jvm/java-17-openjdk-amd64')
        if not env.get('JAVA_HOME') and java.exists():
            env['JAVA_HOME'] = str(java)
            env['PATH'] = str(java/'bin')+os.pathsep+env['PATH']
        subprocess.run(['bash', str(ROOT/'pbsx-android/build.sh')], cwd=ROOT, env=env, check=True)
    with tempfile.TemporaryDirectory(prefix='pbsx-client-artifacts-') as name:
        work = Path(name)
        for system in ('linux', 'windows'):
            if getattr(args,'build_release',None):
                import host_release_transfer
                host_release_transfer.collect(repository,args.build_release,info,system,work/system)
            else:
                run(['gh', 'run', 'download', str(args.run_id), '--repo', repository, '--name', 'host-app-'+system+'-preview', '--dir', work/system])
            candidates = list((work/system).rglob('manifest.json'))
            if len(candidates) != 1:
                raise ValueError('ambiguous CI bundle layout')
            setattr(args, system+'_bundle', candidates[0].parent)
        prepare(args, bundle(args.linux_bundle, 'linux'), bundle(args.windows_bundle, 'windows'), info)
    print('Prepared:', args.output)
    if args.command == 'release':
        publish(args)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    for name in ('prepare', 'release', 'publish'):
        sub = commands.add_parser(name)
        sub.add_argument('--output', type=Path, required=True, help='release directory (new for prepare/release)')
        if name != 'publish':
            sub.add_argument('--run-id', type=int, required=True)
            sub.add_argument('--tag', required=True)
            sub.add_argument('--build-android', action='store_true')
            sub.add_argument('--build-release', help='draft pbsx-client-build-* transport release from the same successful dispatched run')
        if name != 'prepare':
            sub.add_argument('--host', required=True)
            sub.add_argument('--identity', type=Path)
            sub.add_argument('--known-hosts', type=Path)
            sub.add_argument('--allow-rollback', action='store_true', help='explicitly promote an older public release')
    sub = commands.add_parser('activate')
    sub.add_argument('--staging', type=Path, required=True)
    sub.add_argument('--allow-rollback', action='store_true')
    args = parser.parse_args()
    try:
        if args.command == 'activate':
            allowed = Path('/srv/pbs-x-apt/incoming/clients')
            if os.geteuid() != 0 or socket.gethostname() != 'pbs-x-apt' or args.staging.resolve().parent != allowed:
                raise ValueError('activation requires a private PBS-X repository-host staging directory')
            public = Path('/srv/pbs-x-apt/public')
            data = activate(args.staging.resolve(), public, Path('/var/lib/pbs-x-apt/release'), args.allow_rollback)
            print('Published public client release:', data['tag'], '; current latest:', (public/'downloads/latest').readlink().name)
        elif args.command == 'publish':
            publish(args)
        else:
            collect(args)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        detail = error.stderr if isinstance(error, subprocess.CalledProcessError) else str(error)
        parser.exit(1, 'Client publication failed: '+str(detail)+'\n')
