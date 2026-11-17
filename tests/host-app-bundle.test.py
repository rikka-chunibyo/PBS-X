#!/usr/bin/env python3
"""Reject malformed or corrupted bundles before any privileged install actions."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('pbsx_installer', Path(__file__).resolve().parents[1]/'packaging/host-app/install.py')
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)
builder_spec = importlib.util.spec_from_file_location('pbsx_builder', Path(__file__).resolve().parents[1]/'tools/build-host-app.py')
builder = importlib.util.module_from_spec(builder_spec)
builder_spec.loader.exec_module(builder)


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

    def test_portable_source_export_records_commit_without_claiming_clean_index(self):
        (self.root/'.repoid').write_text('a'*40+'\n')
        info = builder.source_info(self.root)
        self.assertEqual(info['source_commit'], 'a'*40)
        self.assertIsNone(info['dirty'])

    def test_portable_source_export_rejects_invalid_commit_marker(self):
        for value in ('unknown', '../source', 'a'*100):
            (self.root/'.repoid').write_text(value)
            with self.assertRaises(ValueError):
                builder.source_info(self.root)

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

    def desktop(self):
        return {'files': {'pbsx-desktop': {}}, 'desktop_runtime': {
            'libc': 'glibc', 'glibc_min': '2.38', 'libraries': ['libX11.so.6', 'libc.so.6']}}

    def test_builder_records_actual_symbol_and_library_requirements(self):
        output = ['Name: GLIBC_2.9\nName: GLIBC_2.38\nName: GLIBC_2.34\n',
                  '(NEEDED) Shared library: [libX11.so.6]\n(NEEDED) Shared library: [libc.so.6]\n']
        with patch.object(builder.subprocess, 'check_output', side_effect=output):
            self.assertEqual(builder.linux_runtime(self.root/'desktop'), self.desktop()['desktop_runtime'])

    def test_desktop_rejects_old_or_unknown_libc_before_loading_libraries(self):
        for libc in ('glibc 2.36', 'musl 1.2.5', None):
            with self.subTest(libc=libc), patch.object(installer.os, 'confstr', return_value=libc), patch.object(installer.ctypes, 'CDLL') as load:
                with self.assertRaisesRegex(ValueError, 'requires glibc 2.38'):
                    installer.check_runtime(self.desktop())
                load.assert_not_called()

    def test_desktop_checks_system_libraries_and_accepts_compatible_runtime(self):
        with patch.object(installer.os, 'confstr', return_value='glibc 2.39'), patch.object(installer.ctypes, 'CDLL') as load:
            installer.check_runtime(self.desktop())
            self.assertEqual([call.args[0] for call in load.call_args_list], ['libX11.so.6', 'libc.so.6'])
        with patch.object(installer.os, 'confstr', return_value='glibc 2.39'), patch.object(installer.ctypes, 'CDLL', side_effect=OSError('missing dependency')):
            with self.assertRaisesRegex(ValueError, 'desktop library unavailable'):
                installer.check_runtime(self.desktop())

    def test_missing_or_unsafe_runtime_metadata_is_rejected(self):
        for runtime in ({}, {**self.desktop()['desktop_runtime'], 'libraries':['../../libc.so.6']}, {**self.desktop()['desktop_runtime'], 'libraries':'libc.so.6'}):
            with self.subTest(runtime=runtime), self.assertRaisesRegex(ValueError, 'requirements are missing or invalid'):
                installer.check_runtime({'files':{'pbsx-desktop':{}}, 'desktop_runtime':runtime})

    def test_headless_does_not_require_glibc_or_graphics_runtime(self):
        with patch.object(installer.os, 'confstr') as libc, patch.object(installer.ctypes, 'CDLL') as load:
            installer.check_runtime(self.desktop(), headless=True)
            installer.check_runtime({'files':{'pbsx-agent':{}}})
            libc.assert_not_called()
            load.assert_not_called()

    def test_incompatible_desktop_fails_before_installation_paths_or_service_changes(self):
        desktop = self.root/'pbsx-desktop'
        desktop.write_bytes(self.files['pbsx-agent'])
        self.manifest['files']['pbsx-desktop'] = {'size': desktop.stat().st_size, 'sha256': hashlib.sha256(desktop.read_bytes()).hexdigest()}
        self.manifest['desktop_runtime'] = self.desktop()['desktop_runtime']
        self.write_manifest()
        with patch.object(installer.os, 'geteuid', return_value=0), patch.object(installer.os, 'umask'), patch.object(installer.platform, 'system', return_value='Linux'), patch.object(installer.platform, 'machine', return_value='x86_64'), patch.object(installer.shutil, 'which', return_value='/fixture/systemctl'), patch.object(installer.os, 'confstr', return_value='glibc 2.36'), patch.object(installer, 'root_directory') as directories, patch.object(installer, 'command') as commands:
            with self.assertRaisesRegex(ValueError, 'requires glibc'):
                installer.install(self.root, headless=False)
            directories.assert_not_called()
            commands.assert_not_called()


if __name__ == '__main__':
    unittest.main()
