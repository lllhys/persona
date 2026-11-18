"""deploy.sh says which update source the hub carries when the config has no publish.update_url (#8108): the site's
dl/ may no longer take releases, and a hub built against it never sees a new one. Runs deploy.sh's own lines, from
UPDATE_URL= to the note, against a config of each kind; nothing is built.
"""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
DEPLOY = ROOT / 'scripts/deploy/deploy.sh'
BASH = shutil.which('bash') or '/bin/bash'


def update_source_lines():
    match = re.search(r'^UPDATE_URL=.*?^fi\n', DEPLOY.read_text(), re.S | re.M)
    assert match, 'the update source lines are not in deploy.sh'
    return match.group(0)


class DeployUpdateSourceTests(unittest.TestCase):
    def deploy(self, publish):
        """deploy.sh's update source lines with a config whose publish section is publish: (UPDATE_URL, stderr)."""
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory) / 'config.json'
            config.write_text(json.dumps({'publish': publish}))
            env = {'PATH': os.environ.get('PATH', ''), 'HOME': directory, 'PERSONA_CONFIG': str(config)}
            exports = subprocess.run(['python3', str(ROOT / 'scripts/persona_config.py'), 'env'], env=env, check=True,
                                     capture_output=True, text=True).stdout
            script = exports + update_source_lines() + 'printf "%s" "$UPDATE_URL"\n'
            # deploy.sh runs these from server/ (cd server): ../scripts/persona_config.py is the repository's.
            done = subprocess.run([BASH, '-c', script], cwd=ROOT / 'server', env=env, check=True, capture_output=True,
                                  text=True, stdin=subprocess.DEVNULL)
            return done.stdout, done.stderr

    def test_site_only_says_the_site_dl_and_what_sets_another(self):
        url, err = self.deploy({'site_url': 'https://site.example.test/'})
        self.assertEqual(url, 'https://site.example.test/dl')
        lines = err.splitlines()
        self.assertEqual(len(lines), 1, err)
        for want in ('publish.update_url is not set', 'https://site.example.test/dl', 'hub.json', 'update.url'):
            self.assertIn(want, lines[0])

    def test_empty_update_url_is_unset(self):
        url, err = self.deploy({'site_url': 'https://site.example.test', 'update_url': ''})
        self.assertEqual(url, 'https://site.example.test/dl')
        self.assertIn('publish.update_url is not set', err)

    def test_neither_says_there_is_none(self):
        url, err = self.deploy({})
        self.assertEqual(url, '')
        self.assertIn('none (it will not look for releases)', err)

    def test_update_url_set_says_nothing(self):
        github = 'https://github.com/nwverse/persona/releases/latest/download'
        url, err = self.deploy({'site_url': 'https://site.example.test', 'update_url': github})
        self.assertEqual(url, github)
        self.assertEqual(err, '')


if __name__ == '__main__':
    unittest.main()
