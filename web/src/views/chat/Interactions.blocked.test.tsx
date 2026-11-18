// @vitest-environment jsdom
// A card this page can't answer says why (#7497), as iOS (#6868) and Android do: a Codex thread Persona hasn't joined
// offers Live link, which makes the card answerable; any other session says to answer on the computer. An answerable
// card has no line, and neither has a manager work card, whose local defer and dismiss still work. A paging hub's
// refresh() never reads an off-page session or a child its parent's chat holds: Live link reads that row itself.
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { InteractionCard } from "./Interactions";
import { setToken } from "../../lib/api";
import { SESSIONS_HIERARCHY_FEATURE } from "../../lib/child-sessions";
import { answerBlock } from "../../lib/format";
import { SESSIONS_LOOKUP_FEATURE } from "../../lib/runtime-list";
import { SESSIONS_LIST_FEATURE } from "../../lib/session-list";
import { getState, rememberSessionRow, reset, upsertSession } from "../../lib/store";
import { useChildSessions } from "../../lib/use-child-sessions";
import { t } from "../../i18n";
import type { Interaction, Session } from "../../lib/types";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const question = { id: "q_1", machine_id: "m", session_id: "s", kind: "question", questions: [{ question: "Which one?", options: [{ label: "A", description: "" }, { label: "B", description: "" }] }] } as Interaction;
const row = (capabilities?: Session["capabilities"], source_kind?: string) => ({ machine_id: "m", session_id: "s", title: "Demo", state: "needs_you", source_kind, capabilities }) as Session;
const unjoinedCaps = { read: true, send: true, approve: false, attach: true };
const linkedCaps = { read: true, send: true, approve: true, attach: false };
const unjoined = row(unjoinedCaps, "codex_app_server");
const subagent = row({ read: true, send: false, approve: false, attach: false }, "codex_app_server");

describe("answerBlock", () => {
  it("is null for a card the page can answer, a sessionless one included", () => {
    expect(answerBlock(question, row())).toBeNull();
    expect(answerBlock(question, row({ read: true, approve: true }, "runtime"))).toBeNull();
    expect(answerBlock({ session_id: "" }, undefined)).toBeNull();
  });
  it("offers attach only where the session can be linked live, else the computer", () => {
    expect(answerBlock(question, unjoined)).toBe("attach");
    expect(answerBlock(question, { ...unjoined, attached: true })).toBe("computer");
    expect(answerBlock(question, subagent)).toBe("computer");
    expect(answerBlock(question, undefined)).toBe("computer");
  });
});

// A hub with session m/s, a Codex thread its node joins on POST …/attach, after which it can approve. `parent`: s is
// folded under p. A paging hub's Agents list (the bounded root page) never holds s.
let linked = false;
let attached: string[] = [];
function hub(parent?: string) {
  const full = () => ({ ...row(linked ? linkedCaps : unjoinedCaps, "codex_app_server"), cwd: "/w", status: "idle", folded_under: parent });
  const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  const page = (sessions: unknown[]) => ({ sessions, remove: [], has_more: false, revision: "r1", total: sessions.length });
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(url, "http://x");
    const q = u.searchParams;
    if (init?.method === "POST" && u.pathname.endsWith("/attach")) {
      attached.push(u.pathname);
      linked = true;
      return json({ ok: true });
    }
    if (u.pathname.endsWith("/interactions")) return json({ interactions: getState().interactions });
    if (u.pathname.endsWith("/machines")) return json({ machines: [] });
    if (!u.pathname.endsWith("/sessions")) return json({});
    // a hub that pushes every row answers a plain read with all of them
    if (!q.has("summary")) return json({ sessions: [full()] });
    if (q.get("counts") === "1") return json({ total: 0, machine: {}, project: {}, group: {}, state: {}, revision: "r1" });
    if (q.get("session_id") === "s") return json(page([q.get("summary") === "0" ? full() : { ...full(), waiting: false }]));
    if (q.get("parent_session_id") === parent) return json(page([{ ...full(), waiting: false }]));
    return json(page([]));
  }));
}

let host: HTMLDivElement;
let root: Root;
const line = () => host.querySelector<HTMLElement>("[data-card-blocked]");
const option = (label: string) => [...host.querySelectorAll<HTMLButtonElement>("[data-card] button")].find((b) => b.textContent?.includes(label))!;
const locked = (b: HTMLButtonElement) => b.disabled || !!b.closest("fieldset[disabled]");
// Waits until `check` holds: the lazy line, a hub read and a reread take as many turns as they take. Every wait is its own
// act, so what resolves meanwhile (the line's chunk included) lands in one and is rendered before the next check; one act
// around the whole wait would hold Suspense's retry until it ended. Bounded, so a change that never comes fails here.
const until = async (check: () => void, timeout = 3000) => {
  const end = Date.now() + timeout;
  for (;;) {
    await act(() => new Promise((r) => setTimeout(r, 10)));
    try {
      return check();
    } catch (e) {
      if (Date.now() > end) throw e;
    }
  }
};
const render = (ui: ReactNode) => act(async () => root.render(ui));
// A blocked card: its line is its own chunk (lazyPage), so wait for the line itself, not a number of turns.
const renderBlocked = async (ui: ReactNode) => {
  await render(ui);
  await until(() => expect(line()).not.toBeNull());
};
const liveLink = async () => {
  const link = [...line()!.querySelectorAll("button")].find((b) => b.textContent === t("chat-chatview.attach"))!;
  expect(link.disabled).toBe(false);
  await act(async () => link.click());
  await until(() => expect(attached).toHaveLength(1));
};
// Linked: the session's new row reaches the card, which answers, and the line goes.
const answersOnceLinked = () => until(() => {
  expect(line()).toBeNull();
  expect(locked(option("A"))).toBe(false);
});

// the module once, as the page has it from its idle preload: a render then waits only for lazyPage's own load
beforeAll(async () => {
  await import("./CardBlockedLine");
});

beforeEach(() => {
  setToken("tok");
  linked = false;
  attached = [];
  Object.assign(getState(), { interactions: [question], answered: {} });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
  vi.unstubAllGlobals();
});

describe("a card the page can't answer", () => {
  it("an unjoined thread's card says linking answers it here, and Live link makes it answerable", async () => {
    hub();
    upsertSession(unjoined);
    await renderBlocked(<InteractionCard ix={question} />);
    expect(locked(option("A"))).toBe(true);
    expect(line()?.dataset.cardBlocked).toBe("attach");
    expect(line()?.textContent).toContain(t("chat-cardblocked.attach"));
    await liveLink();
    await answersOnceLinked();
    expect(attached).toEqual([expect.stringMatching(/\/sessions\/m\/s\/attach$/)]);
  });

  it("a subagent its parent drives says to answer on the computer, with no Live link", async () => {
    upsertSession(subagent);
    await renderBlocked(<InteractionCard ix={question} />);
    expect(locked(option("A"))).toBe(true);
    expect(line()?.textContent).toBe(t("chat-cardblocked.computer"));
    expect(line()?.querySelector("button")).toBeNull();
  });

  it("an answerable card is as before: no line", async () => {
    upsertSession(row());
    await render(<InteractionCard ix={question} />);
    expect(locked(option("A"))).toBe(false);
    expect(line()).toBeNull();
  });

  it("a manager work card has no line: its local defer and dismiss still work", async () => {
    const work = { ...question, id: "work_1", tool_name: "persona_triage", questions: [{ question: "What next?", options: [{ label: "Handled, continue" }, { label: "Leave it", action: "defer" }] }] } as Interaction;
    Object.assign(getState(), { interactions: [work] });
    upsertSession(unjoined);
    await render(<InteractionCard ix={work} />);
    expect(line()).toBeNull();
    expect(locked(option("Leave it"))).toBe(false);
    expect(locked(option("Handled, continue"))).toBe(true);
  });
});

describe("Live link on a paging hub", () => {
  beforeEach(() => {
    Object.assign(getState(), { loaded: true, features: [SESSIONS_LIST_FEATURE, SESSIONS_LOOKUP_FEATURE, SESSIONS_HIERARCHY_FEATURE] });
  });

  it("an off-page session's card answers once linked: its exact lookup reads the new row", async () => {
    hub();
    // what the card's exact summary lookup put among the row hints; the Agents list's page doesn't hold it
    rememberSessionRow({ ...unjoined, waiting: false });
    await renderBlocked(<InteractionCard ix={question} />);
    expect(line()?.dataset.cardBlocked).toBe("attach");
    await liveLink();
    await answersOnceLinked();
    expect(attached).toEqual([expect.stringMatching(/\/sessions\/m\/s\/attach$/)]);
  });

  it("a child's card on its parent's page answers once linked: the parent's held children are read again", async () => {
    hub("p");
    const kid = { ...question, folded_under: "p" } as Interaction;
    Object.assign(getState(), { interactions: [kid] });
    function Parent() {
      useChildSessions("m", "p", true);
      return <InteractionCard ix={kid} />;
    }
    await renderBlocked(<Parent />);
    expect(locked(option("A"))).toBe(true);
    expect(line()?.dataset.cardBlocked).toBe("attach");
    await liveLink();
    await answersOnceLinked();
    expect(attached).toEqual([expect.stringMatching(/\/sessions\/m\/s\/attach$/)]);
  });
});
