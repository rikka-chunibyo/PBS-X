#!/usr/bin/env python3
import importlib.util
from pathlib import Path
import stat
import tempfile
import unittest
import zipfile

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('sources',ROOT/'tools/fetch-host-app-sources.py')
sources=importlib.util.module_from_spec(spec);spec.loader.exec_module(sources)
SHA='a'*40

class PortableSources(unittest.TestCase):
    def setUp(self):
        self.work=tempfile.TemporaryDirectory();self.addCleanup(self.work.cleanup)
        self.root=Path(self.work.name);self.archive=self.root/'source.zip';self.out=self.root/'out'
    def zip(self,extra=None,comment=SHA):
        with zipfile.ZipFile(self.archive,'w') as z:
            z.comment=comment.encode()
            for name in ['.gitignore','tools/build-host-app.py','pbsx-host-agent/go.mod','pbsx-host-agent/third_party/pxar/go.mod']:
                z.writestr('repo-root/'+name,'fixture bytes\n')
            # The server tree contains names invalid on Windows. They must never
            # be materialized merely to obtain the portable client tree.
            z.writestr('repo-root/tests/server/foo:bar','excluded')
            if extra:
                name,data,mode=extra
                info=zipfile.ZipInfo('repo-root/'+name);info.external_attr=mode<<16
                z.writestr(info,data)
    def test_exact_commit_extracts_only_portable_files(self):
        self.zip();sources.extract(self.archive,self.out,SHA)
        self.assertEqual((self.out/'.repoid').read_text(),SHA+'\n')
        self.assertEqual((self.out/'pbsx-host-agent/go.mod').read_text(),'fixture bytes\n')
        self.assertFalse((self.out/'tests').exists())
    def test_wrong_commit_is_rejected_before_writes(self):
        self.zip(comment='b'*40)
        with self.assertRaisesRegex(ValueError,'commit'):sources.extract(self.archive,self.out,SHA)
        self.assertFalse(self.out.exists())
    def test_traversal_symlinks_and_windows_collisions_are_rejected(self):
        for name,mode in [('pbsx-host-agent/../../outside',stat.S_IFREG),('pbsx-host-agent/link',stat.S_IFLNK),('pbsx-host-agent/CON.txt',stat.S_IFREG),('pbsx-host-agent/foo:bar',stat.S_IFREG),('pbsx-host-agent/GO.MOD',stat.S_IFREG)]:
            with self.subTest(name=name):
                self.zip((name,'outside',mode))
                with self.assertRaises(ValueError):sources.extract(self.archive,self.out,SHA)
                self.assertFalse(self.out.exists())
    def test_existing_checkout_is_not_overwritten(self):
        self.zip();self.out.mkdir();(self.out/'.gitignore').write_text('keep')
        with self.assertRaisesRegex(ValueError,'exists'):sources.extract(self.archive,self.out,SHA)
        self.assertEqual((self.out/'.gitignore').read_text(),'keep')
    def test_redirect_does_not_forward_the_api_token(self):
        req=sources.urllib.request.Request('https://api.github.com/repos/owner/repo/zipball/'+SHA,headers={'Authorization':'Bearer synthetic-secret'})
        redirected=sources.ArchiveRedirects().redirect_request(req,None,302,'Found',{},'https://codeload.github.com/owner/repo/legacy.zip/'+SHA)
        self.assertIsNone(redirected.get_header('Authorization'))
        with self.assertRaisesRegex(ValueError,'redirect'):
            sources.ArchiveRedirects().redirect_request(req,None,302,'Found',{},'https://unrelated.invalid/file')

if __name__=='__main__':unittest.main()
