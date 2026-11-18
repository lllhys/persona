"""Signing preflight and export use the same configured account resource names, without live Apple requests."""
import contextlib
import importlib.util
import io
from pathlib import Path
import sys
import types
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[2]


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class SigningProfileTests(unittest.TestCase):
    def signing(self, cfg):
        config = load_module('signing_config_under_test', ROOT / 'scripts/persona_config.py')
        asc = types.ModuleType('asc')
        asc.CONFIG = cfg
        asc.BUNDLE_ID = cfg['app']['bundle_id']
        asc.APIError = RuntimeError
        asc.create = Mock(side_effect=AssertionError('no live Apple writes'))
        asc.get_all = Mock(side_effect=AssertionError('no live Apple reads'))
        asc.request = Mock(side_effect=AssertionError('no live Apple requests'))
        with patch.dict(sys.modules, {'asc': asc, 'persona_config': config}), patch.object(sys, 'path', list(sys.path)):
            return load_module('signing_under_test', ROOT / 'scripts/release/asc_signing.py'), config, asc

    def test_check_uses_export_mapping_without_account_writes(self):
        bid = 'com.example.persona'
        names = {bid: 'Existing Production', bid + '.notify': 'Notification Distribution'}
        cfg = {'app': {'bundle_id': bid}, 'app_store': {'provisioning_profiles': names}}
        signing, config, asc = self.signing(cfg)
        self.assertEqual({key: values[1] for key, values in signing.TARGETS.items()},
                         config.provisioning_profiles(cfg))
        with patch.object(signing, 'certificate', return_value='certificate'), \
                patch.object(signing, 'bundle_id', side_effect=lambda identifier, *_: identifier), \
                patch.object(signing, 'ensure_capability'), patch.object(signing, 'profile') as profile, \
                patch.object(sys, 'argv', ['asc_signing.py', '--check']), contextlib.redirect_stdout(io.StringIO()):
            signing.main()
        self.assertEqual(profile.call_count, 6)
        expected_groups = {
            bid: config.app_group(cfg),
            bid + '.liveactivity': config.app_group(cfg),
            bid + '.watchkitapp': config.watch_app_group(cfg),
            config.watch_widget_bundle_id(cfg): config.watch_app_group(cfg),
            bid + '.notify': None,
            bid + '.share': None,
        }
        self.assertEqual({call.args[0] for call in profile.call_args_list}, set(expected_groups))
        for call in profile.call_args_list:
            self.assertEqual(call.args[1], config.provisioning_profiles(cfg)[call.args[0]])
            self.assertEqual(call.kwargs['group'], expected_groups[call.args[0]])
            self.assertIs(call.kwargs['check_only'], True)
        asc.create.assert_not_called()
        asc.request.assert_not_called()
        asc.get_all.assert_not_called()

    def test_custom_development_name_never_reuses_the_store_profile(self):
        bid = 'com.example.persona'
        signing, _, _ = self.signing({'app': {'bundle_id': bid}, 'app_store': {
            'provisioning_profiles': {bid + '.share': 'Share Production'}}})
        with patch.object(signing, 'certificate', side_effect=['distribution-certificate', 'development-certificate']), \
                patch.object(signing, 'bundle_id', return_value='share-resource'), \
                patch.object(signing, 'ensure_capability'), patch.object(signing, 'profile') as profile, \
                patch.object(sys, 'argv', ['asc_signing.py', '--check', '--dev', '--only', bid + '.share']), \
                contextlib.redirect_stdout(io.StringIO()):
            signing.main()
        self.assertEqual(profile.call_count, 2)
        self.assertEqual(profile.call_args_list[0].args[1], 'Share Production')
        self.assertEqual(profile.call_args_list[1].args, ('share-resource', 'Share Production Development',
                                                        'development-certificate', 'IOS_APP_DEVELOPMENT', True))

    def test_only_retains_the_other_targets(self):
        bid = 'com.example.persona'
        signing, _, _ = self.signing({'app': {'bundle_id': bid}, 'app_store': {
            'provisioning_profiles': {bid + '.share': 'Share Production'}}})
        with patch.object(signing, 'certificate', return_value='certificate'), \
                patch.object(signing, 'bundle_id', return_value='share-resource'), \
                patch.object(signing, 'ensure_capability'), patch.object(signing, 'profile') as profile, \
                patch.object(sys, 'argv', ['asc_signing.py', '--check', '--only', bid + '.share']), \
                contextlib.redirect_stdout(io.StringIO()):
            signing.main()
        profile.assert_called_once_with('share-resource', 'Share Production', 'certificate',
                                        check_only=True, group=None)

    def test_only_watch_widget_checks_its_separate_group(self):
        bid = 'com.example.persona'
        widget = bid + '.watchkitapp.complication'
        group = 'group.com.example.wrist'
        signing, _, asc = self.signing({'app': {'bundle_id': bid, 'watch_widget_bundle_id': widget,
                                              'watch_app_group': group},
                                       'app_store': {'provisioning_profiles': {widget: 'Wrist Production'}}})
        with patch.object(signing, 'certificate', return_value='certificate'), \
                patch.object(signing, 'bundle_id', return_value='widget-resource') as bundle, \
                patch.object(signing, 'ensure_capability') as capability, \
                patch.object(signing, 'profile') as profile, \
                patch.object(sys, 'argv', ['asc_signing.py', '--check', '--only', widget]), \
                contextlib.redirect_stdout(io.StringIO()):
            signing.main()
        bundle.assert_called_once_with(widget, 'Persona Watch Widgets', True)
        capability.assert_called_once_with('widget-resource', 'APP_GROUPS', True)
        profile.assert_called_once_with('widget-resource', 'Wrist Production', 'certificate',
                                       check_only=True, group=group)
        asc.create.assert_not_called()
        asc.request.assert_not_called()
        asc.get_all.assert_not_called()


class SiriCallsSigningTests(unittest.TestCase):
    """app.siri_calls (#9290): SIRIKIT and the SiriKit extension's App ID and profile are asked for only with the switch
    on, so the release preflight (`--check`) of a build without it is what it was."""
    bid = 'com.example.persona'
    BEFORE = ['PUSH_NOTIFICATIONS', 'USERNOTIFICATIONS_TIMESENSITIVE', 'HEALTHKIT', 'APP_GROUPS']
    signing = SigningProfileTests.signing

    def run_main(self, signing, argv, bundle_id=lambda identifier, *_: identifier):
        with patch.object(signing, 'certificate', return_value='certificate'), \
                patch.object(signing, 'bundle_id', side_effect=bundle_id) as bundle, \
                patch.object(signing, 'ensure_capability') as capability, patch.object(signing, 'profile') as profile, \
                patch.object(sys, 'argv', ['asc_signing.py'] + argv), contextlib.redirect_stdout(io.StringIO()):
            signing.main()
        return bundle, capability, profile

    def test_off_checks_what_it_always_did(self):
        for value in (None, False, 'true'):
            with self.subTest(value=value):
                app = {'bundle_id': self.bid} if value is None else {'bundle_id': self.bid, 'siri_calls': value}
                signing, _, asc = self.signing({'app': app})
                self.assertEqual(signing.TARGETS[self.bid][2], self.BEFORE)
                self.assertNotIn(self.bid + '.callintents', signing.TARGETS)
                bundle, capability, profile = self.run_main(signing, ['--check'])
                self.assertNotIn('SIRIKIT', {call.args[1] for call in capability.call_args_list})
                self.assertEqual(profile.call_count, 6)
                self.assertEqual(bundle.call_count, 6)
                asc.create.assert_not_called()

    def test_on_checks_sirikit_and_the_extension(self):
        signing, config, asc = self.signing({'app': {'bundle_id': self.bid, 'siri_calls': True}})
        self.assertEqual(signing.TARGETS[self.bid][2], self.BEFORE + ['SIRIKIT'])
        bundle, capability, profile = self.run_main(signing, ['--check'])
        capability.assert_any_call(self.bid, 'SIRIKIT', True)
        bundle.assert_any_call(self.bid + '.callintents', 'Persona Call Intents', True)
        self.assertEqual(profile.call_count, 7)
        extension = [call for call in profile.call_args_list if call.args[0] == self.bid + '.callintents']
        self.assertEqual(len(extension), 1)
        self.assertEqual(extension[0].args[1], 'Persona Call Intents App Store')
        self.assertIsNone(extension[0].kwargs['group'])
        self.assertEqual({key: values[1] for key, values in signing.TARGETS.items()},
                         config.provisioning_profiles({'app': {'bundle_id': self.bid, 'siri_calls': True}}))
        asc.create.assert_not_called()

    def test_the_maintainer_prepares_one_target_at_a_time(self):
        # docs/operations/release.md "Siri calls": SIRIKIT on the app's App ID, then the extension's, each with --only.
        signing, _, _ = self.signing({'app': {'bundle_id': self.bid, 'siri_calls': True}})
        bundle, capability, profile = self.run_main(signing, ['--only', self.bid], bundle_id=lambda *_: 'app-resource')
        bundle.assert_called_once_with(self.bid, 'Persona', False)
        self.assertEqual([call.args[1] for call in capability.call_args_list], self.BEFORE + ['SIRIKIT'])
        self.assertTrue(all(call.args[2] is False for call in capability.call_args_list))
        profile.assert_called_once_with('app-resource', 'Persona App Store', 'certificate', check_only=False,
                                        group=signing.GROUP)
        bundle, capability, profile = self.run_main(signing, ['--only', self.bid + '.callintents'],
                                                    bundle_id=lambda *_: 'extension-resource')
        bundle.assert_called_once_with(self.bid + '.callintents', 'Persona Call Intents', False)
        capability.assert_not_called()
        profile.assert_called_once_with('extension-resource', 'Persona Call Intents App Store', 'certificate',
                                        check_only=False, group=None)


if __name__ == '__main__':
    unittest.main()
