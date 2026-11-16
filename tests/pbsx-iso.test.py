#!/usr/bin/env python3
import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'tools'))
import pbsx_iso as iso
spec = importlib.util.spec_from_file_location('configure_target', ROOT/'packaging/iso/configure-target.py')
configure_target = importlib.util.module_from_spec(spec)
spec.loader.exec_module(configure_target)


class RepositoryDefaults(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.sources = self.root/'etc/apt/sources.list.d'
        self.sources.mkdir(parents=True)
        (self.root/'proxmox_install_mode').touch()

    def test_offline_defaults_disable_only_enterprise_and_keep_debian(self):
        debian = 'Types: deb\nURIs: http://deb.debian.org/debian\nSuites: trixie\nComponents: main\n'
        (self.sources/'debian.sources').write_text(debian)
        (self.sources/'pbs-enterprise.sources').write_text('Types: deb\nURIs: https://enterprise.proxmox.com/debian/pbs\nSuites: trixie\nComponents: pbs-enterprise\n')
        configure_target.configure(self.root)
        self.assertEqual((self.sources/'debian.sources').read_text(), debian)
        self.assertIn('Enabled: no', (self.sources/'pbs-enterprise.sources').read_text())
        self.assertEqual((self.sources/'pbsx-system.sources').read_text(), configure_target.SYSTEM_SOURCE)

    def test_later_user_choice_is_not_overwritten(self):
        configure_target.configure(self.root)
        source=self.sources/'pbsx-system.sources'
        source.write_text('# administrator chose another source\n')
        configure_target.configure(self.root)
        self.assertEqual(source.read_text(), '# administrator chose another source\n')

    def test_existing_community_source_is_not_duplicated(self):
        (self.sources/'community.list').write_text('deb http://download.proxmox.com/debian/pbs trixie pbs-no-subscription\n')
        configure_target.configure(self.root)
        self.assertFalse((self.sources/'pbsx-system.sources').exists())

    def test_regular_install_does_not_reset_sources(self):
        (self.root/'proxmox_install_mode').unlink()
        configure_target.configure(self.root)
        self.assertFalse((self.sources/'pbsx-system.sources').exists())

    def test_symlinked_sources_are_rejected_without_touching_target(self):
        other=self.root/'other'
        other.write_text('leave unchanged')
        (self.sources/'unsafe.sources').symlink_to(other)
        with self.assertRaises(ValueError): configure_target.configure(self.root)
        self.assertEqual(other.read_text(), 'leave unchanged')


class PackageChecks(unittest.TestCase):
    def test_deb822_round_trip_preserves_dependency_and_description_continuations(self):
        data='Package: example\nVersion: 1.0\nDepends: libone,\n libtwo\nDescription: example\n long description\n .\n another paragraph\n\n'
        fields=iso.records(data)[0]
        self.assertEqual(iso.records(iso.serialize(fields)), [fields])

    def test_missing_runtime_dependency_fails_offline_closure(self):
        fields={'Package':'pbsx-offline-probe','Version':'1.0','Architecture':'all','Depends':'missing-pbsx-runtime (>= 2)','Description':'test fixture'}
        with self.assertRaisesRegex(ValueError, 'offline dependency closure failed'):
            iso.check_closure('', {'pbsx-offline-probe':(Path('fixture.deb'),fields)})

    def test_old_runtime_dependency_fails_and_new_version_satisfies(self):
        fields={'Package':'pbsx-offline-probe','Version':'1.0','Architecture':'all','Depends':'pbsx-runtime (>= 2)','Description':'test fixture'}
        runtime={'Package':'pbsx-runtime','Version':'1.0','Architecture':'all','Description':'test fixture'}
        pool={'pbsx-offline-probe':(Path('fixture.deb'),fields),'pbsx-runtime':(Path('runtime.deb'),runtime)}
        with self.assertRaises(ValueError): iso.check_closure('',pool)
        runtime['Version']='2.0'
        iso.check_closure('',pool)

    def test_overlay_removes_upstream_package_and_normalizes_filename(self):
        with tempfile.TemporaryDirectory() as name:
            root=Path(name)
            pool=root/'pool';pool.mkdir()
            for version in ('4.2.0','4.2.9+pbsx1'):
                package=root/version
                (package/'DEBIAN').mkdir(parents=True)
                (package/'DEBIAN/control').write_text(f'Package: pbsx-fixture\nVersion: {version}\nArchitecture: all\nMaintainer: PBS-X <test@invalid>\nDescription: fixture\n')
                output=(pool/'old.deb') if version=='4.2.0' else root/'noncanonical-name.deb'
                subprocess.run(['dpkg-deb','--root-owner-group','--build',str(package),str(output)],check=True,capture_output=True)
            result=iso.overlay_packages(pool,[root/'noncanonical-name.deb'])
            self.assertEqual(list(pool.glob('*.deb')), [pool/'pbsx-fixture_4.2.9+pbsx1_all.deb'])
            self.assertEqual(result['pbsx-fixture'][1]['Version'],'4.2.9+pbsx1')


if __name__=='__main__': unittest.main()
