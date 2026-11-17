#!/usr/bin/env python3
"""Verify the preview APK and record build provenance without exporting its key."""
import hashlib
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import zipfile

from android_source import source_snapshot

ROOT = Path(__file__).resolve().parents[1]


def run(*args):
    return subprocess.check_output(args, cwd=ROOT, text=True, stderr=subprocess.PIPE)


def main(args):
    app = ROOT/'pbsx-android'
    if args.capture_source:
        args.capture_source.write_text(json.dumps(source_snapshot(ROOT), indent=2)+'\n')
        return
    before = json.loads(args.source_record.read_text())
    after = source_snapshot(ROOT)
    if before['source_inputs'] != after['source_inputs'] or before['dirty'] != after['dirty']:
        raise ValueError('Android source inputs changed during the build; rebuild before publishing')
    apk = app/'artifacts/pbsx-android-preview.apk'
    sdk = Path(os.environ.get('ANDROID_HOME', app/'.tools/sdk'))
    build_tools = sdk/'build-tools/36.0.0'
    certificates = run(str(build_tools/'apksigner'), 'verify', '--print-certs', str(apk))
    values = re.findall(r'^Signer #\d+ certificate SHA-256 digest: ([0-9a-f]{64})$', certificates, re.M)
    expected = (ROOT/'packaging/client-downloads/android-preview-certificate.sha256').read_text().strip()
    if values != [expected]:
        raise ValueError('APK signing certificate changed; retain the existing preview keystore')
    run(str(build_tools/'zipalign'), '-c', '-P', '16', '4', str(apk))
    badging = run(str(build_tools/'aapt'), 'dump', 'badging', str(apk))
    if re.search(r'^application-debuggable', badging, re.M):
        raise ValueError('downloadable APK must have debugging disabled')
    metadata = json.loads((app/'app/build/outputs/apk/release/output-metadata.json').read_text())
    item, = metadata['elements']
    if metadata['applicationId'] != 'com.pbsx.android' or metadata['variantName'] != 'release' or item['filters']:
        raise ValueError('expected the universal PBS-X Android APK')
    with zipfile.ZipFile(apk) as archive:
        abis = sorted({name.split('/')[1] for name in archive.namelist() if name.startswith('lib/') and name.endswith('.so')})
    if abis != ['arm64-v8a', 'x86_64']:
        raise ValueError('unexpected APK architectures')
    info = {
        'schema': 2, 'version': item['versionName'], 'version_code': item['versionCode'],
        'application_id': metadata['applicationId'], 'architectures': abis,
        'variant': 'release', 'debuggable': False,
        'source_commit': before['source_commit'],
        'source_tree': before['source_inputs']['pbsx-android'],
        'source_inputs': before['source_inputs'], 'dirty': before['dirty'],
        'certificate_sha256': expected, 'sha256': hashlib.sha256(apk.read_bytes()).hexdigest(),
        'size': apk.stat().st_size,
        'checks': ['go-tests', 'android-unit-tests', 'lint', 'apk-signature', '16k-alignment', 'non-debuggable'],
    }
    (app/'artifacts/build-info.json').write_text(json.dumps(info, indent=2)+'\n')
    print('Verified Android preview:', info['version'], 'certificate:', expected)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--capture-source', type=Path)
    parser.add_argument('--source-record', type=Path, default=ROOT/'pbsx-android/artifacts/build-source.json')
    main(parser.parse_args())
