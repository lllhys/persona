// The context bar below the composer (#2430, moved by #5056): shown whenever context is known, whatever "Show details in
// Agent rows" says. The model is the composer's model pill (#5032), not the usage row's.
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { setAgentDetails } from "../../lib/context-usage";
import type { Session } from "../../lib/types";
import { ContextMeter } from "./ContextMeter";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

const s: Session = { machine_id: "m", session_id: "s", title: "t", cwd: "/code", status: "idle", model: "claude-opus-5-5", context_tokens: 78000, context_window: 200000 };

describe("ContextMeter", () => {
  afterEach(() => {
    setAgentDetails(false);
  });
  it("shows labelled usage even while the sidebar details setting is off", () => {
    setAgentDetails(false);
    const html = renderToStaticMarkup(<ContextMeter session={{ ...s, context_tokens: 20000 }} />);
    expect(html).toContain(">上下文 10%<");
    expect(html).toContain('aria-valuenow="10"');
    expect(html).toContain('style="width:10%;');
  });
  it.each([170000, 180000])("shows amber caution when using %s of a 200k window", (tokens) => {
    const html = renderToStaticMarkup(<ContextMeter session={{ ...s, context_tokens: tokens }} />);
    const percent = tokens / 2000;
    expect(html).toContain(`>上下文 ${percent}%<`);
    expect(html).toContain(`aria-valuenow="${percent}"`);
    expect(html).toContain(`style="width:${percent}%;`);
    expect(html).toContain("bg-orange");
    expect(html).toContain("text-orange");
    expect(html).toContain("/compact");
  });
  it("shows nothing while the window is unknown, the model alone included", () => {
    setAgentDetails(true);
    expect(renderToStaticMarkup(<ContextMeter session={{ ...s, context_window: undefined }} />)).toBe("");
  });
  it("names both shares for assistive technology and in the tooltip", () => {
    const html = renderToStaticMarkup(<ContextMeter session={s} />);
    expect(html).toMatch(/^<button type="button" aria-haspopup="dialog" aria-expanded="false"/);
    expect(html).toContain('aria-label="上下文 39%（已用），还剩 61%"');
    expect(html).toContain('role="progressbar" aria-label="上下文 39%（已用），还剩 61%" aria-valuenow="39"');
    expect(html).toMatch(/title="上下文 39%（已用），还剩 61%\n上下文是 Agent/);
    expect(html).toContain('aria-description="上下文是 Agent 在这段对话里能记住的量');
    expect(html).not.toContain("opus");
  });
  it("keeps the labelled share and popover button in compact mode", () => {
    const html = renderToStaticMarkup(<ContextMeter session={s} compact />);
    expect(html).toMatch(/^<button type="button" aria-haspopup="dialog"/);
    expect(html).toContain(">上下文 39%<");
    expect(html).toContain('aria-label="上下文 39%（已用），还剩 61%"');
    expect(html).not.toContain("progressbar");
    expect(html).not.toContain("opus");
    expect(renderToStaticMarkup(<ContextMeter session={{ ...s, context_window: undefined }} compact />)).toBe("");
  });
});
