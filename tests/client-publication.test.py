#!/usr/bin/env python3
import hashlib
import importlib.util
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
import subprocess
from types import SimpleNamespace
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('clients', ROOT/'tools/publish-pbsx-clients.py')
clients = importlib.util.module_from_spec(spec)
spec.loader.exec_module(clients)


def staged(parent, tag, content=b'app fixture', created_at='2026-11-17T00:00:00+00:00'):
    folder = parent/tag
    folder.mkdir()
    data = {'schema':1, 'tag':tag, 'created_at':created_at, 'host_version':'0.7.0-preview', 'android_version':'0.5.3-preview', 'files':{}}
    for name in sorted(clients.ASSETS):
        (folder/name).write_bytes(content)
        data['files'][name] = {'sha256':clients.digest(folder/name), 'size':len(content)}
    (folder/'release.json').write_text(json.dumps(data))
    (folder/'SHA256SUMS').write_text(''.join(clients.digest(folder/name)+'  '+name+'\n' for name in sorted(clients.ASSETS | {'release.json'})))
    return folder


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.public = self.root/'public'; self.public.mkdir()
        self.private = self.root/'private'
        (self.public/'index.html').write_text('<h1>Existing repository</h1>')

    def test_publish_exposes_only_allowlisted_assets_and_preserves_homepage(self):
        source = staged(self.root, 'preview-one')
        (source/'private-settings.json').write_text('not for publication')
        clients.activate(source, self.public, self.private)
        destination = self.public/'downloads/releases/preview-one'
        self.assertEqual({p.name for p in destination.iterdir()}, clients.PUBLIC_FILES | {'index.html'})
        self.assertEqual((self.public/'downloads/latest').readlink(), Path('releases/preview-one'))
        self.assertIn('Existing repository', (self.public/'index.html').read_text())
        self.assertIn('/downloads/', (self.public/'index.html').read_text())
        self.assertEqual((destination/'pbsx-android.apk').stat().st_mode & 0o777, 0o644)
        self.assertEqual(destination.stat().st_mode & 0o777, 0o755)

    def test_draft_lookup_uses_release_id_before_a_tag_exists(self):
        view = subprocess.CompletedProcess([], 0, json.dumps({'apiUrl':'https://api.github.com/repos/example/project/releases/123'}), '')
        with patch.object(clients.subprocess, 'run', return_value=view), patch.object(clients, 'run', return_value='{"draft":true,"assets":[]}') as api:
            result = clients.release_info('example/project', 'unpublished-preview')
        self.assertTrue(result['draft'])
        api.assert_called_once_with(['gh','api','repos/example/project/releases/123'])

    def test_corrupt_release_does_not_change_latest(self):
        clients.activate(staged(self.root, 'preview-one'), self.public, self.private)
        other = staged(self.root, 'preview-two')
        (other/'pbsx-android.apk').write_bytes(b'altered')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            clients.activate(other, self.public, self.private)
        self.assertEqual((self.public/'downloads/latest').readlink(), Path('releases/preview-one'))
        self.assertFalse((self.public/'downloads/releases/preview-two').exists())

    def test_same_tag_cannot_replace_published_bytes(self):
        first = staged(self.root, 'preview-one')
        clients.activate(first, self.public, self.private)
        changed = self.root/'changed'; changed.mkdir()
        second = staged(changed, 'preview-one', b'different app')
        with self.assertRaisesRegex(ValueError, 'immutable'):
            clients.activate(second, self.public, self.private)
        self.assertEqual((self.public/'downloads/latest/pbsx-android.apk').read_bytes(), b'app fixture')

    def test_identical_release_can_be_retried(self):
        first = staged(self.root, 'preview-one')
        clients.activate(first, self.public, self.private)
        apk = self.public/'downloads/latest/pbsx-android.apk'
        inode = apk.stat().st_ino
        clients.activate(first, self.public, self.private)
        self.assertEqual(apk.stat().st_ino, inode)
        self.assertEqual((self.public/'index.html').read_text().count('href="/downloads/"'), 1)

    def test_retrying_older_release_keeps_newer_latest(self):
        first = staged(self.root, 'older-preview')
        later = staged(self.root, 'newer-preview', created_at='2026-11-18T00:00:00+00:00')
        clients.activate(first, self.public, self.private)
        clients.activate(later, self.public, self.private)
        clients.activate(first, self.public, self.private)
        self.assertEqual((self.public/'downloads/latest').readlink(), Path('releases/newer-preview'))
        self.assertTrue((self.public/'downloads/releases/older-preview').is_dir())

    def test_explicit_rollback_can_promote_older_release(self):
        first = staged(self.root, 'older-preview')
        later = staged(self.root, 'newer-preview', created_at='2026-11-18T00:00:00+00:00')
        clients.activate(first, self.public, self.private)
        clients.activate(later, self.public, self.private)
        clients.activate(first, self.public, self.private, allow_rollback=True)
        self.assertEqual((self.public/'downloads/latest').readlink(), Path('releases/older-preview'))

    def test_invalid_timestamp_cannot_change_latest(self):
        clients.activate(staged(self.root, 'valid-preview'), self.public, self.private)
        for stamp in ('not a date', '2026-11-18T00:00:00', None):
            with self.subTest(stamp=stamp):
                source = staged(self.root, 'invalid-'+str(len(str(stamp))), created_at=stamp)
                with self.assertRaisesRegex(ValueError, 'creation timestamp'):
                    clients.activate(source, self.public, self.private)
                self.assertEqual((self.public/'downloads/latest').readlink(), Path('releases/valid-preview'))

    def test_empty_draft_starter_upload_can_be_removed_for_retry(self):
        path = self.root/'asset'; path.write_bytes(b'verified file')
        release = {'draft':True, 'assets':[{'name':'asset', 'state':'starter', 'size':0, 'digest':None, 'id':123}]}
        with patch.object(clients, 'release_info', return_value=release), patch.object(clients, 'run') as api:
            self.assertFalse(clients.recover_draft_asset('example/project', 'preview', 'asset', path))
        api.assert_called_once_with(['gh','api','--method','DELETE','repos/example/project/releases/assets/123'])

    def test_recovery_preserves_published_or_nonempty_or_completed_assets(self):
        path = self.root/'asset'; path.write_bytes(b'verified file')
        for draft, state, size, fingerprint in (
            (False, 'starter', 0, None),
            (True, 'starter', 5, None),
            (True, 'uploaded', 0, None),
            (True, 'starter', 0, 'sha256:'+'0'*64),
        ):
            with self.subTest(draft=draft,state=state,size=size,fingerprint=fingerprint):
                release = {'draft':draft, 'assets':[{'name':'asset','state':state,'size':size,'digest':fingerprint,'id':123}]}
                with patch.object(clients, 'release_info', return_value=release), patch.object(clients, 'run') as api:
                    with self.assertRaisesRegex(ValueError, 'asset differs'):
                        clients.recover_draft_asset('example/project', 'preview', 'asset', path)
                    api.assert_not_called()

    def test_recovery_rechecks_an_upload_that_has_finished(self):
        path = self.root/'asset'; path.write_bytes(b'verified file')
        release = {'draft':False, 'assets':[{'name':'asset','state':'uploaded','size':path.stat().st_size,'digest':'sha256:'+clients.digest(path),'id':123}]}
        with patch.object(clients, 'release_info', return_value=release), patch.object(clients, 'run') as api:
            self.assertTrue(clients.recover_draft_asset('example/project', 'preview', 'asset', path))
            api.assert_not_called()

    def test_publication_retries_failed_asset_then_finishes_without_clobber(self):
        source = staged(self.root, 'draft-preview')
        data = json.loads((source/'release.json').read_text())
        data.update(repository='example/project', publisher_commit='1'*40)
        (source/'release.json').write_text(json.dumps(data))
        (source/'SHA256SUMS').write_text(''.join(clients.digest(source/name)+'  '+name+'\n' for name in sorted(clients.ASSETS | {'release.json'})))
        assets = [{'name':name,'id':index+1,'state':'uploaded','size':(source/name).stat().st_size,'digest':'sha256:'+clients.digest(source/name)} for index,name in enumerate(sorted(clients.PUBLIC_FILES))]
        assets[0].update(state='starter',size=0,digest=None)
        release = {'draft':True,'target_commitish':'1'*40,'assets':assets}
        def execute(args, **kwargs):
            if args[:3] == ['gh','repo','view']:
                return 'example/project\n'
            if args[:3] == ['gh','release','upload']:
                file = Path(args[4])
                next(item for item in assets if item['name'] == file.name).update(state='uploaded',size=file.stat().st_size,digest='sha256:'+clients.digest(file))
            return ''
        args = SimpleNamespace(output=source,host='root@fixture',identity=None,known_hosts=None,allow_rollback=False)
        with patch.object(clients,'release_info',return_value=release),patch.object(clients,'run',side_effect=execute) as commands:
            clients.publish(args)
        uploads = [call.args[0] for call in commands.call_args_list if call.args[0][:3] == ['gh','release','upload']]
        self.assertEqual(len(uploads), 1)
        self.assertEqual(Path(uploads[0][4]).name, 'SHA256SUMS')
        self.assertNotIn('--clobber', uploads[0])
        self.assertTrue(any('--draft=false' in call.args[0] for call in commands.call_args_list))

    def test_symlinked_asset_cannot_expose_private_file(self):
        source = staged(self.root, 'preview-one')
        asset = source/'pbsx-android.apk'
        outside = self.root/'private-file'; outside.write_bytes(asset.read_bytes())
        asset.unlink(); asset.symlink_to(outside)
        with self.assertRaisesRegex(ValueError, 'regular file'):
            clients.activate(source, self.public, self.private)
        self.assertFalse((self.public/'downloads').exists())

    def test_traversal_and_unknown_asset_are_rejected(self):
        source = staged(self.root, 'preview-one')
        data = json.loads((source/'release.json').read_text())
        for tag in ('../escape', '/absolute'):
            data['tag'] = tag
            (source/'release.json').write_text(json.dumps(data))
            with self.assertRaisesRegex(ValueError, 'tag/schema'):
                clients.validate_publication(source)
        data['tag'] = 'preview-one'
        data['files']['../secret'] = data['files'].pop('pbsx-android.apk')
        (source/'release.json').write_text(json.dumps(data))
        with self.assertRaisesRegex(ValueError, 'unexpected public assets'):
            clients.validate_publication(source)

    def test_checksum_index_must_cover_the_release_metadata(self):
        source = staged(self.root, 'preview-one')
        (source/'SHA256SUMS').write_text('')
        with self.assertRaisesRegex(ValueError, 'checksum index mismatch'):
            clients.validate_publication(source)

    def test_linux_archive_restores_execute_bits_lost_by_ci_artifacts(self):
        source = self.root/'linux'; source.mkdir()
        fields = {}
        header = bytearray(64); header[:6] = b'\x7fELF\x02\x01'; header[18:20] = b'\x3e\x00'
        for name in ('pbsx-agent', 'pbsx-agentctl', 'pbsx-desktop', 'install.py'):
            payload = bytes(header) if name.startswith('pbsx-') else b'# fixture installer\n'
            (source/name).write_bytes(payload)
            (source/name).chmod(0o644)
            fields[name] = {'sha256':hashlib.sha256(payload).hexdigest(), 'size':len(payload)}
        data = {'schema':1,'os':'linux','arch':'amd64','version':'0.7.0-preview','source_commit':'1'*40,'dirty':False,'files':fields}
        (source/'manifest.json').write_text(json.dumps(data))
        clients.bundle(source, 'linux')
        output = self.root/'linux.tar.gz'
        clients.archive_bundle(source, data, output)
        with tarfile.open(output) as archive:
            for name in ('pbsx-agent','pbsx-agentctl','pbsx-desktop'):
                self.assertEqual(archive.getmember('pbsx-linux-amd64/'+name).mode, 0o755)
            self.assertEqual(archive.getmember('pbsx-linux-amd64/install.py').mode, 0o644)


if __name__ == '__main__': unittest.main()
