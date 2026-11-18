// @vitest-environment jsdom
// The requests graph's hover card: it waits TIP_DELAY of rest, shows at once for the keyboard, goes with the pointer, the
// focus, Escape or a press, and a card asked for by one anchor is not taken away by another one's leaving.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { tipHandlers, TIP_DELAY, useTips } from "./TodoRequestGraphTip";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function Harness() {
  const { api, layer } = useTips();
  return (
    <div>
      <button type="button" data-a="" {...tipHandlers(api, () => <p>first words</p>)}>a</button>
      <svg><circle data-b="" r={3} {...tipHandlers(api, () => <p>second words</p>)} /></svg>
      {layer}
    </div>
  );
}

let host: HTMLDivElement;
let root: Root;
const tip = () => document.querySelector<HTMLElement>('[role="tooltip"]');
// React derives enter and leave from over and out
const enter = (el: Element, from: Element = document.body) => act(() => { el.dispatchEvent(new PointerEvent("pointerover", { bubbles: true, pointerType: "mouse", relatedTarget: from })); });
const leave = (el: Element, to: Element = document.body) => act(() => { el.dispatchEvent(new PointerEvent("pointerout", { bubbles: true, pointerType: "mouse", relatedTarget: to })); });

beforeEach(() => {
  vi.useFakeTimers();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<Harness />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
});

describe("the requests graph's hover card", () => {
  it("comes after the pointer has rested, goes when it leaves, and never before", () => {
    const a = host.querySelector("[data-a]")!;
    enter(a);
    act(() => { vi.advanceTimersByTime(TIP_DELAY - 1); });
    expect(tip()).toBeNull();
    act(() => { vi.advanceTimersByTime(1); });
    expect(tip()!.textContent).toBe("first words");
    leave(a);
    expect(tip()).toBeNull();
    // a pointer that left before the delay shows nothing
    enter(a);
    leave(a);
    act(() => { vi.advanceTimersByTime(TIP_DELAY * 2); });
    expect(tip()).toBeNull();
  });

  it("is drawn for an svg mark as for a button, and takes no pointer", () => {
    const b = host.querySelector("[data-b]")!;
    enter(b);
    act(() => { vi.advanceTimersByTime(TIP_DELAY); });
    expect(tip()!.textContent).toBe("second words");
    expect(tip()!.className).toContain("pointer-events-none");
  });

  it("goes on Escape, on a press and on the focus leaving, and comes for keyboard focus at once", () => {
    const a = host.querySelector<HTMLElement>("[data-a]")!;
    act(() => a.focus());
    expect(tip()!.textContent).toBe("first words");
    act(() => { window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" })); });
    expect(tip()).toBeNull();
    act(() => a.blur());
    act(() => { document.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", bubbles: true })); a.focus(); });
    expect(tip()).not.toBeNull();
    act(() => a.blur());
    expect(tip()).toBeNull();
    enter(a);
    act(() => { vi.advanceTimersByTime(TIP_DELAY); });
    expect(tip()).not.toBeNull();
    act(() => { a.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, pointerType: "mouse" })); });
    expect(tip()).toBeNull();
  });

  it("keeps the next anchor's card when the pointer leaves the one before it", () => {
    const a = host.querySelector("[data-a]")!;
    const b = host.querySelector("[data-b]")!;
    enter(a);
    act(() => { vi.advanceTimersByTime(TIP_DELAY); });
    enter(b, a);
    leave(a, b);
    act(() => { vi.advanceTimersByTime(TIP_DELAY); });
    expect(tip()!.textContent).toBe("second words");
  });
});
