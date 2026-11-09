import base64
import contextlib
import hashlib
import io
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import node_artifacts
import promote_hub_release as promote
from test_node_artifacts import BUILD, health, native_result, release


class PromotionNodePackageTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        base = Path(self.temporary.name)
        self.root = base / 'runtime'
        self.staged = self.root / 'releases/hub/.upload-test'
        (self.root / 'bin').mkdir(parents=True)
        (self.root / 'hub.json').write_text(json.dumps({'auto_update_nodes': False}))
        self.node_manifest = release(self.staged)
        self.hub_manifest = copy.deepcopy(self.node_manifest)
        self.hub_manifest['files'] = {'linux-amd64': self.hub_manifest['files']['linux-amd64']}
        self.hub_manifest['ancestors'] = base64.b64encode(bytes(6)).decode()
        (self.staged / 'latest.json').write_text(json.dumps(self.hub_manifest))
        old_staged = base / 'old-staged'
        old = release(old_staged, '0' * 40, BUILD - 1)
        with patch.object(node_artifacts.subprocess, 'check_output', return_value=native_result(old)):
            node_artifacts.publish_nodes(self.root, old_staged, old, health(old))
        self.old_pointer = (self.root / 'bin/nodes').resolve()
        self.latest = self.root / 'releases/hub/latest.json'
        self.latest.parent.mkdir(parents=True, exist_ok=True)
        self.old_latest = json.dumps({'commit': old['commit'], 'build': old['build']}).encode()
        self.latest.write_bytes(self.old_latest)
        binary = self.root / 'bin/personad'
        archive = self.staged / self.hub_manifest['files']['linux-amd64']['name']
        import gzip
        binary.write_bytes(gzip.decompress(archive.read_bytes()))
        self.serving_hash = self.hub_manifest['files']['linux-amd64']['binary_sha256']
        self.calls = []
        self.events = []
        self.inventory = {'machines': [{'id': 'node', 'kind': 'link', 'online': True,
                                       'node_version': 'unchanged', 'deploying': False}]}

    def api(self, root, path, method='GET', body=None):
        self.assertEqual(root, self.root)
        self.calls.append((path, method))
        self.events.append(path)
        self.assertNotIn('/deploy', path)
        if path == 'machines':
            return copy.deepcopy(self.inventory)
        if path == 'hub/update':
            return {'auto': True, 'can_update': True, 'state': 'updated', 'confirmed': True,
                    'latest': {'build': BUILD - 1},
                    'auto_update_nodes': json.loads((root / 'hub.json').read_text())['auto_update_nodes']}
        if path == 'hub/update/settings':
            self.assertEqual(method, 'PUT')
            self.assertEqual(body, {'auto_update_nodes': True})
            self.assertEqual((root / 'bin/nodes').resolve().name, self.node_manifest['version'])
            return {'auto_update_nodes': True}
        if path == 'hub/update/check':
            return {'latest': {'bad': False}}
        if path == 'health':
            return health(self.hub_manifest)
        if path == 'hub/update/history':
            return {'history': []}
        raise AssertionError('Unexpected API path: ' + path)

    def native(self, argv, **kwargs):
        if argv[-1] == 'node-update-check':
            return native_result(self.node_manifest)
        self.assertIn('--status', argv)
        return 'slot a serving version, pid 1234\n'

    def execute(self, enabled=True, namespace='hub'):
        actual_digest = promote.digest
        def digest(path):
            if str(path) == '/proc/1234/exe':
                return self.serving_hash
            return actual_digest(path)
        with patch.object(promote, 'api', side_effect=self.api), \
                patch.object(promote, 'digest', side_effect=digest), \
                patch.object(promote.subprocess, 'check_output', side_effect=self.native), \
                contextlib.redirect_stdout(io.StringIO()):
            payload = {'root': str(self.root), 'staged': str(self.staged),
                       'release_namespace': namespace}
            if enabled is not None:
                payload['publish_nodes'] = enabled
            promote.promote(payload)

    def assert_unchanged_and_no_update_requested(self):
        self.assertEqual(self.latest.read_bytes(), self.old_latest)
        self.assertEqual((self.root / 'bin/nodes').resolve(), self.old_pointer)
        self.assertFalse(any(method == 'POST' for _, method in self.calls))

    def test_named_feed_preserves_previous_feed_and_uses_shared_lock(self):
        import os
        original_latest = self.latest
        named_latest = self.root / 'releases/hub-canonical/latest.json'
        named_latest.parent.mkdir()
        named_latest.write_bytes(self.old_latest)
        named_staged = named_latest.parent / self.staged.name
        self.staged.rename(named_staged)
        self.staged = named_staged
        common_lock = self.root / 'releases/hub/.publish.lock'
        actual_flock = promote.fcntl.flock
        def shared_lock(stream, operation):
            self.assertEqual(os.fstat(stream.fileno()).st_ino, common_lock.stat().st_ino)
            return actual_flock(stream, operation)
        with patch.object(promote.fcntl, 'flock', side_effect=shared_lock):
            self.execute(namespace='hub-canonical')
        self.assertEqual(original_latest.read_bytes(), self.old_latest)
        self.assertEqual(json.loads(named_latest.read_text())['commit'], self.hub_manifest['commit'])
        self.assertFalse((named_latest.parent / '.publish.lock').exists())

    def test_prepare_failure_preserves_hub_latest_and_node_pointer(self):
        with patch.object(promote, 'prepare_nodes', side_effect=ValueError('Bad Darwin package')):
            with self.assertRaisesRegex(ValueError, 'Bad Darwin'):
                self.execute()
        self.assert_unchanged_and_no_update_requested()
        self.assertTrue((self.staged / 'latest.json').exists())

    def test_real_bad_foreign_archive_fails_before_hub_update(self):
        artifact = self.node_manifest['files']['darwin-arm64']
        (self.staged / artifact['name']).write_bytes(b'bad foreign archive')
        with self.assertRaises(ValueError):
            self.execute()
        self.assert_unchanged_and_no_update_requested()
        self.assertFalse((self.root / 'releases/nodes' / self.node_manifest['version']).exists())

    def test_reupload_after_preflight_failure_cannot_mutate_published_artifact(self):
        name = self.hub_manifest['files']['linux-amd64']['name']
        published = self.root / 'releases/hub' / name
        with patch.object(promote, 'prepare_nodes', side_effect=ValueError('Bad Darwin package')):
            with self.assertRaises(ValueError):
                self.execute()
        original_hash = hashlib.sha256(published.read_bytes()).hexdigest()
        original_inode = published.stat().st_ino
        source = self.staged / name
        self.assertNotEqual(source.stat().st_ino, original_inode)
        source.write_bytes(b'a partial reupload at the staging path')
        self.assertEqual(hashlib.sha256(published.read_bytes()).hexdigest(), original_hash)
        self.assertEqual(published.stat().st_ino, original_inode)
        self.assert_unchanged_and_no_update_requested()

    def test_serving_process_mismatch_preserves_old_node_pointer(self):
        self.serving_hash = 'f' * 64
        with self.assertRaisesRegex(RuntimeError, 'Serving process checksum mismatch'):
            self.execute()
        self.assertEqual((self.root / 'bin/nodes').resolve(), self.old_pointer)
        self.assertEqual([path for path, method in self.calls if method == 'POST'], ['hub/update/check'])

    def test_missing_opted_in_manifest_cannot_publish_hub(self):
        (self.staged / 'node-manifest.json').unlink()
        with self.assertRaisesRegex(ValueError, 'required'):
            self.execute()
        self.assert_unchanged_and_no_update_requested()

    def test_default_off_ignores_nodes_and_never_calls_prepare_or_activate(self):
        (self.staged / 'node-manifest.json').write_text('invalid ignored metadata')
        with patch.object(promote, 'prepare_nodes') as prepare, \
                patch.object(promote, 'activate_nodes') as activate, \
                patch.object(promote, 'node_manifest') as metadata:
            self.execute(enabled=None)
        prepare.assert_not_called()
        activate.assert_not_called()
        metadata.assert_not_called()
        self.assertEqual((self.root / 'bin/nodes').resolve(), self.old_pointer)
        self.assertEqual([path for path, method in self.calls if method == 'POST'], ['hub/update/check'])
        self.assertEqual(json.loads(self.latest.read_text())['commit'], self.hub_manifest['commit'])

    def test_matching_packages_activate_only_after_confirmed_hub_without_deploy(self):
        actual_activate = promote.activate_nodes
        def activate(*args):
            self.assertIn('health', self.events)
            self.assertIn(('hub/update/check', 'POST'), self.calls)
            self.assertEqual((self.root / 'bin/nodes').resolve(), self.old_pointer)
            return actual_activate(*args)
        with patch.object(promote, 'activate_nodes', side_effect=activate) as activation:
            self.execute()
        activation.assert_called_once()
        self.assertEqual((self.root / 'bin/nodes').resolve().name, self.node_manifest['version'])
        self.assertTrue(node_artifacts.managed_nodes(self.root))
        self.assertEqual([path for path, method in self.calls if method == 'POST'], ['hub/update/check'])
        self.assertEqual(json.loads((self.root / 'hub.json').read_text())['auto_update_nodes'], False)

    def enable_automatic(self):
        (self.root / 'hub.json').write_text('{"auto_update_nodes":true}')
        self.inventory['machines'][0].update(manager='manager-a', machine_uid='node-uid')

    def test_automatic_policy_allows_existing_guarded_deployment(self):
        self.enable_automatic()
        self.inventory['machines'][0]['deploying'] = True
        original_api = self.api
        def advancing(root, path, method='GET', body=None):
            if path == 'hub/update/check':
                self.inventory['machines'][0].update(node_version=self.node_manifest['version'],
                                                     online=False)
            return original_api(root, path, method, body)
        with patch.object(self, 'api', side_effect=advancing):
            self.execute()
        self.assertEqual((self.root / 'bin/nodes').resolve().name, self.node_manifest['version'])
        self.assertEqual([path for path, method in self.calls if method == 'POST'], ['hub/update/check'])

    def test_automatic_policy_preserves_busy_nodes_without_forcing_deployment(self):
        self.enable_automatic()
        self.execute()
        self.assertEqual(self.inventory['machines'][0]['node_version'], 'unchanged')
        self.assertEqual([path for path, method in self.calls if method == 'POST'], ['hub/update/check'])

    def test_automatic_policy_rechecks_after_both_feeds_are_active(self):
        self.enable_automatic()
        original_api = self.api
        def checked(root, path, method='GET', body=None):
            if path == 'hub/update/settings':
                self.assertTrue((root / 'releases/hub/node-packages/latest.json').exists())
            return original_api(root, path, method, body)
        with patch.object(self, 'api', side_effect=checked):
            self.execute()
        self.assertEqual(self.calls.count(('hub/update/settings', 'PUT')), 1)

    def test_recheck_failure_does_not_claim_confirmed_or_delete_staging(self):
        self.enable_automatic()
        original_api = self.api
        def failed(root, path, method='GET', body=None):
            if path == 'hub/update/settings':
                raise OSError('settings unavailable')
            return original_api(root, path, method, body)
        with patch.object(self, 'api', side_effect=failed):
            with self.assertRaisesRegex(OSError, 'settings unavailable'):
                self.execute()
        self.assertTrue(self.staged.exists())
        self.assertNotIn(('hub/update/history', 'GET'), self.calls)

    def test_recheck_does_not_reenable_policy_disabled_during_publication(self):
        self.enable_automatic()
        original_api = self.api
        def disabled(root, path, method='GET', body=None):
            result = original_api(root, path, method, body)
            if path == 'hub/update' and 'health' in self.events:
                result['auto_update_nodes'] = False
            return result
        with patch.object(self, 'api', side_effect=disabled):
            self.execute()
        self.assertNotIn(('hub/update/settings', 'PUT'), self.calls)

    def test_automatic_policy_requires_matching_packages_before_publication(self):
        self.enable_automatic()
        with self.assertRaisesRegex(ValueError, 'matching verified'):
            self.execute(enabled=False)
        self.assert_unchanged_and_no_update_requested()

    def test_automatic_policy_rejects_missing_identity_before_publication(self):
        self.enable_automatic()
        del self.inventory['machines'][0]['machine_uid']
        with self.assertRaisesRegex(RuntimeError, 'registered machine identities'):
            self.execute()
        self.assert_unchanged_and_no_update_requested()

    def assert_identity_change_rejected(self, field):
        self.enable_automatic()
        original_api = self.api
        def changed(root, path, method='GET', body=None):
            if path == 'hub/update/check':
                self.inventory['machines'][0][field] = 'changed'
            return original_api(root, path, method, body)
        with patch.object(self, 'api', side_effect=changed):
            with self.assertRaisesRegex(RuntimeError, 'ownership or identity'):
                self.execute()
        self.assertEqual((self.root / 'bin/nodes').resolve(), self.old_pointer)

    def test_automatic_policy_rejects_owner_change(self):
        self.assert_identity_change_rejected('manager')

    def test_automatic_policy_rejects_uid_change(self):
        self.assert_identity_change_rejected('machine_uid')

    def test_runtime_policy_takes_precedence_over_bootstrap_configuration(self):
        self.enable_automatic()
        original_api = self.api
        def runtime_disabled(root, path, method='GET', body=None):
            result = original_api(root, path, method, body)
            if path == 'hub/update':
                result['auto_update_nodes'] = False
            return result
        with patch.object(self, 'api', side_effect=runtime_disabled):
            self.execute()
        self.assertEqual(self.inventory['machines'][0]['node_version'], 'unchanged')

    def test_runtime_can_enable_policy_with_bootstrap_default_off(self):
        self.enable_automatic()
        (self.root / 'hub.json').write_text('{"auto_update_nodes":false}')
        self.inventory['machines'][0]['deploying'] = True
        original_api = self.api
        def runtime_enabled(root, path, method='GET', body=None):
            result = original_api(root, path, method, body)
            if path == 'hub/update':
                result['auto_update_nodes'] = True
            return result
        with patch.object(self, 'api', side_effect=runtime_enabled):
            self.execute()
        self.assertEqual((self.root / 'bin/nodes').resolve().name, self.node_manifest['version'])

    def test_non_boolean_automatic_policy_is_rejected(self):
        (self.root / 'hub.json').write_text('{"auto_update_nodes":"true"}')
        with self.assertRaisesRegex(ValueError, 'must be a boolean'):
            self.execute()
        self.assert_unchanged_and_no_update_requested()

    def test_unmanaged_pointer_blocks_all_publication_before_any_api(self):
        (self.root / 'bin/nodes').unlink()
        (self.root / 'bin/nodes').mkdir()
        with self.assertRaisesRegex(RuntimeError, 'Unmanaged'):
            self.execute()
        self.assertEqual(self.calls, [])
        self.assertEqual(self.latest.read_bytes(), self.old_latest)



    def test_symlink_lock_is_rejected_without_truncating_its_target(self):
        target = self.root / 'lock-target'
        target.write_text('preserve')
        (self.latest.parent / '.publish.lock').symlink_to(target)
        with self.assertRaises(OSError):
            self.execute()
        self.assertEqual(target.read_text(), 'preserve')
        self.assertEqual(self.calls, [])

    def test_selected_feed_is_revalidated_after_acquiring_shared_lock(self):
        actual_flock = promote.fcntl.flock
        def replace_feed(stream, operation):
            actual_flock(stream, operation)
            moved = self.latest.parent.with_name('hub-moved')
            self.latest.parent.rename(moved)
            self.latest.parent.symlink_to(moved, target_is_directory=True)
        with patch.object(promote.fcntl, 'flock', side_effect=replace_feed):
            with self.assertRaisesRegex(ValueError, 'non-symlink'):
                self.execute()
        self.assertEqual(self.calls, [])
        self.assertEqual(self.latest.read_bytes(), self.old_latest)

    def test_replaced_lock_is_rejected_after_acquiring_it(self):
        actual_flock = promote.fcntl.flock
        lock_path = self.latest.parent / '.publish.lock'
        def replace_lock(stream, operation):
            actual_flock(stream, operation)
            lock_path.unlink()
            lock_path.write_text('replacement')
        with patch.object(promote.fcntl, 'flock', side_effect=replace_lock):
            with self.assertRaisesRegex(ValueError, 'lock changed'):
                self.execute()
        self.assertEqual(self.calls, [])
        self.assertEqual(self.latest.read_bytes(), self.old_latest)


if __name__ == '__main__':
    unittest.main()
