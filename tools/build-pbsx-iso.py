#!/usr/bin/env python3
"""Create an interactive offline PBS-X ISO; building may fetch authenticated inputs."""
import argparse
import datetime
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

import pbsx_iso as iso


def build(args):
    for command in ('xorriso', 'unsquashfs', 'dpkg-deb', 'apt-get', 'gpg', 'gpgv', 'curl'):
        if shutil.which(command) is None:
            raise ValueError('missing build tool: '+command)
    output = args.output.resolve()
    if output.exists():
        raise ValueError('output ISO already exists; choose a new filename')
    output.parent.mkdir(parents=True, exist_ok=True)
    cache = args.cache.resolve()
    cache.mkdir(parents=True, exist_ok=True)
    base = args.base_iso.resolve() if args.base_iso else cache/'proxmox-backup-server_4.2-1.iso'
    if not base.exists():
        iso.download(iso.SPEC['url'], base)
    if iso.sha256(base) != iso.SPEC['sha256']:
        raise ValueError('base ISO does not match the pinned official release checksum')
    package_directory = args.packages_dir.resolve() if args.packages_dir else cache/'pbsx-packages'
    if args.packages_dir:
        requested = [item[0] for key, item in iso.inventory(package_directory).items() if key in (*iso.CORE, 'pbsx-repository')]
    else:
        requested = iso.fetch_pbsx(package_directory, args.version)
    selected = {fields['Package']: (path, fields) for path in requested for fields in [iso.package(path)]}
    if not all(name in selected for name in (*iso.CORE, 'pbsx-repository')):
        raise ValueError('server, client, file-restore and repository packages are all required')
    if subprocess.run(['dpkg', '--compare-versions', selected['pbsx-repository'][1]['Version'], 'ge', '1.0.3']).returncode:
        raise ValueError('offline installation requires chroot-safe pbsx-repository >= 1.0.3')
    versions = {selected[name][1]['Version'] for name in iso.CORE}
    if len(versions) != 1 or '+pbsx' not in next(iter(versions)):
        raise ValueError('PBS-X executables must have one matching fork version')
    version = next(iter(versions))
    if args.version and version != args.version:
        raise ValueError('local package version does not match --version')
    report = iso.run(['xorriso', '-indev', base, '-report_system_area', 'cmd']).stdout
    match = re.search(r"^-volume_date uuid '([0-9]{16})'$", report, re.M)
    if not match:
        raise ValueError('cannot preserve the upstream boot volume UUID')
    uuid = match.group(1)
    with tempfile.TemporaryDirectory(prefix='.pbsx-iso-', dir=output.parent) as name:
        work = Path(name)
        pool = work/'packages'
        iso.run(['xorriso', '-osirrox', 'on', '-indev', base,
                 '-extract', '/proxmox/packages', pool,
                 '-extract', '/pbs-base.squashfs', work/'base.squashfs',
                 '-extract', '/boot/grub/grub.cfg', work/'grub.cfg'])
        # ISO directories are read-only; this staging pool must accept replacements.
        pool.chmod(0o755)
        (work/'grub.cfg').chmod(0o644)
        base_status = iso.run(['unsquashfs', '-cat', work/'base.squashfs', 'var/lib/dpkg/status']).stdout
        original = iso.inventory(pool)
        packages = iso.overlay_packages(pool, requested)
        dependencies = args.dependencies_dir.resolve() if args.dependencies_dir else cache/'dependencies'
        if args.dependencies_dir:
            additions = [item[0] for item in iso.inventory(dependencies).values()]
        else:
            additions = iso.resolve_dependencies(base_status, packages, dependencies)
        packages = iso.overlay_dependencies(pool, additions)
        iso.verify_release_packages(packages, selected)
        iso.check_closure(base_status, packages)
        source = iso.run(['git', 'rev-parse', 'HEAD'], cwd=iso.ROOT).stdout.strip() if (iso.ROOT/'.git').exists() else None
        dirty = bool(iso.run(['git', 'status', '--porcelain'], cwd=iso.ROOT).stdout) if source else None
        manifest = {
            'schema': 1, 'product': 'PBS-X', 'mode': 'interactive', 'architecture': 'amd64',
            'version': version, 'base_url': iso.SPEC['url'], 'base_sha256': iso.SPEC['sha256'],
            'repository': iso.SPEC['repository'], 'repository_fingerprint': iso.SPEC['repository_fingerprint'],
            'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'builder_commit': source,
            'builder_dirty': dirty,
            'firmware': ['bios', 'uefi'], 'network_required_to_install': False,
            'packages': [{'name': fields['Package'], 'version': fields['Version'], 'filename': path.name,
                          'sha256': iso.sha256(path), 'size': path.stat().st_size}
                         for key, (path, fields) in sorted(packages.items())
                         if key not in original or fields['Version'] != original[key][1]['Version']],
        }
        defaults = iso.build_defaults_package(work/'defaults', manifest)
        packages = iso.overlay_packages(pool, [defaults])
        iso.verify_release_packages(packages, selected)
        iso.check_closure(base_status, packages)
        manifest['packages'].append({'name': 'pbsx-offline-installer', 'version': '1.0.0', 'filename': defaults.name,
                                     'sha256': iso.sha256(defaults), 'size': defaults.stat().st_size})
        (work/'PBS-X-ISO.json').write_text(json.dumps(manifest, indent=2)+'\n')
        (work/'PBS-X-README.txt').write_text(
            f'PBS-X {version} offline installer\n\n'
            'Based on the official Proxmox Backup Server 4.2-1 installation media.\n'
            'Installations use the packages on this media; an Internet connection is not required.\n'
            'Choose the target disks, password and network configuration in the installer.\n'
            'Updates: apt update && apt upgrade\n'
            f'PBS-X feed: {iso.SPEC["repository"]}\n'
            'Debian and Proxmox community repositories supply system dependencies.\n'
            'This media contains no preselected disks, administrator password or personal SSH keys.\n')
        grub = (work/'grub.cfg').read_text().replace('Proxmox Backup Server', 'PBS-X')
        (work/'grub.cfg').write_text(grub)
        candidate = work/'pbsx.iso'
        # Append a replacement filesystem session as the upstream assistant does.
        # Replaying the hybrid HFS+/GPT layout with relocated EFI files can
        # create overlapping partitions in xorriso 1.5.6.
        shutil.copyfile(base, candidate)
        iso.run(['xorriso', '-abort_on', 'FAILURE', '-dev', candidate,
                 '-boot_image', 'any', 'keep', '-volume_date', 'uuid', uuid,
                 '-rm_r', '/proxmox/packages', '--', '-map', pool, '/proxmox/packages',
                 '-map', work/'grub.cfg', '/boot/grub/grub.cfg',
                 '-map', work/'PBS-X-ISO.json', '/PBS-X-ISO.json',
                 '-map', work/'PBS-X-README.txt', '/PBS-X-README.txt', '-commit'])
        boot = iso.run(['xorriso', '-indev', candidate, '-report_el_torito', 'plain', '-report_system_area', 'plain']).stdout
        if not re.search(r'El Torito boot img\s*:\s*1\s+BIOS', boot) or not re.search(r'El Torito boot img\s*:\s*2\s+UEFI', boot):
            raise ValueError('remastered image lost a BIOS/UEFI boot entry')
        iso.run(['xorriso', '-osirrox', 'on', '-indev', candidate, '-extract', '/proxmox/packages', work/'verify-packages'])
        exported = iso.inventory(work/'verify-packages')
        if set(exported) != set(packages):
            raise ValueError('ISO package membership differs from the checked offline pool')
        for name, (path, fields) in exported.items():
            if iso.sha256(path) != iso.sha256(packages[name][0]):
                raise ValueError('ISO package content mismatch: '+name)
        candidate.replace(output)
        output.with_suffix('.manifest.json').write_text(json.dumps(manifest, indent=2)+'\n')
        output.with_suffix('.boot.txt').write_text(boot)
    output.with_suffix('.iso.sha256').write_text(iso.sha256(output)+'  '+output.name+'\n')
    print('Built offline PBS-X ISO:', output)
    print('Server version:', version)
    print('SHA256:', iso.sha256(output))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--cache', type=Path, default=iso.ROOT/'artifacts/iso/cache')
    parser.add_argument('--base-iso', type=Path)
    parser.add_argument('--packages-dir', type=Path, help='trusted local release packages; otherwise fetch from signed PBS-X feed')
    parser.add_argument('--dependencies-dir', type=Path, help='pre-fetched dependencies; otherwise resolve with isolated signed Debian/Proxmox APT')
    parser.add_argument('--version', help='published PBS-X version; default is newest published matching executable set')
    try:
        build(parser.parse_args())
    except subprocess.CalledProcessError as error:
        parser.exit(1, (error.stdout or '')+(error.stderr or '')+f'\nISO build failed: {error}\n')
    except (OSError, ValueError) as error:
        parser.exit(1, f'ISO build failed: {error}\n')
