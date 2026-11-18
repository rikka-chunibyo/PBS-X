#!/usr/bin/env python3
"""Run the production completion module's Rust tests without a PBS installation."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import tomllib

ROOT = Path(__file__).resolve().parents[1]
packages = tomllib.loads((ROOT / 'Cargo.lock').read_text())['package']


def version(name):
    versions = {item['version'] for item in packages if item['name'] == name}
    if len(versions) != 1:
        raise ValueError('expected one locked version for ' + name)
    return versions.pop()


with tempfile.TemporaryDirectory(prefix='pbsx-completion-tests-') as directory:
    work = Path(directory)
    (work / 'src').mkdir()
    (work / 'Cargo.toml').write_text('''[package]
name = "pbsx-completion-tests"
version = "0.0.0"
edition = "2021"
[workspace]
[dependencies]
anyhow = "=''' + version('anyhow') + '''"
serde = { version = "=''' + version('serde') + '''", features = ["derive"] }
serde_json = "=''' + version('serde_json') + '''"
''')
    module = json.dumps(str(ROOT / 'src/server/host_run_completion.rs'), ensure_ascii=False)
    (work / 'src/lib.rs').write_text('#[allow(dead_code)]\n#[path = ' + module + ']\nmod completion;\n')
    env = dict(os.environ)
    env['CARGO_BUILD_JOBS'] = '1'
    env.setdefault('CARGO_HOME', str(ROOT / 'artifacts/rust-completion-cache'))
    env.setdefault('CARGO_TARGET_DIR', str(ROOT / 'artifacts/rust-completion-target'))
    # Running outside the PBS workspace avoids its Debian-only Cargo registry
    # configuration. The production module is included directly, without copies.
    subprocess.run(['cargo', 'test', '--quiet'], cwd=work, env=env, check=True)
