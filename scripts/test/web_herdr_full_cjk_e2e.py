#!/usr/bin/env python3
"""Check that the terminal's "Full view" keeps every cell on the grid when a row holds Chinese, full-width punctuation,
symbols and emoji (docs/integrations/terminal.md "Character width and CJK alignment", #2751).

The fake hub runs this script as the herdr client (MOCK_TERMINAL_FULL_EXEC, `--client`): it draws what herdr's own
client writes for a two-pane layout, a box table in the left pane, with the cursor placed before every wide character
the way herdr does (docs/lessons/terminal.md). The page draws it in xterm's DOM renderer; the check reads where the
browser put every border glyph:

  - the `│` of every row is in the same place as on the row that holds only ASCII (before the fix the full-width
    full-width brackets, parentheses and punctuation drifted a row by up to three cells and the pane next to it zig-zagged);
  - an emoji covers two cells like it does in herdr (xterm's default table makes it one: the cell herdr leaves to its
    second half kept what an earlier frame drew there).

    MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1 MOCK_TERMINAL_FULL=1 \\
        MOCK_TERMINAL_FULL_EXEC="python3 scripts/test/web_herdr_full_cjk_e2e.py --client" PORT=18992 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18992 npx vite --port 18993 --strictPort &)
    python3 scripts/test/web_herdr_full_cjk_e2e.py --base http://127.0.0.1:18993 [--shots <dir>]

Needs Playwright for Python (the installed Chrome, or the browser PERSONA_E2E_CHROMIUM points at). Prints one line per
check and a verdict; exits 1 on the first failure.
"""
import argparse
import os
import sys
import time
import unicodedata

SHELL = "pane%3Aw1%3Ap2"  # a shell of the "persona" workspace on the fake hub's first machine
BORDERS = (0, 59, 61, 99)  # columns (0-based) of the two panes' borders
STALE = "Z"
END = "FRAME-TWO-DRAWN"
# Wide for herdr but not for xterm's default table (Unicode 6); the East Asian Width property covers the rest.
EMOJI = "\U0001F600\U0001F680✅"
ROWS = [
    ("Ctrl+B is the prefix key", "plain ASCII row, nothing wide"),
    ("Ctrl+B 是 herdr 的前缀键", "完整界面里交给 herdr"),
    ("「开关右侧面板」（快捷键）", "默认快捷键里只有这一个，"),
    ("、：；！？ 逗号顿号", "改成 ⌘/Ctrl+点击才打开"),
    ("⌘ ⇧ ✓ ★ → ← ↑ ↓", "箭头与符号 ✔ ✘ ● ○ ■ □"),
    ("日本語のテキスト ひらがな", "한국어 텍스트 hangul"),
    (f"emoji {EMOJI[0]} {EMOJI[1]} {EMOJI[2]} 中文", "，。；：！？“”‘’（）【】《》"),
    ("Plain ASCII row again", "-- ASCII --"),
]
FIRST_ROW = 3  # 1-based screen row of ROWS[0]


def width(ch):
    return 2 if ch in EMOJI or unicodedata.east_asian_width(ch) in "WF" else 1


def put(row, col, text):
    """Text at a (1-based) cell the way herdr writes it: the cursor is placed before a wide character and after it."""
    out, prev_wide = [f"\x1b[{row};{col}H"], False
    for ch in text:
        w = width(ch)
        if (w == 2 or prev_wide) and len(out) > 1:
            out.append(f"\x1b[{row};{col}H")
        out.append(ch)
        col += w
        prev_wide = w == 2
    return "".join(out)


def frame(second):
    out = [] if second else ["\x1b[2J"]  # the second frame draws over the first, as herdr redraws only what changed
    top = FIRST_ROW - 1
    bottom = FIRST_ROW + len(ROWS)
    for r in range(top, bottom + 1):
        for c in BORDERS:
            out.append(f"\x1b[{r};{c + 1}H" + ("│" if top < r < bottom else "┌┐"[c in (59, 99)] if r == top else "└┘"[c in (59, 99)]))
        if top < r < bottom:
            continue
        out.append(put(r, 2, "─" * 58) + put(r, 63, "─" * 37))
    for i, (left, right) in enumerate(ROWS):
        r = FIRST_ROW + i
        out.append(put(r, 3, left) + put(r, 64, right))
        if left.startswith("emoji"):
            c = 3 + len("emoji ")
            for k, e in enumerate(EMOJI):
                if second:
                    out.append(put(r, c, e))  # drawn over the cell the first frame filled
                else:
                    out.append(f"\x1b[{r};{c + 1}H{STALE}")  # what an earlier frame left in the cell an emoji covers
                c += 3
    if second:
        out.append(put(bottom + 2, 3, END))
    return "".join(out)


def client():
    """What the fake hub runs in place of herdr's client: draws, waits a moment, draws again, stays alive."""
    sys.stdout.write("\x1b[?2031h\x1b[?1000h\x1b[?1006h\x1b[?2004h" + frame(False))
    sys.stdout.flush()
    time.sleep(1.5)
    sys.stdout.write(frame(True))
    sys.stdout.flush()
    time.sleep(600)


def ok(msg):
    print("ok  ", msg, flush=True)


ROWS_JS = """() => {
  const rows = [...document.querySelectorAll('[data-surface="shown"] .xterm-rows > div')];
  const style = getComputedStyle(rows[0].parentElement);
  const context = document.createElement('canvas').getContext('2d');
  context.font = `${style.fontSize} ${style.fontFamily}`;
  const metrics = { wide: context.measureText('W').width, narrow: context.measureText('i').width,
    cjk: context.measureText('中').width, adjusted: [...document.fonts].some(f => f.family === 'PersonaTermCJK' && f.status === 'loaded') };
  return { metrics, rows: rows.map((row) => {
    const xs = [], walker = document.createTreeWalker(row, NodeFilter.SHOW_TEXT);
    for (let n; (n = walker.nextNode()); )
      for (let i = 0; i < n.data.length; i++)
        if (n.data[i] === '│') {
          const r = document.createRange(); r.setStart(n, i); r.setEnd(n, i + 1);
          const b = r.getBoundingClientRect(); xs.push(b.left + b.width / 2);
        }
    return { text: row.textContent, xs };
  }) };
}"""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--client", action="store_true", help="act as herdr's client (what the fake hub runs)")
    ap.add_argument("--base")
    ap.add_argument("--shots")
    ap.add_argument("--tolerance", type=float, default=0.25, help="cells a border may differ from the ASCII row's")
    a = ap.parse_args()
    if a.client:
        client()
        return
    if not a.base:
        ap.error("--base is required")
    from playwright.sync_api import sync_playwright

    with sync_playwright() as p:
        exe = os.environ.get("PERSONA_E2E_CHROMIUM")
        browser = p.chromium.launch(executable_path=exe) if exe else p.chromium.launch(channel="chrome")
        for scheme in ("light", "dark"):
            ctx = browser.new_context(viewport={"width": 1800, "height": 900}, color_scheme=scheme, device_scale_factor=2)
            ctx.add_init_script(f"localStorage.setItem('persona.token', 'mock'); localStorage.setItem('persona.theme', {scheme!r})")
            page = ctx.new_page()
            page.set_default_timeout(15000)
            page.goto(f"{a.base}/terminal/server/{SHELL}")
            page.wait_for_function(
                """(end) => [...document.querySelectorAll('[data-surface="shown"] .xterm-rows > div')].some((r) => r.textContent.includes(end))""",
                arg=END,
            )
            time.sleep(0.5)
            data = page.evaluate(ROWS_JS)
            metrics = data["metrics"]
            assert abs(metrics["wide"] - metrics["narrow"]) < 0.01, f"{scheme}: proportional terminal font: {metrics}"
            if metrics["adjusted"]:
                assert abs(metrics["cjk"] - 2 * metrics["wide"]) < 0.05, f"{scheme}: adjusted CJK face is shadowed: {metrics}"
            ok(f"{scheme}: Latin glyphs share one cell and the adjusted CJK face fills two")
            rows = data["rows"]
            # one cell: the distance between the first two borders over their columns
            ref_xs = next(r["xs"] for r in rows if r["text"].count("Plain ASCII row again") and len(r["xs"]) == len(BORDERS))
            cell = (ref_xs[1] - ref_xs[0]) / (BORDERS[1] - BORDERS[0])
            worst = (0.0, "")
            for i, (left, _right) in enumerate(ROWS):
                row = rows[FIRST_ROW - 1 + i]
                assert len(row["xs"]) == len(BORDERS), f"{scheme}: row {left!r} has {len(row['xs'])} borders: {row['text']!r}"
                for x, ref, col in zip(row["xs"], ref_xs, BORDERS):
                    d = abs(x - ref) / cell
                    if d > worst[0]:
                        worst = (d, f"row {left!r}, column {col}")
            assert worst[0] <= a.tolerance, f"{scheme}: a border is {worst[0]:.2f} cells off the ASCII row's ({worst[1]})"
            ok(f"{scheme}: every row's four borders sit where the ASCII row's do (worst {worst[0]:.2f} cells, {worst[1] or 'none'})")
            text = "\n".join(r["text"] for r in rows)
            assert STALE not in text, f"{scheme}: an emoji left the cell it covers as it was:\n{text}"
            ok(f"{scheme}: an emoji covers two cells like herdr's (nothing left in the second one)")
            if a.shots:
                os.makedirs(a.shots, exist_ok=True)
                page.screenshot(path=os.path.join(a.shots, f"cjk-full-{scheme}.png"))
            ctx.close()
        browser.close()
    print("all passed")


if __name__ == "__main__":
    main()
