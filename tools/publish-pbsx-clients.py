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
    if info.get('dirty') is not False or info.get('source_tree') != run(['git', 'rev-parse', 'HEAD:pbsx-android'], cwd=ROOT).strip():
        raise ValueError('Android build is dirty or stale; run pbsx-android/build.sh')
    required = {'go-tests', 'android-unit-tests', 'lint', 'apk-signature', '16k-alignment', 'non-debuggable'}
    certificate = (ROOT/'packaging/client-downloads/android-preview-certificate.sha256').read_text().strip()
    if info.get('variant') != 'release' or info.get('debuggable') is not False or set(info.get('checks', [])) != required or info.get('certificate_sha256') != certificate or info.get('sha256') != digest(apk) or info.get('size') != apk.stat().st_size:
        raise ValueError('Android build verification does not match the APK')
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
<p><a href="'''+prefix+'''SHA256SUMS">Checksums</a> · <a href="'''+prefix+'''release.json">Build details</a> · <a href="/iso/pbsx_4.2.9-1+pbsx1-offline-amd64.iso">Server ISO</a> · <a href="/">APT repository</a></p><small>Release: '''+tag+'''</small></html>\n'''


def atomic_text(path, text, mode=0o644):
    descriptor, name = tempfile.mkstemp(prefix='.pbsx-', dir=path.parent)
    try:
        with os.fdopen(descriptor, 'w') as stream:
            stream.write(text)
            stream.flush()
            os.fchmod(stream.fileno(), mode)
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        Path(name).unlink(missing_ok=True)


def activate(staging, public, private):
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
                os.rename(work, destination)
        latest = downloads/'latest'
        if os.path.lexists(latest) and not latest.is_symlink():
            raise ValueError('latest is not a managed symlink')
        link = downloads/('.latest-'+uuid.uuid4().hex)
        try:
            link.symlink_to('releases/'+data['tag'])
            os.replace(link, latest)
        finally:
            link.unlink(missing_ok=True)
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
                atomic_text(index, old+'\n<p><a href="/downloads/">Windows, Linux and Android apps</a> · <a href="/iso/pbsx_4.2.9-1+pbsx1-offline-amd64.iso">PBS-X offline ISO</a></p>\n')
    return data


def publish(args):
    data = validate_publication(args.output)
    if not re.fullmatch(r'[A-Za-z0-9_.@-]+', args.host) or args.host.startswith('-'):
        raise ValueError('invalid SSH destination')
    repository = run(['gh', 'repo', 'view', '--json', 'nameWithOwner', '--jq', '.nameWithOwner'], cwd=ROOT).strip()
    if data['repository'] != repository:
        raise ValueError('release belongs to a different repository')
    api = ['gh', 'api', 'repos/'+repository+'/releases/tags/'+data['tag']]
    existing = subprocess.run(api, text=True, capture_output=True)
    if existing.returncode:
        if '404' not in existing.stderr:
            raise ValueError('cannot read release: '+existing.stderr)
        notes = args.output.parent/(data['tag']+'.notes.md')
        notes.write_text('PBS-X client previews: Windows/Linux '+data['host_version']+', Android '+data['android_version']+'.\n\nDesktop bundles include the background agent, native window, CLI, manifest and installer. Android retains its established preview signing certificate. Installation instructions and public downloads: https://pbsx.rikka.ca/downloads/ .\n\nVerified native build: '+data['ci_run_url']+'\n\nSHA256SUMS and release.json record file integrity and source provenance. These previews do not enable automatic client updates.\n')
        run(['gh', 'release', 'create', data['tag'], '--repo', repository, '--target', data['publisher_commit'], '--draft', '--prerelease', '--title', 'PBS-X client previews — '+data['host_version'], '--notes-file', notes])
    release = json.loads(run(api))
    assets = {asset['name']:asset for asset in release['assets']}
    print('Uploading and verifying GitHub release assets...', flush=True)
    for name in sorted(PUBLIC_FILES):
        file = args.output/name
        if name in assets:
            asset = assets[name]
            if asset.get('state') != 'uploaded' or asset['size'] != file.stat().st_size or asset.get('digest') != 'sha256:'+digest(file):
                raise ValueError('existing GitHub asset differs: '+name)
        else:
            run(['gh', 'release', 'upload', data['tag'], file, '--repo', repository])
    assets = {asset['name']:asset for asset in json.loads(run(api))['assets']}
    for name in PUBLIC_FILES:
        if assets[name].get('digest') != 'sha256:'+digest(args.output/name):
            raise ValueError('GitHub upload digest mismatch: '+name)
    options = ['-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15']
    if args.identity:
        options += ['-o', 'IdentitiesOnly=yes', '-i', str(args.identity.resolve())]
    if args.known_hosts:
        options += ['-o', 'UserKnownHostsFile='+str(args.known_hosts.resolve())]
    staging = '/srv/pbs-x-apt/incoming/clients/'+data['tag']
    print('Staging verified public downloads...', flush=True)
    run(['ssh', *options, args.host, shlex.join(['install', '-d', '-m', '0700', staging])])
    run(['scp', *options, *[str(args.output/name) for name in sorted(PUBLIC_FILES)], __file__, args.host+':'+staging+'/'])
    print(run(['ssh', *options, args.host, shlex.join(['python3', staging+'/'+Path(__file__).name, 'activate', '--staging', staging])]).strip())
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
        if name != 'prepare':
            sub.add_argument('--host', required=True)
            sub.add_argument('--identity', type=Path)
            sub.add_argument('--known-hosts', type=Path)
    sub = commands.add_parser('activate')
    sub.add_argument('--staging', type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == 'activate':
            allowed = Path('/srv/pbs-x-apt/incoming/clients')
            if os.geteuid() != 0 or socket.gethostname() != 'pbs-x-apt' or args.staging.resolve().parent != allowed:
                raise ValueError('activation requires a private PBS-X repository-host staging directory')
            data = activate(args.staging.resolve(), Path('/srv/pbs-x-apt/public'), Path('/var/lib/pbs-x-apt/release'))
            print('Activated public client release:', data['tag'])
        elif args.command == 'publish':
            publish(args)
        else:
            collect(args)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        detail = error.stderr if isinstance(error, subprocess.CalledProcessError) else str(error)
        parser.exit(1, 'Client publication failed: '+str(detail)+'\n')
