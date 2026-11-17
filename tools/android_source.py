"""Committed source inputs for Android, including local Go replacement modules."""
import json
import os
from pathlib import Path
import shutil
import subprocess


def command(args, root, **kwargs):
    return subprocess.check_output([str(item) for item in args], cwd=root, text=True, stderr=subprocess.PIPE, **kwargs).strip()


def local_replacements(root):
    root = Path(root).resolve()
    sdk_go = root/'pbsx-android/.tools/go/bin/go'
    go = str(sdk_go) if sdk_go.is_file() else shutil.which('go')
    if not go:
        raise ValueError('Go is required to verify Android source dependencies')
    env = {**os.environ, 'GOTOOLCHAIN': 'local', 'GOWORK': 'off'}
    env.setdefault('GOPATH', str(root/'pbsx-android/.tools/gopath'))
    env.setdefault('GOCACHE', str(root/'pbsx-android/.tools/gocache'))
    output = command([go, 'list', '-mod=readonly', '-m', '-json', 'all'], root/'pbsx-android/core', env=env)
    decoder = json.JSONDecoder()
    paths = set()
    while output.strip():
        module, offset = decoder.raw_decode(output.lstrip())
        output = output.lstrip()[offset:].lstrip()
        replacement = module.get('Replace', {})
        if replacement and not replacement.get('Version'):
            if not replacement.get('Dir'):
                raise ValueError('local Go replacement has no source directory')
            path = Path(replacement['Dir']).resolve()
            try:
                paths.add(path.relative_to(root).as_posix())
            except ValueError:
                raise ValueError('Android local Go dependencies must be committed inside this checkout') from None
    return sorted(paths)


def source_snapshot(root):
    root = Path(root).resolve()
    paths = sorted({'pbsx-android', *local_replacements(root)})
    trees = {}
    for path in paths:
        expression = 'HEAD^{tree}' if path == '.' else 'HEAD:'+path
        trees[path] = command(['git', 'rev-parse', expression], root)
    return {
        'source_commit': command(['git', 'rev-parse', 'HEAD'], root),
        'source_inputs': trees,
        'dirty': bool(command(['git', 'status', '--porcelain', '--untracked-files=all', '--ignore-submodules=none', '--', *paths], root)),
    }
