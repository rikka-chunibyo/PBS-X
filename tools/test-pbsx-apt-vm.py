#!/usr/bin/python3
"""Real APT integration on disposable VM115 only; never run on production."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import time

assert os.geteuid() == 0 and socket.gethostname() == 'pbsx-install-115'
ROOT = Path('/var/tmp/pbsx-apt-lab-tests')
LOGS = ROOT / 'logs'
KEY = Path('/etc/apt/keyrings/pbsx-lab-archive-key.asc')
CA = Path('/etc/apt/keyrings/pbsx-lab-tls-ca.crt')
SOURCE = Path('/etc/apt/sources.list.d/pbsx-lab.sources')
TLS = Path('/etc/apt/apt.conf.d/80pbsx-lab-ca')
URL = 'https://10.0.1.37'
PACKAGES = ('proxmox-backup-server', 'proxmox-backup-client', 'proxmox-backup-file-restore')
HOST = 'linux-install-115'
os.environ['LC_ALL'] = 'C'
os.environ['DEBIAN_FRONTEND'] = 'noninteractive'


def command(args, *, check=True, log=None, cwd=None):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, cwd=cwd)
    if log:
        path = LOGS / log
        path.write_text(result.stdout)
        path.chmod(0o600)
    if check and result.returncode:
        raise RuntimeError(f'{args[0]} failed: {result.stdout[-3500:]}')
    return result


def api(method, path, *args):
    output = command(['proxmox-backup-debug', 'api', method, path, *args, '--output-format', 'json']).stdout
    if not output.strip():
        return None
    for line in (output, *reversed(output.splitlines())):
        try:
            return json.loads(line)
        except json.JSONDecodeError:
            pass
    raise RuntimeError('Invalid API response')


def source(url, key, suites='trixie'):
    return f'Types: deb\nURIs: {url}\nSuites: {suites}\nComponents: main\nArchitectures: amd64\nSigned-By: {key}\n'


def isolated(name, endpoint='lab', key=KEY, trusted_tls=True):
    directory = ROOT / name
    directory.mkdir(mode=0o755)
    for part in ('lists/partial', 'cache/archives/partial', 'empty', 'downloads'):
        (directory / part).mkdir(parents=True, mode=0o755)
    (directory / 'source.sources').write_text(source(f'{URL}/{endpoint}', key))
    (directory / 'status').touch()
    ca = CA if trusted_tls else Path('/etc/ssl/certs/ca-certificates.crt')
    options = [
        '-o', f'Dir::Etc::sourcelist={directory}/source.sources',
        '-o', f'Dir::Etc::sourceparts={directory}/empty',
        '-o', f'Dir::State::lists={directory}/lists',
        '-o', f'Dir::State::status={directory}/status',
        '-o', f'Dir::Cache={directory}/cache',
        '-o', f'Acquire::https::10.0.1.37::CAInfo={ca}',
        '-o', 'Acquire::Retries=0', '-o', 'Acquire::https::Timeout=10',
        '-o', 'APT::Update::Error-Mode=any', '-o', 'Acquire::Languages=none',
    ]
    return directory, options


def reject(name, pattern, **kwargs):
    _, options = isolated(name, **kwargs)
    result = command(['apt-get', *options, 'update'], check=False, log=f'{name}.log')
    assert result.returncode != 0 and re.search(pattern, result.stdout, re.I | re.S), result.stdout
    print(f'PASS: {name} rejected', flush=True)


def health():
    assert not command(['dpkg', '--audit']).stdout.strip()
    for unit in ('proxmox-backup', 'proxmox-backup-proxy', 'pbsx-agent'):
        command(['systemctl', 'is-active', '--quiet', unit])
    assert api('get', '/nodes/localhost/tasks', '--running', 'true') == []
    command(['curl', '--insecure', '--fail', '--silent', 'https://127.0.0.1:8007/'])
    command(['zpool', 'status', '-x'])


def prepare():
    assert not ROOT.exists() and not SOURCE.exists() and not TLS.exists()
    ROOT.mkdir(mode=0o755)
    LOGS.mkdir(mode=0o700)
    KEY.parent.mkdir(mode=0o755, exist_ok=True)
    for origin, target in [('/home/rikka/pbsx-lab-archive-key.asc', KEY), ('/home/rikka/pbsx-lab-tls-ca.crt', CA)]:
        assert not target.exists()
        shutil.copyfile(origin, target)
        target.chmod(0o644)
    directory, options = isolated('valid')
    command(['apt-get', *options, 'update'], log='valid-update.log')
    policy = command(['apt-cache', *options, 'policy', *PACKAGES], log='valid-policy.log').stdout
    assert policy.count('Candidate: 4.2.9-1+pbsx1') == 3, policy
    command(['apt-get', *options, 'download', 'proxmox-backup-client=4.2.9-1+pbsx1'],
            log='valid-download.log', cwd=directory/'downloads')
    downloaded = next((directory/'downloads').glob('*.deb'))
    reference = Path('/home/rikka/pbsx-4.2.9-1+pbsx1-amd64')/downloaded.name
    assert hashlib.sha256(downloaded.read_bytes()).digest() == hashlib.sha256(reference.read_bytes()).digest()
    print('PASS: signed metadata, verified HTTPS and authentic package download', flush=True)
    reject('untrusted-tls', r'certificate.*(not trusted|unknown|failed|unable)', trusted_tls=False)
    reject('wrong-signing-key', r'NO_PUBKEY|Missing key|signature.*(failed|verified|check)',
           key=Path('/usr/share/keyrings/debian-archive-keyring.gpg'))
    reject('tampered-metadata', r'BADSIG|bad signature|signature.*(failed|verified|check)', endpoint='lab-negative/tampered')
    reject('unsigned-metadata', r'not signed', endpoint='lab-negative/unsigned')
    reject('expired-metadata', r'expired', endpoint='lab-negative/expired')
    directory, options = isolated('corrupt-package', endpoint='lab-negative/corrupt-package')
    command(['apt-get', *options, 'update'], log='corrupt-update.log')
    result = command(['apt-get', *options, 'download', 'proxmox-backup-client=4.2.9-1+pbsx1'],
                     check=False, log='corrupt-download.log', cwd=directory/'downloads')
    assert result.returncode and 'Hash Sum mismatch' in result.stdout, result.stdout
    print('PASS: corrupt package rejected against signed checksums', flush=True)
    result = command(['curl', '--silent', '--show-error', '--noproxy', '*', '--cacert', str(CA),
                      '--resolve', 'wrong.pbs-x.invalid:443:10.0.1.37',
                      'https://wrong.pbs-x.invalid/lab/dists/trixie/InRelease'], check=False, log='wrong-tls-name.log')
    assert result.returncode == 60, result.stdout
    print('PASS: TLS hostname mismatch rejected', flush=True)
    SOURCE.write_text(source(f'{URL}/lab', KEY, 'trixie trixie-baseline'))
    TLS.write_text(f'Acquire::https::10.0.1.37::CAInfo "{CA}";\n')
    for path in (SOURCE, TLS):
        path.chmod(0o644)
    command(['apt-get', '-o', 'APT::Update::Error-Mode=any', 'update'], log='system-update.log')
    print('PASS: ordinary apt update with repository-specific TLS and signing trust', flush=True)


def snapshot_state(phase):
    health()
    destination = Path('/var/backups') / f'pbsx-apt-lab-{phase}-{time.time_ns()}'
    destination.mkdir(mode=0o700)
    units = ('proxmox-backup', 'proxmox-backup-proxy', 'pbsx-agent')
    command(['systemctl', 'stop', 'pbsx-agent', 'proxmox-backup-proxy', 'proxmox-backup'])
    try:
        command(['tar', '--acls', '--xattrs', '-C', '/', '-cpf', str(destination/'server-state.tar'),
                 'etc/proxmox-backup', 'var/lib/proxmox-backup/pbsx-agent-credentials'])
    finally:
        command(['systemctl', 'start', *units])
    # The current package has no APT safety hook. This explicit backup protects
    # the lab rehearsal; it must not be described as automatic APT behavior.
    print(f'Protected manual pre-update state: {destination}', flush=True)


def roundtrip():
    path = f'/config/host-backup/{HOST}'
    enabled = api('get', path)['enable']
    api('set', path, '--enable', 'true')
    try:
        for phase in ('backup', 'restore'):
            print(command(['python3', '/home/rikka/pbsx-install-e2e.py', phase]).stdout.strip(), flush=True)
    finally:
        api('set', path, '--enable', str(enabled).lower())


def install(phase, version, *flags):
    snapshot_state(phase)
    command(['apt-get', '-o', 'APT::Update::Error-Mode=any', 'update'], log=f'{phase}-update.log')
    policy = command(['apt-cache', 'policy', *PACKAGES], log=f'{phase}-policy.log').stdout
    candidate = '4.2.9-1+pbsx1' if phase == 'reinstall' else '4.2.9-1+pbsx2'
    assert policy.count(f'Candidate: {candidate}') == 3, policy
    upstream = [(v, int(p)) for v, p in re.findall(r'^\s+(?:\*\*\*\s+)?(\d\S*)\s+(-?\d+)\s*$', policy, re.M) if '+pbsx' not in v]
    assert upstream and all(priority < 0 for _, priority in upstream), policy
    planned = command(['apt-get', '--simulate', 'upgrade'], log=f'{phase}-upgrade-plan.log').stdout
    if phase == 'upgrade':
        assert all(re.search(rf'^Inst {name} .*\(4\.2\.9-1\+pbsx2\b', planned, re.M) for name in PACKAGES), planned
        print('PASS: ordinary apt upgrade selects all three new fork packages', flush=True)
    elif phase == 'rollback':
        assert not any(re.search(rf'^Inst {name} ', planned, re.M) for name in PACKAGES), planned
        print('PASS: ordinary apt upgrade does not downgrade the installed newer fork', flush=True)
    for name in PACKAGES:
        for path in Path('/var/cache/apt/archives').glob(f'{name}_*.deb'):
            path.unlink()
    command(['apt-get', '-y', '-o', 'Dpkg::Options::=--force-confold', *flags,
             'install', *(f'{name}={version}' for name in PACKAGES)], log=f'{phase}-install.log')
    versions = command(['dpkg-query', '-W', '-f=${Package} ${Version}\n', *PACKAGES]).stdout
    assert all(line.endswith(' '+version) for line in versions.splitlines()), versions
    assert 'https://10.0.1.37/lab' in (LOGS/f'{phase}-install.log').read_text(), 'packages did not download from lab'
    health()
    roundtrip()
    print(f'PASS: APT {phase}, version={version}, healthy services and native backup/restore', flush=True)


def finish():
    health()
    SOURCE.write_text(SOURCE.read_text()+'Enabled: no\n')
    TLS.unlink()
    assert not api('get', f'/config/host-backup/{HOST}')['enable']
    assert all(line.endswith('4.2.9-1+pbsx1') for line in command(['dpkg-query','-W','-f=${Package} ${Version}\n',*PACKAGES]).stdout.splitlines())
    print('PASS: original package versions restored; test source disabled; example job remains disabled', flush=True)


phase = sys.argv[1]
if phase == 'prepare':
    prepare()
elif phase == 'reinstall':
    install(phase, '4.2.9-1+pbsx1', '--reinstall')
elif phase == 'upgrade':
    install(phase, '4.2.9-1+pbsx2', '--only-upgrade')
elif phase == 'rollback':
    install(phase, '4.2.9-1+pbsx1', '--allow-downgrades')
elif phase == 'finish':
    finish()
else:
    raise SystemExit('prepare | reinstall | upgrade | rollback | finish')
