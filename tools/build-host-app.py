#!/usr/bin/env python3
"""Build a version-matched native agent/CLI/desktop preview bundle; never publish."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent


def source_info(root):
    if (root/'.git').exists():
        commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
        dirty = bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=root, text=True).strip())
    else:
        # Portable source exports avoid Windows-incompatible PBS filenames.
        marker = root/'.repoid'
        if marker.stat().st_size > 64:
            raise ValueError('invalid source commit marker')
        commit = marker.read_text().strip()
        dirty = None  # A source archive has no index against which to check edits.
    if not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('invalid source commit marker')
    return {'source_commit': commit, 'dirty': dirty}


def build(args):
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?', args.version):
        raise ValueError('use a plain semantic version, for example 0.7.0-preview')
    output = args.output.resolve()
    if output.exists():
        raise ValueError('output already exists; choose a new bundle directory')
    output.parent.mkdir(parents=True, exist_ok=True)
    go = os.environ.get('GO', 'go')
    env = {**os.environ, 'GOOS': args.os, 'GOARCH': 'amd64', 'CGO_ENABLED': '0'}
    if args.cc:
        env['CC'] = args.cc
    suffix = '.exe' if args.os == 'windows' else ''
    with tempfile.TemporaryDirectory(prefix='.pbsx-build-', dir=output.parent) as name:
        staging = Path(name)
        names = []
        for name, package, tags, graphics in [
            ('pbsx-agent', './cmd/agent', 'agent', False),
            ('pbsx-agentctl', './cmd/agentctl', '', False),
            ('pbsx-desktop', './cmd/desktop', 'desktop', True),
        ]:
            if graphics and args.headless:
                continue
            target = staging / (name + suffix)
            flags = f'-s -w -X main.Version={args.version}'
            if graphics and args.os == 'windows':
                flags += ' -H=windowsgui'
            command = [go, 'build', '-trimpath', '-buildvcs=false', '-ldflags', flags, '-o', str(target)]
            if tags:
                command += ['-tags', tags]
            command.append(package)
            subprocess.run(command, cwd=ROOT/'pbsx-host-agent', env={**env, 'CGO_ENABLED': '1' if graphics else '0'}, check=True)
            names.append(target.name)
        installer = 'install.ps1' if args.os == 'windows' else 'install.py'
        shutil.copyfile(ROOT/'packaging/host-app'/installer, staging/installer)
        names.append(installer)
        manifest = {
            'schema': 1, 'version': args.version, 'os': args.os, 'arch': 'amd64',
            **source_info(ROOT),
            'go': subprocess.check_output([go, 'version'], text=True).strip(),
            'files': {name: {'sha256': hashlib.sha256((staging/name).read_bytes()).hexdigest(),
                             'size': (staging/name).stat().st_size} for name in names},
        }
        (staging/'manifest.json').write_text(json.dumps(manifest, indent=2)+'\n')
        # One directory rename publishes a complete local bundle to the output.
        os.rename(staging, output)
    print(output)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--os', choices=['linux', 'windows'], required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--headless', action='store_true', help='omit the desktop and its C/graphics dependencies')
    parser.add_argument('--cc', help='C compiler for a cross-compiled native desktop')
    build(parser.parse_args())
