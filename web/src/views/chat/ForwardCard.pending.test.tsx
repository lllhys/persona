// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it, vi } from "vitest";
import fixture from "../../../../spec/fixtures/forward/several.json";
import { initI18n } from "../../i18n";
import { parseUserText } from "../../lib/turns";
import { ForwardCards } from "./ForwardCard";

// Several forwards of one chat in a bubble (#3838): the card is a chunk of its own; until it is in, the bubble says how
// many in a line of plain words, and the card takes its place once it is.

vi.mock("../../lib/router", () => ({ navigate: vi.fn() }));
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe("a group of forwards in a bubble", () => {
  it("is a line saying how many until its card's chunk is in", async () => {
    await initI18n("en");
    const group = parseUserText((fixture as { read: { message: string }[] }).read[0].message).contexts;
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    act(() => root.render(<ForwardCards blocks={group} count={group.length} />));
    expect(host.querySelector("[data-forward-group-pending]")?.textContent).toBe("3 forwarded messages");
    await act(async () => {
      await vi.dynamicImportSettled();
    });
    await act(async () => {});
    expect(host.querySelector("[data-forward-group-pending]")).toBeNull();
    expect(host.querySelector("[data-forward-group]")?.textContent).toContain("3 forwarded messages · Network tests");
    act(() => root.unmount());
    host.remove();
    await initI18n("zh-Hans");
  });
});
