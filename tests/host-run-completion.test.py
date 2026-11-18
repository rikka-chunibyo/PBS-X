#!/usr/bin/env python3
"""Run the production completion module's Rust tests without a PBS installation."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = ROOT / 'tests/host-run-completion'


with tempfile.TemporaryDirectory(prefix='pbsx-completion-tests-') as directory:
    work = Path(directory)
    for name in ('Cargo.toml', 'Cargo.lock'):
        (work / name).write_bytes((FIXTURE / name).read_bytes())
    module = json.dumps(str(ROOT / 'src/server/host_run_completion.rs'), ensure_ascii=False)
    (work / 'lib.rs').write_text('#[allow(dead_code)]\n#[path = ' + module + ']\nmod completion;\n')
    env = dict(os.environ)
    env['CARGO_BUILD_JOBS'] = '1'
    env.setdefault('CARGO_HOME', str(ROOT / 'artifacts/rust-completion-cache'))
    env.setdefault('CARGO_TARGET_DIR', str(ROOT / 'artifacts/rust-completion-target'))
    # Running outside the PBS workspace avoids its Debian-only Cargo registry
    # configuration. The production module is included directly, without copies.
    subprocess.run(['cargo', 'test', '--locked', '--quiet'], cwd=work, env=env, check=True)
