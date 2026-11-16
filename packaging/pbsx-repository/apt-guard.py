#!/usr/bin/python3
"""APT transaction protection for the PBS-X server, installed by pbsx-repository."""
import fcntl
from contextlib import contextmanager
import json
import os
import pwd
from pathlib import Path
import subprocess
import stat
import sys
import tempfile
import time

PACKAGES = {'proxmox-backup-server', 'proxmox-backup-client', 'proxmox-backup-file-restore', 'proxmox-backup-client-static'}
STATE = Path('/run/pbsx-repository-update.json')
RUNS = Path('/var/lib/proxmox-backup/pbsx-host-runs')
CONFIG = Path('/etc/proxmox-backup')
BACKUPS = Path('/var/backups')
UNITS = ('proxmox-backup.service', 'proxmox-backup-proxy.service')
PATHS = ('etc/proxmox-backup', 'var/lib/proxmox-backup/pbsx-agent-credentials',
         'var/lib/proxmox-backup/host-status', 'var/lib/proxmox-backup/pbsx-host-runs',
         'var/lib/proxmox-backup/pbsx-enrollment')


def run(args, check=True):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if check and result.returncode:
        raise RuntimeError(f'{args[0]} failed: {result.stderr.strip()[-1000:]}')
    return result


def actions(stream):
    if stream.readline().strip() != 'VERSION 2':
        raise RuntimeError('Unsupported APT hook protocol; expected version 2')
    for line in stream:
        if not line.strip():
            break
    changed = {}
    for line in stream:
        fields = line.rstrip('\r\n').split(maxsplit=4)
        if len(fields) != 5:
            raise RuntimeError('Malformed APT package action')
        name, _, _, _, action = fields
        if name in PACKAGES:
            changed[name] = action
    return changed


def active(unit):
    return run(['systemctl', 'is-active', '--quiet', unit], False).returncode == 0


def check_idle():
    for kind in ('running', 'restore-running'):
        if next(RUNS.glob(f'*/{kind}/*.json'), None) is not None:
            raise RuntimeError('PBS-X host backup/restore is running; retry the update when idle')
    if active('proxmox-backup.service'):
        output = run(['proxmox-backup-debug', 'api', 'get', '/nodes/localhost/tasks',
                      '--running', 'true', '--output-format', 'json']).stdout
        if json.loads(output) != []:
            raise RuntimeError('PBS tasks are running; retry the update when idle')


@contextmanager
def host_dispatch_gate():
    """Exclude native queue/claim mutations through the idle check and stop.

    This is flock, matching pbs_config::open_backup_lockfile. STATE continues
    to block dispatch across hook processes and package-triggered restarts.
    """
    descriptor = os.open(CONFIG/'.pbsx-maintenance.lck',
                         os.O_RDWR | os.O_CREAT | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK, 0o660)
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise RuntimeError('Unsafe PBS-X maintenance lock')
        user = pwd.getpwnam('backup')
        os.fchown(descriptor, user.pw_uid, user.pw_gid)
        os.fchmod(descriptor, 0o660)
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError('PBS-X host dispatch is busy; retry the update') from None
        yield
    finally:
        os.close(descriptor)


def read_state():
    data = json.loads(STATE.read_text())
    if data.get('version') != 1 or not set(data['units']).issubset(set(UNITS)):
        raise RuntimeError('Invalid PBS-X update recovery state')
    return data


def resume():
    if not STATE.exists():
        return
    state = read_state()
    removing = False
    if state.get('remove-server', False):
        status = run(['dpkg-query', '-W', '-f=${db:Status-Status}', 'proxmox-backup-server'], False)
        removing = status.returncode != 0 or status.stdout in ('not-installed', 'config-files')
    if state['units'] and not removing:
        run(['systemctl', 'start', *state['units']])
        for unit in state['units']:
            if not active(unit):
                raise RuntimeError(f'{unit} did not restart; inspect {state["backup"]}')
    STATE.unlink()
    print(f'PBS-X services restored; protected pre-update state: {state["backup"]}', flush=True)


def prepare(changed):
    if not changed:
        return
    if 'proxmox-backup-server' in changed:
        installed = run(['dpkg-query', '-W', '-f=${db:Status-Status}', 'pve-manager'], False)
        if installed.returncode == 0 and installed.stdout == 'installed':
            raise RuntimeError('PBS-X server requires a dedicated PBS system, not a PVE hypervisor')
    pending = read_state() if STATE.exists() else None
    if not CONFIG.exists() and not any(active(unit) for unit in UNITS) and pending is None:
        return  # First server installation has no prior server state.
    with host_dispatch_gate():
        prepare_locked(changed, pending)


def prepare_locked(changed, pending):
    check_idle()
    # Carry restart intent through a repair instead of trying to start possibly
    # broken binaries before dpkg gets a chance to repair them.
    units = [unit for unit in UNITS if active(unit) or (pending and unit in pending['units'])]
    backup = Path(tempfile.mkdtemp(prefix=time.strftime('pbsx-apt-%Y%m%d-%H%M%S-'), dir=BACKUPS))
    backup.chmod(0o700)
    (backup/'installed-packages.tsv').write_text(run(['dpkg-query', '-W', '-f=${Package}\t${Version}\n']).stdout)
    data = {'version': 1, 'units': units, 'backup': str(backup), 'packages': sorted(changed),
            'remove-server': changed.get('proxmox-backup-server') == '**REMOVE**'}
    descriptor, name = tempfile.mkstemp(prefix='.pbsx-update-', dir=STATE.parent)
    with os.fdopen(descriptor, 'w') as out:
        json.dump(data, out)
        out.flush()
        os.fsync(out.fileno())
    os.replace(name, STATE)
    try:
        # Stop before archiving so configuration and credential history agree.
        check_idle()
        run(['systemctl', 'stop', 'proxmox-backup-proxy.service', 'proxmox-backup.service'])
        for kind in ('running', 'restore-running'):
            if next(RUNS.glob(f'*/{kind}/*.json'), None) is not None:
                raise RuntimeError('Host work became active; update cancelled')
        paths = [path for path in PATHS if Path('/'+path).exists()]
        run(['tar', '--acls', '--xattrs', '-C', '/', '-cpf', str(backup/'server-state.tar'), '--', *paths])
        print(f'PBS-X protected pre-update state: {backup}', flush=True)
    except BaseException:
        resume()
        raise


def recover():
    if not STATE.exists():
        return
    # APT and dpkg use POSIX locks, not flock. Never restart during unpack.
    descriptors = []
    try:
        for name in ('/var/lib/dpkg/lock-frontend', '/var/lib/dpkg/lock'):
            descriptor = os.open(name, os.O_RDWR | os.O_CREAT, 0o640)
            descriptors.append(descriptor)
            try:
                fcntl.lockf(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                return
        resume()
    finally:
        for descriptor in descriptors:
            os.close(descriptor)


def main():
    if os.geteuid() != 0:
        raise RuntimeError('PBS-X APT protection must run as root')
    os.umask(0o077)
    mode = sys.argv[1] if len(sys.argv) > 1 else Path(sys.argv[0]).name
    if mode == 'pre-install':
        descriptor = int(os.environ.get('APT_HOOK_INFO_FD', '0'))
        with os.fdopen(os.dup(descriptor)) as stream:
            prepare(actions(stream))
    elif mode == 'post-install':
        resume()
    elif mode == 'recover':
        recover()
    else:
        raise RuntimeError('Unknown PBS-X APT protection operation')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'PBS-X update protection: {error}', file=sys.stderr)
        sys.exit(1)
