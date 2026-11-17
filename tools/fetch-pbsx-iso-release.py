#!/usr/bin/env python3
"""Fetch hash-pinned ISO package inputs via authenticated GitHub release access."""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

import pbsx_iso as iso


def validate(path, expected):
    if path.stat().st_size != expected['size'] or iso.sha256(path) != expected['sha256']:
        raise ValueError('release input hash/size mismatch: '+expected['name'])
    fields = iso.package(path)
    if (fields['Package'], fields['Version']) != (expected['name'], expected['version']):
        raise ValueError('release input identity mismatch: '+expected['name'])


def fetch(output, version=None):
    manifest = json.loads((iso.ROOT/'packaging/iso/release-inputs.json').read_text())
    if version and version != manifest['version']:
        raise ValueError('requested version is not the pinned release; update release-inputs.json first')
    entries = manifest['packages']
    if {item['name'] for item in entries} != {*iso.CORE, 'pbsx-repository'} or len(entries) != 4:
        raise ValueError('release inputs must contain exactly the four PBS-X packages')
    for entry in entries:
        if not re.fullmatch(r'[A-Za-z0-9.+_-]+\.deb', entry['filename']):
            raise ValueError('unsafe release filename')
    output = output.resolve()
    if output.exists() and any(output.iterdir()):
        raise ValueError('release input directory must be empty')
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='pbsx-iso-release-') as directory:
        work = Path(directory)
        command = ['gh', 'release', 'download', manifest['tag'], '--repo', manifest['repository'], '--dir', work]
        for entry in entries:
            command += ['--pattern', entry['filename']]
        iso.run(command)
        for entry in entries:
            validate(work/entry['filename'], entry)
        for entry in entries:
            shutil.copy2(work/entry['filename'], output/entry['filename'])
    print('Verified pinned ISO package inputs:', manifest['version'])


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--version')
    args = parser.parse_args()
    try:
        fetch(args.output, args.version)
    except subprocess.CalledProcessError as error:
        parser.exit(1, (error.stderr or '')+'\nRelease input download failed.\n')
    except (OSError, ValueError) as error:
        parser.exit(1, str(error)+'\n')
