#!/usr/bin/env python3
"""Reject malformed or corrupted bundles before any privileged install actions."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('pbsx_installer', Path(__file__).resolve().parents[1]/'packaging/host-app/install.py')
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class BundleChecks(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='pbsx-bundle-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        elf = b'\x7fELF\x02\x01'+b'\x00'*12+b'\x3e\x00'+b'fixture'
        self.files = {'pbsx-agent': elf, 'pbsx-agentctl': elf, 'install.py': b'# installer fixture'}
        self.manifest = {'schema': 1, 'os': 'linux', 'arch': 'amd64', 'version': '0.7.0-preview', 'files': {}}
        for name, payload in self.files.items():
            (self.root/name).write_bytes(payload)
            self.manifest['files'][name] = {'sha256': hashlib.sha256(payload).hexdigest(), 'size': len(payload)}
        self.write_manifest()

    def write_manifest(self):
        (self.root/'manifest.json').write_text(json.dumps(self.manifest))

    def test_valid_headless_bundle(self):
        self.assertEqual(installer.validate(self.root)['version'], '0.7.0-preview')

    def test_tampering_same_length(self):
        payload = self.root/'pbsx-agent'
        data = bytearray(payload.read_bytes())
        data[-1] ^= 1
        payload.write_bytes(data)
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            installer.validate(self.root)

    def test_truncated_binary(self):
        (self.root/'pbsx-agent').write_bytes(b'\x7fELF')
        with self.assertRaisesRegex(ValueError, 'truncated'):
            installer.validate(self.root)

    def test_other_architecture_even_with_matching_hash(self):
        name = 'pbsx-agent'
        payload = bytearray(self.files[name])
        payload[18:20] = b'\xb7\x00'  # AArch64
        (self.root/name).write_bytes(payload)
        self.manifest['files'][name]['sha256'] = hashlib.sha256(payload).hexdigest()
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'amd64'):
            installer.validate(self.root)

    def test_manifest_cannot_escape_bundle(self):
        self.manifest['files']['../outside'] = {'sha256': 'ignored', 'size': 0}
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'file list'):
            installer.validate(self.root)

    def test_symlinked_executable(self):
        path = self.root/'pbsx-agent'
        path.rename(self.root/'actual')
        path.symlink_to(self.root/'actual')
        with self.assertRaisesRegex(ValueError, 'unsafe'):
            installer.validate(self.root)

    def test_atomic_copy_keeps_old_file_if_source_fails(self):
        target = self.root/'installed'
        target.write_bytes(b'previous release')
        with self.assertRaises(FileNotFoundError):
            installer.atomic_copy(self.root/'missing', target)
        self.assertEqual(target.read_bytes(), b'previous release')
        self.assertFalse(list(self.root.glob('.pbsx-*')))

    def test_atomic_copy_replaces_symlink_without_following_it(self):
        outside = self.root/'unrelated'
        outside.write_bytes(b'leave alone')
        target = self.root/'installed'
        target.symlink_to(outside)
        installer.atomic_copy(self.root/'pbsx-agent', target)
        self.assertFalse(target.is_symlink())
        self.assertEqual(outside.read_bytes(), b'leave alone')


if __name__ == '__main__':
    unittest.main()
