#!/usr/bin/env python3
"""Android artifact reuse must include shared Go sources and build-start state."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'tools'))
import android_source
spec = importlib.util.spec_from_file_location('android_info', ROOT/'tools/android-build-info.py')
android_info = importlib.util.module_from_spec(spec); spec.loader.exec_module(android_info)
spec = importlib.util.spec_from_file_location('clients', ROOT/'tools/publish-pbsx-clients.py')
clients = importlib.util.module_from_spec(spec); spec.loader.exec_module(clients)


class AndroidSourceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.vendor = self.root/'pbsx-host-agent/third_party/pxar'
        self.vendor.mkdir(parents=True)
        (self.vendor/'codec.go').write_text('package pxar\n// original source\n')
        (self.root/'pbsx-android/core').mkdir(parents=True)
        (self.root/'pbsx-android/core/go.mod').write_text('module fixture/android\n')
        (self.root/'.gitignore').write_text('**/artifacts/\n')
        pin = self.root/'packaging/client-downloads'
        pin.mkdir(parents=True)
        (pin/'android-preview-certificate.sha256').write_text('a'*64+'\n')
        self.git('init','-q')
        self.git('config','user.name','Test fixture')
        self.git('config','user.email','test@example.invalid')
        self.git('config','commit.gpgsign','false')
        self.commit()
        self.sources = patch.object(android_source, 'local_replacements', return_value=['pbsx-host-agent/third_party/pxar'])
        self.sources.start(); self.addCleanup(self.sources.stop)

    def git(self, *args):
        return subprocess.check_output(['git',*args],cwd=self.root,text=True,stderr=subprocess.PIPE).strip()

    def commit(self):
        self.git('add','.')
        self.git('commit','-qm','source fixture')

    def record(self):
        state = android_source.source_snapshot(self.root)
        artifacts = self.root/'pbsx-android/artifacts'; artifacts.mkdir(exist_ok=True)
        apk = artifacts/'fixture.apk'; apk.write_bytes(b'verified APK fixture')
        info = {
            'schema':2, **state, 'variant':'release', 'debuggable':False,
            'certificate_sha256':'a'*64, 'sha256':clients.digest(apk), 'size':apk.stat().st_size,
            'checks':['go-tests','android-unit-tests','lint','apk-signature','16k-alignment','non-debuggable'],
        }
        return info, apk

    def test_unchanged_inputs_allow_reusing_verified_apk(self):
        info, apk = self.record()
        with patch.object(clients, 'ROOT', self.root):
            clients.validate_android_build(info, apk)

    def test_committed_shared_dependency_change_rejects_cached_apk(self):
        info, apk = self.record()
        old_android_tree = info['source_inputs']['pbsx-android']
        (self.vendor/'codec.go').write_text('package pxar\n// fixed shared implementation\n')
        self.commit()
        self.assertEqual(android_source.source_snapshot(self.root)['source_inputs']['pbsx-android'], old_android_tree)
        with patch.object(clients, 'ROOT', self.root), self.assertRaisesRegex(ValueError, 'dirty or stale'):
            clients.validate_android_build(info, apk)

    def test_dirty_shared_dependency_rejects_cached_apk(self):
        info, apk = self.record()
        (self.vendor/'new.go').write_text('package pxar\n')
        self.assertTrue(android_source.source_snapshot(self.root)['dirty'])
        with patch.object(clients, 'ROOT', self.root), self.assertRaisesRegex(ValueError, 'dirty or stale'):
            clients.validate_android_build(info, apk)

    def test_legacy_record_cannot_claim_untracked_shared_dependencies(self):
        info, apk = self.record()
        info['schema'] = 1
        info.pop('source_inputs')
        with patch.object(clients, 'ROOT', self.root), self.assertRaisesRegex(ValueError, 'dirty or stale'):
            clients.validate_android_build(info, apk)

    def test_source_changed_during_build_cannot_be_stamped_as_current(self):
        info, _ = self.record()
        record = self.root/'pbsx-android/artifacts/build-source.json'
        record.write_text(json.dumps({key:info[key] for key in ('source_commit','source_inputs','dirty')}))
        (self.vendor/'codec.go').write_text('package pxar\n// source committed during build\n')
        self.commit()
        args = SimpleNamespace(capture_source=None, source_record=record)
        with patch.object(android_info, 'ROOT', self.root), self.assertRaisesRegex(ValueError, 'changed during the build'):
            android_info.main(args)


class GoReplacementDiscovery(unittest.TestCase):
    def test_local_replacements_are_discovered_but_versioned_modules_use_go_sum(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            data = '\n'.join(json.dumps(item) for item in [
                {'Path':'main/module','Main':True},
                {'Path':'shared/pxar','Replace':{'Dir':str(root/'shared/pxar')}},
                {'Path':'remote/module','Replace':{'Version':'v1.2.3','Dir':'/module-cache/remote'}},
            ])
            with patch.object(android_source.shutil, 'which', return_value='/fixture/go'), patch.object(android_source, 'command', return_value=data):
                self.assertEqual(android_source.local_replacements(root), ['shared/pxar'])

    def test_uncommitted_external_replacement_cannot_be_omitted_from_provenance(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            data = json.dumps({'Path':'outside/module','Replace':{'Dir':str(root.parent/'outside-sources')}})
            with patch.object(android_source.shutil, 'which', return_value='/fixture/go'), patch.object(android_source, 'command', return_value=data):
                with self.assertRaisesRegex(ValueError, 'inside this checkout'):
                    android_source.local_replacements(root)


if __name__ == '__main__': unittest.main()
