#!/usr/bin/env python3
"""Stable APT/ISO publication regressions with real tiny Debian input packages."""
import copy
import gzip
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'tools'))
import pbsx_iso_publication as pub
spec = importlib.util.spec_from_file_location('server_publisher', ROOT/'tools/publish-pbsx-server.py')
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)
fetch_spec = importlib.util.spec_from_file_location('iso_fetcher', ROOT/'tools/fetch-pbsx-iso-release.py')
fetcher = importlib.util.module_from_spec(fetch_spec)
fetch_spec.loader.exec_module(fetcher)


class IsoPublicationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.public = self.root/'public'
        self.public.mkdir()
        (self.public/'index.html').write_text('<h1>Existing repo</h1><a href="/iso/old.iso">ISO</a>')

    def bundle(self, identity='release', version='4.2.9-1+pbsx2', repository_version='1.0.3', created='2000-01-01T00:00:00+00:00', payload='fixture'):
        bundle = self.root/identity
        bundle.mkdir()
        for name in pub.PACKAGES:
            ver = repository_version if name == 'pbsx-repository' else version
            arch = 'all' if name == 'pbsx-repository' else 'amd64'
            source = bundle/(name+'-source')
            (source/'DEBIAN').mkdir(parents=True)
            (source/'DEBIAN/control').write_text(f'Package: {name}\nVersion: {ver}\nArchitecture: {arch}\nMaintainer: Test <test@invalid>\nDescription: fixture\n')
            (source/'payload').write_text(payload)
            subprocess.run(['dpkg-deb', '--root-owner-group', '--threads-max=1', '--build', source,
                            bundle/(name+'_'+ver+'_'+arch+'.deb')], check=True, capture_output=True,
                           env={**os.environ, 'SOURCE_DATE_EPOCH': '1700000000'})
        entries = [pub.descriptor(p) for p in sorted(bundle.glob('*.deb'))]
        inputs = {'repository': 'rikka-chunibyo/PBS-X', 'tag': identity, 'version': version, 'packages': entries}
        (bundle/'iso-inputs.json').write_text(json.dumps(inputs))
        (bundle/pub.IMAGE).write_bytes(b'opaque ISO fixture '+identity.encode())
        digest = pub.digest(bundle/pub.IMAGE)
        (bundle/(pub.IMAGE+'.sha256')).write_text(digest+'  '+pub.IMAGE+'\n')
        manifest = {'version': version, 'builder_commit': 'a'*40, 'builder_dirty': False,
                    'network_required_to_install': False, 'firmware': ['bios', 'uefi'], 'created_at': created,
                    'packages': [{k: v for k, v in e.items() if k != 'architecture'} for e in entries]}
        (bundle/'pbsx-offline-amd64.manifest.json').write_text(json.dumps(manifest))
        (bundle/'pbsx-offline-amd64.boot.txt').write_text('boot report fixture')
        metadata = {'schema': 1, 'release_id': identity, 'version': version, 'repository_version': repository_version,
                    'builder_commit': 'a'*40, 'ci_run_id': 42, 'created_at': created,
                    'image_size': (bundle/pub.IMAGE).stat().st_size, 'image_sha256': digest}
        (bundle/'iso-publication.json').write_text(json.dumps(metadata))
        return bundle

    def inspect(self, path):
        return pub.read_json(path.parent/'pbsx-offline-amd64.manifest.json')

    def stable(self, bundle):
        index = self.public/'apt/dists/trixie/main/binary-amd64/Packages.gz'
        index.parent.mkdir(parents=True, exist_ok=True)
        text = ''
        for p in sorted(bundle.glob('*.deb')):
            item = pub.descriptor(p)
            text += '\n'.join(f'{k}: {v}' for k, v in {
                'Package': item['name'], 'Version': item['version'], 'Architecture': item['architecture'],
                'Filename': 'pool/'+item['filename'], 'Size': item['size'], 'SHA256': item['sha256']}.items())+'\n\n'
        index.write_bytes(gzip.compress(text.encode()))

    def active(self, bundle):
        pub.prepare(bundle, self.public, self.inspect)
        self.stable(bundle)
        pub.activate(bundle, self.public)

    def test_matching_publication_switches_latest_and_keeps_private_files_private(self):
        bundle = self.bundle()
        (bundle/'credentials.json').write_text('private fixture')
        self.active(bundle)
        latest = self.public/'iso/latest'
        self.assertEqual(latest.readlink(), Path('releases/release'))
        self.assertEqual((latest/pub.IMAGE).read_bytes(), (bundle/pub.IMAGE).read_bytes())
        self.assertEqual({p.name for p in latest.iterdir()}, set(pub.PUBLIC_FILES)|{'index.html'})
        self.assertIn(pub.LATEST_URL, (self.public/'index.html').read_text())
        self.assertEqual((latest/pub.IMAGE).stat().st_mode & 0o777, 0o644)

    def test_private_umask_does_not_make_public_iso_unreadable(self):
        bundle = self.bundle()
        previous = os.umask(0o077)
        try:self.active(bundle)
        finally:os.umask(previous)
        for name in ('iso', 'iso/releases', 'iso/latest'):
            self.assertEqual((self.public/name).stat().st_mode & 0o777, 0o755)
        self.assertEqual((self.public/'iso/latest'/pub.IMAGE).stat().st_mode & 0o777, 0o644)

    def test_another_release_cannot_overwrite_a_pending_transaction(self):
        pending = self.root/'pending'
        first = self.root/'first';first.mkdir()
        second = self.root/'second';second.mkdir()
        pending.write_text(str(first.resolve())+'\n')
        pub.check_pending(pending, first)
        with self.assertRaisesRegex(ValueError, 'pending APT/ISO'):
            pub.check_pending(pending, second)
        self.assertEqual(pending.read_text(), str(first.resolve())+'\n')

    def test_missing_or_corrupted_image_cannot_change_latest(self):
        old = self.bundle('old', version='4.2.9-1+pbsx1')
        self.active(old)
        new = self.bundle('new')
        (new/pub.IMAGE).write_bytes(b'damaged')
        with self.assertRaisesRegex(ValueError, 'digest/size'):
            pub.prepare(new, self.public, self.inspect)
        self.assertEqual((self.public/'iso/latest').readlink(), Path('releases/old'))
        (new/pub.IMAGE).unlink()
        with self.assertRaises(ValueError):pub.prepare(new, self.public, self.inspect)

    def test_support_only_update_requires_the_new_support_package_in_the_iso(self):
        old = self.bundle('old')
        new = self.bundle('new', repository_version='1.0.4')
        self.active(old)
        pub.prepare(new, self.public, self.inspect)
        with self.assertRaisesRegex(ValueError, 'stable APT changed'):
            pub.activate(new, self.public)
        self.assertEqual((self.public/'iso/latest').readlink(), Path('releases/old'))
        self.stable(new)
        pub.activate(new, self.public)
        self.assertEqual(pub.read_json(self.public/'iso/latest/iso-publication.json')['repository_version'], '1.0.4')

    def test_corrupt_or_changed_package_cannot_pass_preflight(self):
        bundle = self.bundle()
        path = next(bundle.glob('*.deb'))
        path.write_bytes(path.read_bytes()+b'changed')
        with self.assertRaises((ValueError, subprocess.CalledProcessError)):
            pub.prepare(bundle, self.public, self.inspect)
        self.assertFalse((self.public/'iso/latest').exists())

    def test_stale_sidecar_cannot_describe_another_embedded_image(self):
        bundle = self.bundle()
        embedded = copy.deepcopy(self.inspect(bundle/pub.IMAGE))
        embedded['version'] = '4.2.9-1+pbsx1'
        with self.assertRaisesRegex(ValueError, 'embedded ISO manifest'):
            pub.prepare(bundle, self.public, lambda _: embedded)

    def test_dirty_or_wrong_builder_is_rejected(self):
        bundle = self.bundle()
        manifest = self.inspect(bundle/pub.IMAGE)
        manifest['builder_dirty'] = True
        (bundle/'pbsx-offline-amd64.manifest.json').write_text(json.dumps(manifest))
        with self.assertRaisesRegex(ValueError, 'build/offline properties'):
            pub.prepare(bundle, self.public, self.inspect)

    def test_immutable_release_cannot_be_replaced(self):
        bundle = self.bundle()
        self.active(bundle)
        before = (self.public/'iso/latest'/pub.IMAGE).read_bytes()
        manifest = pub.read_json(bundle/'iso-publication.json')
        manifest['ci_run_id'] = 99
        (bundle/'iso-publication.json').write_text(json.dumps(manifest))
        with self.assertRaisesRegex(ValueError, 'immutable'):
            pub.prepare(bundle, self.public, self.inspect)
        self.assertEqual((self.public/'iso/latest'/pub.IMAGE).read_bytes(), before)

    def test_retry_is_idempotent_and_preserves_old_images(self):
        old = self.bundle('old', version='4.2.9-1+pbsx1')
        self.active(old)
        new = self.bundle('new')
        self.active(new)
        inode = (self.public/'iso/latest'/pub.IMAGE).stat().st_ino
        self.active(new)
        self.assertEqual((self.public/'iso/latest'/pub.IMAGE).stat().st_ino, inode)
        self.assertTrue((self.public/'iso/releases/old'/pub.IMAGE).is_file())

    def test_older_versions_and_older_same_package_builds_are_rejected(self):
        current = self.bundle('current', created='2000-01-02T00:00:00+00:00')
        self.active(current)
        old = self.bundle('old', version='4.2.9-1+pbsx1')
        with self.assertRaisesRegex(ValueError, 'older ISO package'):
            pub.prepare(old, self.public, self.inspect)
        earlier = self.bundle('earlier')
        self.assertEqual(pub.package_set(pub.read_json(current/'iso-inputs.json')['packages']), pub.package_set(pub.read_json(earlier/'iso-inputs.json')['packages']))
        with self.assertRaisesRegex(ValueError, 'older ISO build'):
            pub.prepare(earlier, self.public, self.inspect)

    def test_same_version_rebuild_is_rejected_before_public_staging(self):
        current = self.bundle('current');self.active(current)
        rebuilt = self.bundle('rebuilt', payload='changed executable bytes')
        with self.assertRaisesRegex(ValueError, 'same package version'):
            pub.prepare(rebuilt, self.public, self.inspect)
        self.assertEqual((self.public/'iso/latest').readlink(), Path('releases/current'))
        self.assertFalse((self.public/'iso/releases/rebuilt').exists())

    def test_initial_iso_cannot_downgrade_existing_stable_apt(self):
        installed = self.bundle('installed', version='4.2.9-1+pbsx3');self.stable(installed)
        older = self.bundle('older')
        with self.assertRaisesRegex(ValueError, 'older ISO package'):
            pub.prepare(older, self.public, self.inspect)
        self.assertFalse((self.public/'iso').exists())

    def test_initial_iso_cannot_replace_same_version_apt_bytes(self):
        installed = self.bundle('installed');self.stable(installed)
        rebuilt = self.bundle('rebuilt', payload='changed bytes')
        with self.assertRaisesRegex(ValueError, 'same package version'):
            pub.prepare(rebuilt, self.public, self.inspect)
        self.assertFalse((self.public/'iso').exists())

    def test_equivalent_debian_version_spellings_cannot_replace_package_bytes(self):
        installed = self.bundle('installed');self.stable(installed)
        rebuilt = self.bundle('rebuilt', repository_version='1.0.3-0')
        with self.assertRaisesRegex(ValueError, 'same package version'):
            pub.prepare(rebuilt, self.public, self.inspect)
        self.assertFalse((self.public/'iso').exists())

    def test_invalid_published_version_cannot_bypass_upgrade_checks(self):
        installed = self.bundle('installed');self.stable(installed)
        index = self.public/'apt/dists/trixie/main/binary-amd64/Packages.gz'
        damaged = gzip.decompress(index.read_bytes()).decode().replace('Version: 1.0.3\n', 'Version: invalid version\n')
        index.write_bytes(gzip.compress(damaged.encode()))
        with self.assertRaisesRegex(ValueError, 'compare published package versions'):
            pub.prepare(installed, self.public, self.inspect)
        self.assertFalse((self.public/'iso').exists())

    def test_first_full_release_accepts_a_partial_stable_repository(self):
        bundle = self.bundle();self.stable(bundle)
        index = self.public/'apt/dists/trixie/main/binary-amd64/Packages.gz'
        partial = [block for block in gzip.decompress(index.read_bytes()).decode().split('\n\n')
                   if block.startswith('Package: pbsx-repository\n')]
        index.write_bytes(gzip.compress(('\n\n'.join(partial)+'\n\n').encode()))
        self.active(bundle)
        self.assertEqual((self.public/'iso/latest').readlink(), Path('releases/release'))

    def test_symlink_escape_and_path_traversal_are_rejected(self):
        bundle = self.bundle()
        outside = self.root/'outside';outside.mkdir()
        (self.public/'iso').symlink_to(outside)
        with self.assertRaisesRegex(ValueError, 'unsafe ISO publication'):
            pub.prepare(bundle, self.public, self.inspect)
        self.assertEqual(list(outside.iterdir()), [])
        (self.public/'iso').unlink()
        data = pub.read_json(bundle/'iso-publication.json');data['release_id'] = '../outside'
        (bundle/'iso-publication.json').write_text(json.dumps(data))
        with self.assertRaisesRegex(ValueError, 'identity'):
            pub.prepare(bundle, self.public, self.inspect)
        with self.assertRaisesRegex(ValueError, 'identity'):
            pub.activate(bundle, self.public)

    def test_failed_link_switch_keeps_the_previous_image_and_allows_retry(self):
        old = self.bundle('old', version='4.2.9-1+pbsx1');self.active(old)
        new = self.bundle('new');pub.prepare(new, self.public, self.inspect);self.stable(new)
        replace = os.replace
        def fail_link(source, target):
            if Path(target) == self.public/'iso/latest':raise OSError('simulated rename failure')
            return replace(source, target)
        with patch.object(pub.os, 'replace', side_effect=fail_link):
            with self.assertRaises(OSError):pub.activate(new, self.public)
        self.assertEqual((self.public/'iso/latest').readlink(), Path('releases/old'))
        self.assertEqual(list((self.public/'iso').glob('.latest-*')), [])
        pub.activate(new, self.public)
        self.assertEqual((self.public/'iso/latest').readlink(), Path('releases/new'))

    def test_publication_needs_prepared_image_and_exact_live_apt_set(self):
        bundle = self.bundle();self.stable(bundle)
        with self.assertRaises(ValueError):pub.activate(bundle, self.public)
        pub.prepare(bundle, self.public, self.inspect)
        changed = self.bundle('changed', repository_version='1.0.4');self.stable(changed)
        with self.assertRaisesRegex(ValueError, 'stable APT changed'):pub.activate(bundle, self.public)

    def test_navigation_symlink_escape_is_rejected_before_preparing_an_iso(self):
        bundle = self.bundle()
        downloads = self.public/'downloads';downloads.mkdir()
        (downloads/'latest').symlink_to('../../outside')
        with self.assertRaisesRegex(ValueError, 'navigation link'):
            pub.prepare(bundle, self.public, self.inspect)
        self.assertFalse((self.public/'iso').exists())

    def test_generated_app_page_tracks_latest_iso_without_changing_app_assets(self):
        bundle = self.bundle()
        apps = self.public/'downloads/releases/apps';apps.mkdir(parents=True)
        (apps/'index.html').write_text('<h1>Apps</h1><a href="/iso/old.iso">Server ISO</a>')
        (apps/'app.zip').write_bytes(b'immutable app payload')
        (apps.parent.parent/'latest').symlink_to('releases/apps')
        self.active(bundle)
        self.assertIn(pub.LATEST_URL, (apps/'index.html').read_text())
        self.assertEqual((apps/'app.zip').read_bytes(), b'immutable app payload')

    def test_hosted_fetch_uses_exact_mirrored_release_and_validates_real_debs(self):
        bundle = self.bundle()
        output = self.root/'download'
        original = fetcher.iso.run
        requests = []
        def fake_gh(args):
            if args[0] != 'gh':return original(args)
            self.assertEqual(args[3], 'release')
            requests.append(args)
            destination = Path(args[args.index('--dir')+1])
            for i, arg in enumerate(args):
                if arg == '--pattern':shutil.copy2(bundle/args[i+1], destination/args[i+1])
        with patch.object(fetcher.iso, 'run', side_effect=fake_gh):
            fetcher.fetch(output, '4.2.9-1+pbsx2', 'release')
        self.assertEqual(len(requests), 2)
        self.assertEqual(pub.package_set(pub.read_json(output/'iso-inputs.json')['packages']),
                         pub.package_set(pub.read_json(bundle/'iso-inputs.json')['packages']))
        self.assertEqual(len(list(output.glob('*.deb'))), 4)

    def test_hosted_fetch_rejects_a_manifest_pointing_to_another_repository(self):
        bundle = self.bundle()
        inputs = pub.read_json(bundle/'iso-inputs.json');inputs['repository'] = 'attacker/repo'
        (bundle/'iso-inputs.json').write_text(json.dumps(inputs))
        def fake_gh(args):
            destination = Path(args[args.index('--dir')+1])
            shutil.copy2(bundle/'iso-inputs.json', destination/'iso-inputs.json')
        with patch.object(fetcher.iso, 'run', side_effect=fake_gh):
            with self.assertRaisesRegex(ValueError, 'identity mismatch'):
                fetcher.fetch(self.root/'download', None, 'release')


class HostedBuildChecks(unittest.TestCase):
    def test_wrong_source_repository_workflow_or_conclusion_is_rejected(self):
        good = {'conclusion': 'success', 'path': '.github/workflows/iso.yml', 'event': 'workflow_dispatch',
                'head_sha': 'a'*40, 'head_repository': {'full_name': 'org/repo'}}
        self.assertEqual(publisher.checked_run(good, 'org/repo', 'a'*40), good)
        for key, value in [('conclusion', 'failure'), ('path', '.github/workflows/other.yml'),
                           ('event', 'pull_request'), ('head_sha', 'b'*40), ('head_repository', {'full_name': 'attacker/repo'})]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                publisher.checked_run({**good, key: value}, 'org/repo', 'a'*40)

    def test_completed_github_assets_are_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'image.iso';path.write_bytes(b'fixture')
            asset = {'id': 1, 'name': path.name, 'state': 'uploaded', 'size': path.stat().st_size, 'digest': 'sha256:'+'0'*64}
            with patch.object(publisher, 'release_info', return_value={'assets': [asset]}), patch.object(publisher, 'run') as run:
                with self.assertRaisesRegex(ValueError, 'completed GitHub asset'):
                    publisher.ensure_assets('org/repo', 'tag', [path])
                run.assert_not_called()


class MirroredISOCollection(unittest.TestCase):
    def test_checked_mirror_download_and_corruption_rejection(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);destination=root/'download'
            content=b'synthetic ISO transport fixture'
            assets=[{'name':name,'state':'uploaded','size':len(content),'digest':'sha256:'+hashlib.sha256(content).hexdigest()}
                    for name in pub.PUBLIC_FILES[:4]]
            info={'target_commitish':'a'*40,'draft':False,'assets':assets}
            def download(args):
                name=args[args.index('--pattern')+1]
                (Path(args[args.index('--dir')+1])/name).write_bytes(content)
            with patch.object(publisher,'release_info',return_value=info),patch.object(publisher,'run',side_effect=download):
                publisher.download_iso_assets('org/repo','tag','a'*40,destination)
            self.assertEqual({p.name for p in destination.iterdir()},set(pub.PUBLIC_FILES[:4]))
            shutil.rmtree(destination)
            def corrupt(args):
                name=args[args.index('--pattern')+1]
                (Path(args[args.index('--dir')+1])/name).write_bytes(b'corrupt')
            with patch.object(publisher,'release_info',return_value=info),patch.object(publisher,'run',side_effect=corrupt):
                with self.assertRaisesRegex(ValueError,'hash mismatch'):
                    publisher.download_iso_assets('org/repo','tag','a'*40,destination)

    def test_wrong_source_draft_or_missing_asset_does_not_collect(self):
        with tempfile.TemporaryDirectory() as directory:
            for info in [None,{'target_commitish':'b'*40,'draft':False},
                    {'target_commitish':'a'*40,'draft':True},
                    {'target_commitish':'a'*40,'draft':False,'assets':[]}]:
                with self.subTest(info=info),patch.object(publisher,'release_info',return_value=info),patch.object(publisher,'run') as download:
                    with self.assertRaises(ValueError):
                        publisher.download_iso_assets('org/repo','tag','a'*40,Path(directory)/'download')
                    download.assert_not_called()


if __name__ == '__main__':unittest.main()
