#!/usr/bin/env python3
"""agents_list.py [runs] [case …]: AgentsListTests cases through the runtime CLI (apple/PersonaUITests/AgentsListTests.swift,
which stays as it is for now). One launch for every case and run; each case starts from the mock reset and the list's
saved look forgotten on a new view tree, instead of a relaunch.

  python3 scripts/mock/mock_scenarios.py --port 18761 --scenario agents &
  PERSONACTL_UDID=<udid> HUB=http://127.0.0.1:18761 scripts/app/cli-tests/agents_list.py 3
"""
import os

from clitest import PINNED, Fail, main, mock, to_agents

HUB = os.environ.get("HUB", "http://127.0.0.1:18761")
WIDGET = "给天气 App 加主屏幕小组件"
LAUNCH = PINNED + ["-perfCounts", "YES", "-openAgentsFresh", "YES"]
# The mock's agents data (MOCK_AGENTS=1): 48 finished-unread (6 on MacBook, 35 in platform), 6 needing you, 19 working.
UNREAD = "未读 48"


def row_text(s):  # a row is one element: "<state>, <title>, <age>, ..." (the Chinese label)
    return dict(labelContains="、" + s + "、")


def filt(name):
    return dict(id="agents.filter." + name)


def header(space):  # a space's header, the button that folds it: "<space>, expanded, ..." (the Chinese label)
    return dict(labelPrefix=space + "，", trait="button")


def unread_label(t):
    return (t.find(**filt("unread")) or {}).get("label")


def to_top(t):
    """Back to the quick filters at the top of the list (a row below the cards that need you was reached)."""
    for _ in range(6):
        chip = t.find(**filt("all"))
        if chip and chip["visible"]:
            return
        t.c.swipe("down", duration=0.15)
        t.c.call("settle", timeout=2, stable=2, every=100, pixels=600)
    t.see("the quick filters", visibleOnly=True, timeout=3, **filt("all"))


def list_menu(t):
    """Open the list's grouping menu, labeled "Group: ..." in the shell header."""
    t.settle()
    for _ in range(2):
        t.tap("the list menu", labelContains="分组：", trait="button")
        try:
            t.see("the list menu open", label="细列表", timeout=3)
            return
        except Fail:
            pass
    t.see("the list menu open", label="细列表", timeout=2)


def menu_item(t, label):
    t.see(label + " in the menu", label=label, trait="button", timeout=5)
    t.c.tap(label=label, trait="button")  # a menu's items take no accessibility activation: a real touch


def visible_rows(t):
    els = t.c.call("tree", idPrefix="agents.", visibleOnly=True)["elements"]
    return sum(1 for e in els if e["id"] in ("agents.row", "agents.compactRow") and e["frame"][1] > 100)


def setup(t):
    mock(t.c, "reset")  # every agent back to its state, nothing marked seen
    t.c.call("defaults", remove=["dev.machineFilter", "dev.grouping", "dev.compact", "dev.folded"])
    to_agents(t)
    t.c.call("refresh")
    t.see("the quick filters", timeout=15, **filt("all"))
    try:
        t.until("the mock's counts", lambda: unread_label(t) == UNREAD, 3)
    except Fail:
        # The app keeps what it marked seen itself (a case that marked agents read) past the mock's reset: start it
        # again, as XCUITest does for every case.
        t.c.launch(HUB, LAUNCH)
        to_agents(t)
        t.see("the quick filters", timeout=15, **filt("all"))
        t.until("the mock's counts after a relaunch", lambda: unread_label(t) == UNREAD, got=lambda: unread_label(t))


def quick_filters(t):
    t.eq(t.label(**filt("needsYou")), "需要你 6", "needsYou")
    t.eq(t.label(**filt("unread")), UNREAD, "unread")
    t.eq(t.label(**filt("working")), "工作中 19", "working")
    t.see("the cards that need you, under 全部", label="需要你处理")
    t.reach("a working agent under 全部", **row_text(WIDGET))  # below the cards
    to_top(t)

    t.c.tap(**filt("unread"))
    t.until("未读 selected", lambda: "selected" in t.find(**filt("unread"))["traits"])
    t.see("a finished-unread agent", **row_text("platform 任务 4"))
    t.gone("working agents leave under 未读", **row_text(WIDGET))
    t.true(not t.find(label="需要你处理"), "no cards that need you under 未读")
    t.see("mark every one read, under 未读", labelContains="全部标为已读", trait="button")

    t.c.tap(**filt("working"))
    t.see("a working agent under 工作中", **row_text(WIDGET))
    t.gone("finished ones leave under 工作中", **row_text("platform 任务 4"))

    t.c.tap(**filt("needsYou"))
    t.see("the cards that need you, under 需要你", label="需要你处理")
    t.reach("an agent that needs you", **row_text("补齐网络层的单元测试"))
    t.gone("working agents leave under 需要你", **row_text(WIDGET))
    to_top(t)

    list_menu(t)
    menu_item(t, "MacBook")
    t.until("未读 counts MacBook's only", lambda: unread_label(t) == "未读 6", got=lambda: unread_label(t))
    t.eq(t.label(**filt("needsYou")), "需要你 1", "needsYou on MacBook")
    t.c.tap(**filt("unread"))
    t.see("MacBook's unread agent", **row_text("mobile 任务 1"))
    t.gone("Mac Studio's agents leave", **row_text("platform 任务 4"))
    list_menu(t)
    menu_item(t, "全部机器")
    t.until("未读 counts every machine again", lambda: unread_label(t) == UNREAD, got=lambda: unread_label(t))
    t.c.tap(**filt("all"))
    t.reach("everything again under 全部", **row_text(WIDGET))


def compact_rows(t):
    t.c.tap(**filt("unread"))
    t.see("platform's rows", **row_text("platform 任务 4"))
    t.settle()
    cards = visible_rows(t)
    t.true(cards > 0, "card rows on screen")
    list_menu(t)
    menu_item(t, "细列表")
    t.see("compact rows", id="agents.compactRow")
    t.settle()
    compact = visible_rows(t)
    t.true(compact >= cards * 2.5, f"compact {compact} vs cards {cards}")
    t.c.tap(**filt("all"))
    t.reach("a working agent", **row_text(WIDGET))
    for _ in range(4):
        t.c.swipe("up", duration=0.15)

    # Remembered across launches: a real relaunch (the saved look is read at launch, so not -openAgentsFresh).
    t.c.launch(HUB, PINNED + ["-perfCounts", "YES"])
    to_agents(t)
    t.reach("still compact after a relaunch", id="agents.compactRow")
    list_menu(t)
    menu_item(t, "头像")  # back to the default style (#2530)
    t.reach("card rows again", id="agents.row")
    t.gone("no compact rows", timeout=3, id="agents.compactRow")


def folding_spaces(t):
    h = t.reach("platform's header", **header("platform"))
    t.true("已展开" in h["label"], h["label"])
    t.reach("platform's first row", **row_text("platform 任务 1"))
    t.c.tap(**header("platform"))
    t.gone("platform's rows fold away", **row_text("platform 任务 1"))
    folded = t.label(**header("platform"))
    t.true("已折叠" in folded, folded)
    for n in ["3 个需要你", "12 个工作中", "35 个完成未看"]:
        t.true(n in folded, f"{n} in {folded}")

    # Remembered across launches: a real relaunch (the saved fold is read at launch, so not -openAgentsFresh).
    t.c.launch(HUB, PINNED + ["-perfCounts", "YES"])
    to_agents(t)
    t.see("the list after a relaunch", timeout=15, **filt("all"))
    t.true("已折叠" in t.reach("platform's header", **header("platform"))["label"], "still folded after a relaunch")
    t.true(not t.find(**row_text("platform 任务 1")), "platform's rows stay folded")

    list_menu(t)
    menu_item(t, "全部折叠")
    for space in ["weather", "platform", "infra"]:
        t.true("已折叠" in t.reach(f"{space}'s header", **header(space))["label"], f"{space} folded")
    t.gone("the pinned space folds too", **row_text(WIDGET))
    list_menu(t)
    menu_item(t, "全部展开")
    t.reach("platform open again", **row_text("platform 任务 1"))
    t.true("已展开" in t.label(**header("platform")), "platform open")

    # A folded space still opens from its menu.
    to_top(t)
    t.reach("weather's header", **header("weather"))
    t.c.tap(**header("weather"))
    t.tap("weather's menu", label="weather 的更多操作")
    menu_item(t, "打开空间")
    t.see("the space page", label="weather", trait="header", timeout=5)


def mark_space_read(t):
    t.reach("platform's header", **header("platform"))
    t.tap("platform's menu", label="platform 的更多操作")
    menu_item(t, "全部标为已读（35）")
    t.until("the hub hears of 35 seen", lambda: len(mock(t.c, "seen").get("seen", [])) == 35)
    t.until("no finished-unread left on the header",
            lambda: "完成未看" not in t.label(**header("platform")))
    to_top(t)
    t.until("未读 13", lambda: unread_label(t) == "未读 13", got=lambda: unread_label(t))


def working_through_unread(t):
    t.c.tap(**filt("unread"))
    t.until("未读 selected", lambda: "selected" in t.find(**filt("unread"))["traits"])
    t.settle()
    rows = t.c.call("tree", id="agents.row", visibleOnly=True)["elements"]
    if not rows:
        raise Fail("no unread row")
    title = rows[0]["label"].split("、")[1]
    t.c.tap(id="agents.row", labelContains="、" + title + "、")
    t.see("the chat", label="返回", trait="button", visibleOnly=True, timeout=5)
    t.settle()  # the push has to finish first
    t.c.tap(label="返回", trait="button")
    t.see("the opened agent stays in place under 未读", timeout=5, **row_text(title))
    t.until("the hub hears it was seen", lambda: len(mock(t.c, "seen").get("seen", [])) == 1)
    t.until("未读 47", lambda: unread_label(t) == "未读 47", got=lambda: unread_label(t))
    after = t.c.call("tree", id="agents.row", visibleOnly=True)["elements"]
    t.true(len(after) > 1 and after[1]["label"].split("、")[1] != title, "the next one is below it")

    t.c.tap(**filt("all"))
    t.c.tap(**filt("unread"))
    t.gone("a seen agent leaves once the filter is chosen again", **row_text(title))
    t.tap("全部标为已读", labelContains="全部标为已读", trait="button")
    menu_item(t, "全部标为已读（47）")
    t.until("the hub hears of every one", lambda: len(mock(t.c, "seen").get("seen", [])) == 48)
    t.see("the empty filter", label="没有完成未看的会话", timeout=5)
    t.eq(t.label(**filt("unread")), "未读 0", "未读 0")


CASES = [quick_filters, compact_rows, folding_spaces, mark_space_read, working_through_unread]

if __name__ == "__main__":
    main(CASES, setup, lambda c: c.launch(HUB, LAUNCH))
