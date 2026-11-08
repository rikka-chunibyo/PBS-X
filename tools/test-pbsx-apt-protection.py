#!/usr/bin/python3
"""Exercise real installed APT hooks on disposable VM115, including failures."""
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile

assert os.geteuid() == 0 and socket.gethostname() == 'pbsx-install-115'
os.umask(0o077)
os.environ['DEBIAN_FRONTEND'] = 'noninteractive'
os.environ['LC_ALL'] = 'C'
LOGS = Path('/var/tmp/pbsx-apt-protection-tests')
LOGS.mkdir(mode=0o700, exist_ok=True)
STATE = Path('/run/pbsx-repository-update.json')
GUARD = '/usr/lib/pbsx-repository/apt-guard.py'
UNITS = ('proxmox-backup.service', 'proxmox-backup-proxy.service')


def run(args, check=True, log=None):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if log:
        (LOGS/log).write_text(result.stdout)
    if check and result.returncode:
        raise RuntimeError(result.stdout[-3000:])
    return result


def healthy():
    for unit in (*UNITS, 'pbsx-agent.service'):
        run(['systemctl', 'is-active', '--quiet', unit])
    assert not STATE.exists(), 'recovery state was not cleared'
    assert not run(['dpkg', '--audit']).stdout.strip()


def active_refusal():
    healthy()
    root = Path('/var/lib/proxmox-backup/pbsx-host-runs/pbsx-apt-guard-probe')
    assert not root.exists()
    before = {unit: run(['systemctl', 'show', '-p', 'MainPID', unit]).stdout for unit in UNITS}
    archives = set(Path('/var/backups').glob('pbsx-apt-*'))
    try:
        (root/'running').mkdir(parents=True)
        # This host ID is not enrolled; no agent consumes the isolated state fixture.
        (root/'running/probe.json').write_text('{"fixture":"active-work-refusal"}\n')
        result = run(['apt-get', '-y', '--reinstall', 'install',
                      'proxmox-backup-server=4.2.9-1+pbsx1'], False, 'active-refusal.log')
        assert result.returncode and 'backup/restore is running' in result.stdout, result.stdout
        assert archives == set(Path('/var/backups').glob('pbsx-apt-*'))
        for unit in UNITS:
            assert run(['systemctl', 'show', '-p', 'MainPID', unit]).stdout == before[unit]
    finally:
        shutil.rmtree(root)
    healthy()
    print('PASS: real APT refuses active host work before stopping services or changing packages', flush=True)


def failed_transaction():
    healthy()
    archives = set(Path('/var/backups').glob('pbsx-apt-*'))
    with tempfile.TemporaryDirectory(prefix='pbsx-apt-failure-', dir='/var/tmp') as temporary:
        root = Path(temporary)
        root.chmod(0o755)
        control = root/'package/DEBIAN'
        control.mkdir(parents=True)
        control.chmod(0o755)
        control.parent.chmod(0o755)
        (control/'control').write_text('Package: pbsx-apt-failure-probe\nVersion: 1.0\nArchitecture: all\n'
                                     'Maintainer: PBS-X tests <apt@pbsx.rikka.ca>\n'
                                     'Description: Disposable failing preinst, VM115 only\n')
        (control/'preinst').write_text('#!/bin/sh\necho "intentional PBS-X failure probe" >&2\nexit 42\n')
        (control/'preinst').chmod(0o755)
        package = root/'probe.deb'
        run(['dpkg-deb', '--build', str(root/'package'), str(package)])
        package.chmod(0o644)
        result = run(['apt-get', '-y', '--reinstall', 'install',
                      'proxmox-backup-server=4.2.9-1+pbsx1', str(package)], False, 'failed-transaction.log')
        assert result.returncode and 'intentional PBS-X failure probe' in result.stdout, result.stdout
        assert 'PBS-X protected pre-update state:' in result.stdout, result.stdout
        assert 'PBS-X services restored;' in result.stdout, result.stdout
    run(['dpkg', '--remove', 'pbsx-apt-failure-probe'], False, 'failure-probe-cleanup.log')
    for unit in UNITS:
        run(['systemctl', 'is-active', '--quiet', unit])
    assert not STATE.exists()
    # dpkg can leave later packages unpacked when one preinst fails. Exercise
    # the normal repair too; service recovery is not automatic package rollback.
    run(['apt-get', '-y', '--fix-broken', 'install'], log='transaction-repair.log')
    healthy()
    created = set(Path('/var/backups').glob('pbsx-apt-*')) - archives
    assert created, 'missing automatic state archive'
    backup = created.pop()
    assert backup.stat().st_mode & 0o777 == 0o700
    assert (backup/'server-state.tar').stat().st_mode & 0o077 == 0
    entries = run(['tar', '-tf', str(backup/'server-state.tar')]).stdout
    assert 'etc/proxmox-backup/' in entries and 'var/lib/proxmox-backup/pbsx-agent-credentials/' in entries
    print('PASS: failed dpkg transaction retains protected state and restarts original services', flush=True)
    return backup


def interrupted_recovery(backup):
    healthy()
    child = subprocess.Popen([sys.executable, '-c',
        'import fcntl,sys; files=[open(p,"r+") for p in '
        '["/var/lib/dpkg/lock-frontend","/var/lib/dpkg/lock"]]; '
        '[fcntl.lockf(f,fcntl.LOCK_EX|fcntl.LOCK_NB) for f in files]; '
        'print("locked",flush=True); sys.stdin.read()'], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
    try:
        assert child.stdout.readline().strip() == 'locked'
        STATE.write_text(json.dumps({'version': 1, 'units': list(UNITS), 'backup': str(backup)}))
        run(['systemctl', 'stop', *UNITS])
        run([GUARD, 'recover'])
        assert STATE.exists()
        for unit in UNITS:
            assert run(['systemctl', 'is-active', '--quiet', unit], False).returncode != 0
        child.stdin.close()
        child.wait(timeout=5)
        run([GUARD, 'recover'], log='interrupted-recovery.log')
        healthy()
    finally:
        if child.poll() is None:
            child.kill()
            child.wait()
        if STATE.exists():
            run([GUARD, 'recover'], False)
        run(['systemctl', 'start', *UNITS])
    print('PASS: recovery waits for real dpkg locks, then resumes services after an interrupted update', flush=True)


def native_roundtrip():
    def api(method, *args):
        output = run(['proxmox-backup-debug', 'api', method,
                      '/config/host-backup/linux-install-115', *args, '--output-format', 'json']).stdout
        return json.loads(output) if output.strip() else None
    enabled = api('get')['enable']
    api('set', '--enable', 'true')
    try:
        for phase in ('backup', 'restore'):
            print(run(['python3', '/home/rikka/pbsx-install-e2e.py', phase]).stdout.strip(), flush=True)
    finally:
        api('set', '--enable', str(enabled).lower())


active_refusal()
backup = failed_transaction()
interrupted_recovery(backup)
native_roundtrip()
healthy()
print('PASS: all public-repository APT protection integration checks', flush=True)
