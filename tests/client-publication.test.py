#!/usr/bin/env python3
import hashlib
import importlib.util
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('clients', ROOT/'tools/publish-pbsx-clients.py')
clients = importlib.util.module_from_spec(spec)
spec.loader.exec_module(clients)


def staged(parent, tag, content=b'app fixture'):
    folder = parent/tag
    folder.mkdir()
    data = {'schema':1, 'tag':tag, 'host_version':'0.7.0-preview', 'android_version':'0.5.3-preview', 'files':{}}
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
