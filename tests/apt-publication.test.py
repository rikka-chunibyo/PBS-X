#!/usr/bin/env python3
"""Exercise the actual read-only APT publication validator with tiny Debian packages."""
import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
CORE = ('proxmox-backup-server', 'proxmox-backup-client', 'proxmox-backup-file-restore')


class AptPublicationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bundle = self.root/'bundle'
        self.bundle.mkdir()

    def package(self, name, version='4.2.9-1+pbsx1', arch='amd64', filename=None):
        work = self.root/('source-'+str(len(list(self.root.iterdir()))))
        (work/'DEBIAN').mkdir(parents=True)
        (work/'DEBIAN/control').write_text(f'Package: {name}\nVersion: {version}\nArchitecture: {arch}\nMaintainer: Test <test@example.invalid>\nDescription: test fixture\n')
        target = self.bundle/(filename or f'{name}_{version}_{arch}.deb')
        subprocess.run(['dpkg-deb','--build','--root-owner-group','--threads-max=1',work,target],check=True,capture_output=True)
        return target

    def core(self):
        for name in CORE:
            self.package(name)

    def check(self, success=True, message='', omit=()):
        (self.bundle/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.name+'\n' for p in sorted(self.bundle.glob('*.deb')) if p.name not in omit))
        result = subprocess.run(['bash',ROOT/'tools/pbsx-apt-repository.sh','--check-release',self.bundle],text=True,capture_output=True)
        self.assertEqual(result.returncode == 0, success, result.stdout+result.stderr)
        if message:
            self.assertIn(message, result.stderr)

    def test_matching_core_set(self):
        self.core()
        self.check()

    def test_core_and_repository_support(self):
        self.core()
        self.package('pbsx-repository','1.0.3','all')
        self.check()

    def test_repository_only_update_remains_allowed(self):
        self.package('pbsx-repository','1.0.4','all')
        self.check()

    def test_partial_core_set_rejected(self):
        self.package(CORE[0])
        self.check(False,'all three matching')

    def test_mixed_core_versions_rejected(self):
        self.package(CORE[0],'4.2.10-1+pbsx1')
        for name in CORE[1:]: self.package(name)
        self.check(False,'versions must match')

    def test_duplicate_package_rejected_even_at_same_version(self):
        self.core()
        self.package(CORE[0],filename='duplicate.deb')
        self.check(False,'Duplicate release package')

    def test_upstream_package_rejected(self):
        self.package(CORE[0],'4.2.10-1')
        self.check(False,'Invalid PBS-X executable')

    def test_wrong_architecture_rejected(self):
        self.package(CORE[0],arch='arm64')
        self.check(False,'Invalid PBS-X executable')

    def test_uncovered_package_rejected(self):
        self.core()
        omit = next(self.bundle.glob('*.deb')).name
        self.check(False,'not covered',omit=(omit,))

    def test_unrelated_package_rejected(self):
        self.package('unrelated-package','1.0')
        self.check(False,'Not an allowed')

    def test_empty_bundle_rejected(self):
        self.check(False)


if __name__ == '__main__': unittest.main()
