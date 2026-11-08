import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('guard', Path(__file__).with_name('apt-guard.py'))
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


class ProtectionTests(unittest.TestCase):
    def test_action_protocol_and_removal(self):
        stream = io.StringIO('VERSION 2\nAPT::Something=value\n\n'
                             'unrelated 1 < 2 /tmp/unrelated.deb\n'
                             'proxmox-backup-server 1 < 2 /tmp/server.deb\n'
                             'proxmox-backup-client 1 > - **REMOVE**\n')
        self.assertEqual(guard.actions(stream), {'proxmox-backup-server': '/tmp/server.deb',
                                                'proxmox-backup-client': '**REMOVE**'})

    def test_unknown_protocol_and_malformed_action_fail_closed(self):
        for value in ('VERSION 3\n', 'VERSION 2\n\ninvalid action\n'):
            with self.assertRaises(RuntimeError):
                guard.actions(io.StringIO(value))

    def test_local_package_filename_with_spaces(self):
        stream = io.StringIO('VERSION 2\n\nproxmox-backup-server 1 < 2 /tmp/local packages/server.deb\n')
        self.assertEqual(guard.actions(stream)['proxmox-backup-server'], '/tmp/local packages/server.deb')

    def test_running_restore_blocks_but_queued_backup_does_not(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(guard, 'active', return_value=False):
            root = Path(temporary)
            queued = root/'phone/queued/run.json'
            queued.parent.mkdir(parents=True)
            queued.write_text('{}')
            with patch.object(guard, 'RUNS', root):
                guard.check_idle()
                running = root/'phone/restore-running/run.json'
                running.parent.mkdir(parents=True)
                running.write_text('{}')
                with self.assertRaisesRegex(RuntimeError, 'backup/restore is running'):
                    guard.check_idle()

    def test_pbs_worker_blocks_update(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(guard, 'RUNS', Path(temporary)), \
                patch.object(guard, 'active', return_value=True), \
                patch.object(guard, 'run', return_value=subprocess.CompletedProcess([], 0, '[{"upid":"active"}]', '')):
            with self.assertRaisesRegex(RuntimeError, 'PBS tasks are running'):
                guard.check_idle()

    def test_archive_failure_restarts_original_services(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            state = root/'state.json'
            calls = []

            def run(args, check=True):
                calls.append(args)
                if args[0] == 'tar':
                    raise RuntimeError('archive disk full')
                return subprocess.CompletedProcess(args, 0, '', '')

            with patch.object(guard, 'STATE', state), patch.object(guard, 'CONFIG', root), \
                    patch.object(guard, 'RUNS', root/'runs'), patch.object(guard, 'BACKUPS', root), \
                    patch.object(guard, 'active', return_value=True), patch.object(guard, 'check_idle'), \
                    patch.object(guard, 'run', side_effect=run):
                with self.assertRaisesRegex(RuntimeError, 'disk full'):
                    guard.prepare({'proxmox-backup-client': '/tmp/client.deb'})
            self.assertFalse(state.exists())
            self.assertIn(['systemctl', 'start', *guard.UNITS], calls)

    def test_repair_retains_restart_intent_without_starting_broken_binary(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            state = root/'state.json'
            state.write_text(json.dumps({'version': 1, 'units': list(guard.UNITS), 'backup': '/old-backup'}))
            with patch.object(guard, 'STATE', state), patch.object(guard, 'CONFIG', root), \
                    patch.object(guard, 'RUNS', root/'runs'), patch.object(guard, 'BACKUPS', root), \
                    patch.object(guard, 'active', return_value=False), patch.object(guard, 'check_idle'), \
                    patch.object(guard, 'run', return_value=subprocess.CompletedProcess([], 0, '', '')) as run:
                guard.prepare({'proxmox-backup-client': '/tmp/client.deb'})
                self.assertEqual(json.loads(state.read_text())['units'], list(guard.UNITS))
                self.assertFalse(any(call.args[0][:2] == ['systemctl', 'start'] for call in run.call_args_list))

    def test_recovery_rejects_unknown_service(self):
        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary)/'state.json'
            state.write_text(json.dumps({'version': 1, 'units': ['unrelated.service'], 'backup': '/tmp/x'}))
            with patch.object(guard, 'STATE', state), self.assertRaisesRegex(RuntimeError, 'Invalid'):
                guard.resume()


if __name__ == '__main__':
    unittest.main()
