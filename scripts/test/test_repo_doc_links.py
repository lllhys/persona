"""Links to this repository's own files on GitHub must name main, a tracked file and, for a doc, one of its headings.

The clients and the website open docs on GitHub: Add computer's install guides on the web, Android, iPhone and Mac,
the website's setup guide. A link to another branch works only while GitHub keeps a rename redirect, a moved file
breaks it, and an anchor that matches no heading opens the doc at the top (#6402: `master` and `#linuxheadless`).

GitHub makes a heading's anchor by lower-casing its text, dropping everything but letters, digits, spaces, hyphens and
underscores, and turning each space into a hyphen; a repeated heading gets -1, -2. An <a id> or <a name> also counts.
Files under docs/archive are dated records and are not scanned. The self-tests build their URLs from pieces so this
file passes its own scan.
"""
from pathlib import Path
import re
import subprocess
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import test_doc_paths  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
BRANCH = "main"

LINK = re.compile(r"https://github\.com/nwverse/persona/(blob|tree|raw)/([^/\s]+)/([^\s#?\"'<>)\]`]*)(?:#([^\s\"'<>)\]`]*))?")
HEADING = re.compile(r"^#{1,6}\s+(.*?)\s*#*\s*$")
ANCHOR = re.compile(r"<a\s+(?:id|name)=\"([^\"]+)\"")


def slug(heading):
    """GitHub's anchor for a heading: the rendered text, lower-cased, punctuation dropped, spaces as hyphens."""
    text = re.sub(r"!?\[([^\]]*)\]\([^)]*\)", r"\1", heading)  # a link or image keeps its text
    text = re.sub(r"<[^>]+>", "", text)
    text = re.sub(r"\*\*|__|`", "", text).strip().lower()
    return re.sub(r"[^\w\- ]", "", text).replace(" ", "-")


def anchors(text):
    """Every anchor GitHub gives the doc: heading slugs, numbered when repeated, and <a id> / <a name> targets."""
    found, seen = set(ANCHOR.findall(text)), {}
    for _, line in test_doc_paths.outside_fences(text):
        heading = HEADING.match(line)
        if not heading:
            continue
        base = slug(heading[1])
        count = seen.get(base, 0)
        seen[base] = count + 1
        found.add(base if count == 0 else f"{base}-{count}")
    return found


def problems(links, tracked, read):
    """(source, line, url, what is wrong) for each link that names another branch, a missing path or a missing anchor.

    `links` holds (source, line, url) and `tracked` the repository's file paths; `read(path)` returns a tracked doc's text."""
    found, cache = [], {}
    for source, line, url in links:
        match = LINK.match(url)
        kind, branch, path, anchor = match[1], match[2], match[3].rstrip("/"), match[4]
        if branch != BRANCH:
            found.append((source, line, url, f"names the branch {branch!r}, not {BRANCH!r}"))
            continue
        exists = path in tracked if kind != "tree" else any(p.startswith(path + "/") for p in tracked)
        if not exists:
            found.append((source, line, url, f"{path} is not a tracked {'directory' if kind == 'tree' else 'file'}"))
            continue
        if anchor and path.endswith(".md"):
            if path not in cache:
                cache[path] = anchors(read(path))
            if anchor.lower() not in cache[path]:
                found.append((source, line, url, f"#{anchor} matches no heading or <a id> of {path}"))
    return found


def repository_links():
    """(source, line, url) for each link to this repository's files in the tracked files outside docs/archive."""
    # git grep finds the lines (its POSIX patterns lack \s and (?:), LINK reads the links out of them.
    grep = subprocess.run(["git", "grep", "-I", "-n", "-E", r"github\.com/nwverse/persona/(blob|tree|raw)/", "--", ".",
                           ":!docs/archive"], cwd=ROOT, capture_output=True, text=True)
    if grep.returncode not in (0, 1):
        raise RuntimeError(grep.stderr)
    links = []
    for row in grep.stdout.splitlines():
        source, line, text = row.split(":", 2)
        links += [(source, int(line), match[0].rstrip(".,;:")) for match in LINK.finditer(text)]
    return links


def tracked_files():
    listed = subprocess.run(["git", "ls-files", "-z"], cwd=ROOT, check=True, capture_output=True, text=True).stdout
    return {name for name in listed.split("\0") if name}


class RepoDocLinkTests(unittest.TestCase):
    def test_links_name_main_a_tracked_file_and_a_heading(self):
        links = repository_links()
        self.assertTrue(links, "found no links to this repository's files; the pattern or the scan broke")
        bad = problems(links, tracked_files(), lambda path: (ROOT / path).read_text(encoding="utf-8", errors="replace"))
        self.assertEqual([], [f"{s}:{n}: {url}: {why}" for s, n, url, why in bad],
                         f"point each link at {BRANCH}, a tracked path and a current heading's anchor")

    def test_slugs_follow_github(self):
        self.assertEqual("linux-headless", slug("Linux (headless)"))
        self.assertEqual("menu-bar-app-applepersonalink", slug("Menu bar App (`apple/PersonaLink/`)"))
        self.assertEqual("hub-behind-a-reverse-proxy", slug("Hub behind a [reverse proxy](proxy.md)"))
        self.assertEqual("link_bundle_id-and-ports", slug("`link_bundle_id` and ports"))
        self.assertEqual("配对-pairing", slug("配对 Pairing"))
        self.assertEqual({"pairing", "pairing-1", "wire-names"},
                         anchors('# Pairing\n```sh\n# not a heading\n```\n## Pairing\n<a id="wire-names"></a>\n'))

    def test_problems(self):
        base = "https://github.com/" + "nwverse/persona"
        doc = "# Guide\n## Linux (headless)\n"
        tracked = {"docs/link.md", "docs/sub/a.md"}
        links = [("a.ts", 1, f"{base}/blob/{BRANCH}/docs/link.md#linux-headless"),
                 ("a.ts", 2, f"{base}/tree/{BRANCH}/docs/sub"),
                 ("a.ts", 3, f"{base}/blob/master/docs/link.md"),
                 ("a.ts", 4, f"{base}/blob/{BRANCH}/docs/gone.md"),
                 ("a.ts", 5, f"{base}/blob/{BRANCH}/docs/link.md#linuxheadless"),
                 ("a.ts", 6, f"{base}/tree/{BRANCH}/docs/none")]
        self.assertEqual([3, 4, 5, 6], [line for _, line, _, _ in problems(links, tracked, lambda _: doc)])


if __name__ == "__main__":
    unittest.main()
