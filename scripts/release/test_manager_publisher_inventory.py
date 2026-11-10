"""Authoritative CLI evidence and isolated remote dependency closure regressions."""
import copy
import datetime as dt
import json
import subprocess
import sys
import unittest
from unittest.mock import Mock, patch

import promote_manager_release as promoter
with patch.dict(sys.modules, {'paramiko': Mock()}):
    import publish_manager_release as publisher


class ModuleClosureTests(unittest.TestCase):
    def test_remote_program_loads_without_repository_import_path(self):
        program = publisher.remote_program({}).rsplit('module.promote(', 1)[0]
        result = subprocess.run([sys.executable, '-I', '-c', program],
                                capture_output=True, text=True, timeout=20)
        self.assertEqual(result.returncode, 0, result.stderr)


class LocalInventoryTests(unittest.TestCase):
    def setUp(self):
        self.target = dict(manager_id='m-fixture', main='main', machine_id='box',
                           dir='/fixture/manager', binary='/fixture/manager/bin/personad',
                           unit='com.persona.manager.m-fixture',
                           plist_path='/fixture/manager.plist', uid=501,
                           previous_sha256='a' * 64, config_sha256='b' * 64,
                           plist_sha256='c' * 64)
        self.identity = dict(machines=[('box', 'uid')])
        now = dt.datetime.now(dt.timezone.utc).isoformat()
        self.evidence = dict(schema=1, complete=True, idle=True, complete_reads=2,
                             sha256='d' * 64, started_at=now, completed_at=now,
                             authority=dict(manager='m-fixture', main='main',
                                            machine_id='box', revision='e' * 64),
                             inventory=dict(target=dict(manager='m-fixture', main='main',
                                                        machine_id='box', machine_uid='uid',
                                                        authority_revision='e' * 64)))

    def read(self):
        return promoter.idle_evidence(['ssh', 'example.invalid'], self.target,
                                      self.identity, '/fixture/candidate')

    def test_reader_gets_only_identity_and_exact_scope_through_stdin(self):
        with patch.object(promoter, 'remote', return_value=self.evidence) as remote:
            self.assertEqual(self.read(), ('d' * 64, dt.datetime.fromisoformat(self.evidence['completed_at'])))
        args, kwargs = remote.call_args
        self.assertEqual(args[1][:6],
                         ['/fixture/candidate', 'manager', 'update', 'inventory', '--plan', '-'])
        plan = json.loads(args[2])
        self.assertEqual(plan['manager_id'], 'm-fixture')
        for field in ('idle_snapshot_sha256', 'run_id', 'source', 'activation_enabled'):
            self.assertNotIn(field, plan)
        self.assertEqual(kwargs['timeout'], 45)

    def test_go_nanosecond_observations_parse_with_z_or_explicit_offset(self):
        for suffix in ('Z', '+00:00'):
            with self.subTest(suffix=suffix):
                result = promoter.observation_time('2026-01-01T00:00:00.123456789' + suffix)
                self.assertEqual(result.microsecond, 123456)
                self.assertEqual(result.utcoffset(), dt.timedelta(0))
        self.assertEqual(promoter.observation_time('2026-01-01T00:00:00.1+02:00').microsecond,
                         100000)

    def test_busy_incomplete_missing_reads_or_digest_fail_closed(self):
        for change in (dict(idle=False), dict(complete=False), dict(schema=2),
                       dict(complete_reads=1), dict(complete_reads=True),
                       dict(sha256=''), dict(sha256='0' * 63)):
            with self.subTest(change=change), patch.object(
                    promoter, 'remote', return_value=dict(self.evidence, **change)):
                with self.assertRaises(ValueError):
                    self.read()

    def test_wrong_authority_or_runtime_target_fail_closed(self):
        for section, field in (('authority', 'main'), ('authority', 'manager'),
                               ('authority', 'machine_id'), ('inventory', 'main'),
                               ('inventory', 'machine_uid'), ('inventory', 'authority_revision')):
            response = copy.deepcopy(self.evidence)
            source = response[section] if section == 'authority' else response['inventory']['target']
            source[field] = 'foreign'
            with self.subTest(section=section, field=field), patch.object(
                    promoter, 'remote', return_value=response), self.assertRaises(ValueError):
                self.read()

    def test_stale_future_slow_and_malformed_observations_fail_closed(self):
        now = dt.datetime.now(dt.timezone.utc)
        for change in (dict(completed_at=(now - dt.timedelta(seconds=31)).isoformat()),
                       dict(completed_at=(now + dt.timedelta(seconds=6)).isoformat()),
                       dict(started_at=(now - dt.timedelta(seconds=31)).isoformat()),
                       dict(completed_at='unknown'), dict(completed_at=now.replace(tzinfo=None).isoformat())):
            with self.subTest(change=change), patch.object(
                    promoter, 'remote', return_value=dict(self.evidence, **change)):
                with self.assertRaises(ValueError):
                    self.read()
        with patch.object(promoter, 'remote', return_value=self.evidence), patch.object(
                promoter.time, 'monotonic', side_effect=[0, 30]), self.assertRaises(TimeoutError):
            self.read()

    def test_transport_or_runtime_error_is_not_turned_into_idle(self):
        with patch.object(promoter, 'remote', side_effect=RuntimeError('runtime busy')):
            with self.assertRaisesRegex(RuntimeError, 'runtime busy'):
                self.read()

    def test_multiple_owned_machines_are_not_silently_omitted(self):
        self.identity['machines'].append(('other', 'other-uid'))
        with patch.object(promoter, 'remote') as remote, self.assertRaises(ValueError):
            self.read()
        remote.assert_not_called()


if __name__ == '__main__':
    unittest.main()
