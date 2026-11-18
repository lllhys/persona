"""The link device helper's server and pure judgments; no device, adb or WebView is involved."""
import importlib.util
import re
from pathlib import Path
import tempfile
import threading
import unittest
import urllib.error
import urllib.request

SPEC = importlib.util.spec_from_file_location("link_reopen", Path(__file__).with_name("android_link_reopen_check.py"))
check = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(check)

DUMP = """<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy rotation="0"><node text="" content-desc="Close page" bounds="[900,100][1000,200]" />
<node text="page=a load=7 history=1" content-desc="" bounds="[0,300][1080,400]" />
<node text="http://192.0.2.10:8000/a" content-desc="" bounds="[100,1000][700,1100]" /></hierarchy>"""


class ServerTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.log = str(Path(self.tmp.name) / "requests.jsonl")
        self.server = check.make_server("127.0.0.1", 0, self.log)
        self.token = self.server.RequestHandlerClass.session_token
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.base = f"http://127.0.0.1:{self.server.server_address[1]}"

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.tmp.cleanup()

    def get(self, path, cookie=None):
        request = urllib.request.Request(self.base + path, headers={"Cookie": cookie} if cookie else {})
        with urllib.request.urlopen(request) as response:
            return response.read().decode(), response

    def login(self):
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args, **kwargs):
                return None
        try:
            urllib.request.build_opener(NoRedirect).open(self.base + "/login")
        except urllib.error.HTTPError as error:
            self.assertEqual(error.code, 302)
            return error
        self.fail("login should redirect")

    def test_every_request_is_logged_with_a_sequence_a_time_and_the_session(self):
        self.get("/a")
        self.get("/whoami", "session=kept")
        entries = check.read_log(self.log)
        self.assertEqual([(entry["seq"], entry["path"], entry["session"]) for entry in entries], [(1, "/a", None), (2, "/whoami", "kept")])
        self.assertLessEqual(entries[0]["t"], entries[1]["t"])

    def test_a_page_carries_the_load_number_of_the_request_that_made_it_and_is_never_cached(self):
        first, response = self.get("/a")
        second, _ = self.get("/a")
        self.assertIn("load=1 ", first)
        self.assertIn("load=2 ", second)
        self.assertEqual(response.headers["Cache-Control"], "no-store")

    def test_whoami_echoes_the_exact_session_or_missing(self):
        self.assertIn("session=missing", self.get("/whoami")[0])
        self.assertIn("session=kept", self.get("/whoami", "session=kept")[0])
        self.assertIn("session=missing", self.get("/whoami", "othersession=kept")[0])

    def test_login_issues_one_session_and_later_preloads_cannot_reissue_it(self):
        first = self.login()
        self.assertEqual(first.headers["Location"], "/a")
        self.assertIn(f"session={self.token}", first.headers["Set-Cookie"])
        second = self.login()
        self.assertIsNone(second.headers["Set-Cookie"])
        issued = [entry["issued"] for entry in check.read_log(self.log) if entry["issued"]]
        self.assertEqual(issued, [self.token])

    def test_the_cache_page_has_a_no_store_document_and_one_unique_cacheable_script(self):
        page, response = self.get("/cache")
        self.assertEqual(response.headers["Cache-Control"], "no-store")
        asset = page.split('src="', 1)[1].split('"', 1)[0]
        self.assertTrue(asset.startswith("/cache-asset.js?run="))
        script, asset_response = self.get(asset)
        self.assertEqual(script, check.CACHE_ASSET)
        self.assertEqual(asset_response.headers["Cache-Control"], "public, max-age=3600")
        self.assertTrue(asset_response.headers["Content-Type"].startswith("application/javascript"))
        self.assertEqual([entry["path"] for entry in check.read_log(self.log)], ["/cache", "/cache-asset.js"])


class DumpTest(unittest.TestCase):
    def test_nodes_are_found_by_text_or_description_and_tapped_at_their_center(self):
        nodes = check.parse_nodes(DUMP)
        self.assertEqual(check.center(check.find(nodes, desc="Close page")["bounds"]), (950, 150))
        self.assertEqual(check.center(check.find(nodes, text="192.0.2.10:8000/a")["bounds"]), (400, 1050))
        self.assertIsNone(check.find(nodes, desc="Missing"))

    def test_the_page_state_is_read_from_its_state_line_only(self):
        self.assertEqual(check.page_state(check.parse_nodes(DUMP)), ("a", 7, 1))
        self.assertIsNone(check.page_state([{"text": "history=1", "desc": "", "bounds": (0, 0, 1, 1)}]))

    def test_session_and_cache_states_include_the_response_sequence(self):
        nodes = [{"text": "whoami load=8 session=abc123", "desc": "", "bounds": (0, 0, 1, 1)},
                 {"text": "cache load=9 asset=loaded", "desc": "", "bounds": (0, 0, 1, 1)}]
        self.assertEqual(check.whoami_state(nodes), (8, "abc123"))
        self.assertEqual(check.cache_state(nodes), (9, "loaded"))


class CookieJudgmentTest(unittest.TestCase):
    def test_the_original_session_must_survive_in_two_new_responses_produced_after_login(self):
        first = {"seq": 2, "path": "/whoami", "t": 11}
        second = {"seq": 3, "path": "/whoami", "t": 12}
        self.assertEqual(check.judge_cookie("token", 10, [(2, "token", first), (3, "token", second)]), [])
        self.assertTrue(check.judge_cookie("token", 10, [(2, "token", first), (3, "missing", second)]))
        self.assertTrue(check.judge_cookie("token", 10, [(2, "token", first), (2, "token", first)]))

    def test_a_prelogin_response_cannot_prove_cookie_preservation(self):
        stale = {"seq": 2, "path": "/whoami", "t": 9}
        fresh = {"seq": 3, "path": "/whoami", "t": 12}
        failures = check.judge_cookie("token", 10, [(2, "token", stale), (3, "token", fresh)])
        self.assertTrue(any("predates login" in failure for failure in failures))


class CacheJudgmentTest(unittest.TestCase):
    def test_only_the_first_load_of_the_unique_script_may_reach_the_server(self):
        produced = {"seq": 2, "path": "/cache", "t": 12}
        one_asset = [{"seq": 1, "path": "/cache-asset.js", "t": 10}]
        self.assertEqual(check.judge_cache(1, 2, produced, one_asset, 11), [])
        self.assertTrue(check.judge_cache(1, 2, produced, one_asset * 2, 11))
        self.assertTrue(check.judge_cache(1, 1, produced, one_asset, 11))
        self.assertTrue(check.judge_cache(1, 2, None, one_asset, 11))


class PlaceholderHostTest(unittest.TestCase):
    def test_links_carry_a_placeholder_name_not_an_address(self):
        self.assertEqual(check.PLACEHOLDER_HOST, "persona.test")
        self.assertIsNone(re.search(r"\d+\.\d+\.\d+\.\d+", check.PLACEHOLDER_HOST))

    def test_the_webview_rule_maps_the_name_to_the_emulator_host_alias(self):
        self.assertEqual(check.webview_command_line("persona.test", check.EMULATOR_HOST_ALIAS),
                         f'_ --host-resolver-rules="MAP persona.test {check.EMULATOR_HOST_ALIAS}"\n')

    def test_a_name_or_address_that_could_carry_more_flags_is_refused(self):
        alias = check.EMULATOR_HOST_ALIAS
        for name, address in (("a b", alias), ("persona.test", alias + " --x"), ('p"', alias), ("p", "host;rm")):
            with self.assertRaises(SystemExit):
                check.webview_command_line(name, address)


if __name__ == "__main__":
    unittest.main()
