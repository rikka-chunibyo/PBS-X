#!/usr/bin/env python3
import importlib.util
import json
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch
import zipfile

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('transport',ROOT/'tools/host_release_transfer.py')
t=importlib.util.module_from_spec(spec);spec.loader.exec_module(t)

class ReleaseTransfer(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=Path(self.temp.name)
    def archive(self,extra=None,symlink=False):
        path=self.root/'bundle.zip'
        with zipfile.ZipFile(path,'w') as archive:
            for name in sorted(t.names('linux')):
                info=zipfile.ZipInfo(name)
                if symlink and name=='pbsx-agent':info.external_attr=(stat.S_IFLNK|0o777)<<16
                archive.writestr(info,'fixture')
            if extra:archive.writestr(extra,'outside')
        return path
    def test_extract_allowlists_native_payloads(self):
        output=self.root/'output';t.checked_extract(self.archive(),output,'linux')
        self.assertEqual({p.name for p in output.iterdir()},t.names('linux'))
    def test_paths_symlinks_and_reused_destination_are_rejected(self):
        for extra,link in [('../outside',False),('private-key',False),(None,True)]:
            with self.subTest(extra=extra,link=link),self.assertRaises(ValueError):
                t.checked_extract(self.archive(extra,link),self.root/'output','linux')
        self.assertFalse((self.root/'outside').exists())
        output=self.root/'existing';output.mkdir()
        with self.assertRaises(ValueError):t.checked_extract(self.archive(),output,'linux')
    def test_only_dedicated_build_tags_and_original_draft_can_be_written(self):
        for tag in ['pbsx-clients-public','../elsewhere','pbsx-client-build-']:
            with self.assertRaises(ValueError):t.validate_tag(tag)
        for release in [{'draft':False,'target_commitish':'a'*40},{'draft':True,'target_commitish':'b'*40}]:
            with self.assertRaises(ValueError):t.checked_release(release,'a'*40,writing=True)
    def test_conflicting_completed_asset_cannot_be_replaced(self):
        path=self.root/'bundle';path.write_bytes(b'new')
        info={'draft':True,'target_commitish':'a'*40,'assets':[{'name':'bundle','state':'uploaded','size':3,'digest':'sha256:'+'0'*64}]}
        with patch.object(t,'release',return_value=info),patch.object(t.subprocess,'run') as upload:
            with self.assertRaises(ValueError):t.upload('org/repo','pbsx-client-build-fixture',path,'a'*40)
            upload.assert_not_called()
    def test_mirror_refuses_automatic_pushes(self):
        with patch.dict(t.os.environ,{'GITHUB_EVENT_NAME':'push'}):
            with self.assertRaisesRegex(ValueError,'dispatch'):t.mirror(self.root,'linux','pbsx-client-build-fixture')
    def test_collection_requires_same_run_attempt_source_and_payload_hash(self):
        # Reject a forged provenance before any archive extraction.
        archive=self.archive();archive_name,proof_name=t.asset_names('linux')
        proof={'schema':1,'repository':'org/repo','commit':'a'*40,'run_id':999,'run_attempt':1,'system':'linux','sha256':t.digest(archive),'size':archive.stat().st_size,'version':'0.7.6-preview'}
        proof_file=self.root/proof_name;proof_file.write_text(json.dumps(proof))
        originals={archive_name:archive,proof_name:proof_file}
        assets=[{'name':name,'state':'uploaded','size':p.stat().st_size,'digest':'sha256:'+t.digest(p)} for name,p in originals.items()]
        info={'target_commitish':'a'*40,'assets':assets}
        run_info={'event':'workflow_dispatch','head_sha':'a'*40,'id':123,'run_attempt':1}
        def download(args,check):
            name=args[args.index('--pattern')+1];dest=Path(args[args.index('--dir')+1]);(dest/name).write_bytes(originals[name].read_bytes())
        with patch.object(t,'release',return_value=info),patch.object(t.subprocess,'run',side_effect=download):
            with self.assertRaisesRegex(ValueError,'provenance'):t.collect('org/repo','pbsx-client-build-fixture',run_info,'linux',self.root/'output')
        self.assertFalse((self.root/'output').exists())

if __name__=='__main__':unittest.main()
