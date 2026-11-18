// @vitest-environment jsdom
// The mis-tap guard engine (#8161): a fresh answer swallows a pointer click for TAP_GUARD_MS, keyboard and programmatic
// clicks always pass, the open-grace exempts what a deliberately opened view shows first, only a changed key restarts
// the window, and an answer first scrolled into view restarts it then.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { setTapGuardClockForTest, TAP_GUARD_ATTR, TAP_GUARD_MS, TapGuardSurface, useTapGuard } from "./tap-guard";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

type IOCallback = (records: Array<{ target: Element; isIntersecting: boolean }>) => void;
const observers: Array<{ callback: IOCallback; targets: Set<Element> }> = [];

let now = 0;
/** A container with the same capture listener useTapGuard installs, so the surface-only tests block real clicks too. */
let box: HTMLDivElement;
beforeEach(() => {
  now = 1000;
  setTapGuardClockForTest(() => now);
  document.body.replaceChildren();
  observers.length = 0;
  (globalThis as { IntersectionObserver?: typeof IntersectionObserver }).IntersectionObserver = class {
    private entry: { callback: IOCallback; targets: Set<Element> };
    constructor(callback: IOCallback) {
      this.entry = { callback, targets: new Set() };
      observers.push(this.entry);
    }
    observe(target: Element) {
      this.entry.targets.add(target);
    }
    unobserve(target: Element) {
      this.entry.targets.delete(target);
    }
    disconnect() {
      this.entry.targets.clear();
    }
  } as unknown as typeof IntersectionObserver;
  box = document.createElement("div");
  document.body.append(box);
});
afterEach(() => {
  setTapGuardClockForTest(null);
});

function intersect(target: Element, isIntersecting: boolean): void {
  for (const observer of observers) if (observer.targets.has(target)) observer.callback([{ target, isIntersecting }]);
}

function answer(label = "Allow"): { el: HTMLButtonElement; taps: () => number } {
  const el = document.createElement("button");
  el.textContent = label;
  let taps = 0;
  el.addEventListener("click", () => {
    taps++;
  });
  box.append(el);
  return { el, taps: () => taps };
}

/** A finger or mouse click carries press detail; a keyboard or programmatic click carries none. */
function tap(el: HTMLElement, detail = 1): void {
  el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, detail }));
}

function opened(grace = 0): TapGuardSurface {
  const surface = new TapGuardSurface(grace);
  box.addEventListener("click", (event) => {
    if (event.detail !== 0 && surface.blocks(event)) {
      event.preventDefault();
      event.stopPropagation();
    }
  }, true);
  surface.view("pane-1");
  return surface;
}

describe("TapGuardSurface", () => {
  it("swallows a tap inside the window and admits one just past it", () => {
    const surface = opened();
    const { el, taps } = answer();
    surface.guard("q1", [el]);
    expect(el.hasAttribute(TAP_GUARD_ATTR)).toBe(true);
    now += 300;
    tap(el);
    expect(taps()).toBe(0);
    now += TAP_GUARD_MS - 300;
    tap(el);
    expect(taps()).toBe(1);
  });

  it("changes nothing about the look: no class, style, disabled or aria state", () => {
    const surface = opened();
    const { el } = answer();
    el.className = "approval-act approval-allow";
    surface.guard("q1", [el]);
    expect(el.className).toBe("approval-act approval-allow");
    expect(el.getAttribute("style")).toBeNull();
    expect(el.disabled).toBe(false);
    expect(el.getAttribute("aria-disabled")).toBeNull();
  });

  it("never holds keyboard and programmatic clicks (detail 0)", () => {
    const surface = opened();
    const { el, taps } = answer();
    surface.guard("q1", [el]);
    el.click();
    tap(el, 0);
    expect(taps()).toBe(2);
  });

  it("exempts what the view shows inside its open-grace window, then guards new answers", () => {
    const surface = opened(1000);
    const early = answer();
    surface.guard("row-a", [early.el]);
    expect(early.el.hasAttribute(TAP_GUARD_ATTR)).toBe(false);
    tap(early.el);
    expect(early.taps()).toBe(1);
    now += 1100;
    const late = answer("Deny");
    surface.guard("row-b", [late.el]);
    tap(late.el);
    expect(late.taps()).toBe(0);
    now += TAP_GUARD_MS;
    tap(late.el);
    expect(late.taps()).toBe(1);
  });

  it("restarts the window when the key changes, not when the same answers redraw", () => {
    const surface = opened();
    const first = answer();
    surface.guard("q1", [first.el]);
    now += 300;
    // output streaming above redraws the card: same key, a rebuilt element
    first.el.remove();
    const redrawn = answer();
    surface.guard("q1", [redrawn.el]);
    now += 200; // 500 ms after the key first appeared: the original window is over
    tap(redrawn.el);
    expect(redrawn.taps()).toBe(1);
    // the option set changes: a fresh window starts now
    const changed = answer("Deny");
    surface.guard("q1b", [changed.el]);
    now += 300;
    tap(changed.el);
    expect(changed.taps()).toBe(0);
    now += TAP_GUARD_MS - 300;
    tap(changed.el);
    expect(changed.taps()).toBe(1);
  });

  it("never keeps an answer locked past the window even while it redraws repeatedly", () => {
    const surface = opened();
    let current = answer();
    surface.guard("q1", [current.el]);
    for (let i = 0; i < 10; i++) {
      now += 100;
      current.el.remove();
      current = answer();
      surface.guard("q1", [current.el]);
    }
    tap(current.el);
    expect(current.taps()).toBe(1);
  });

  it("starts a view over when the user opens another one", () => {
    const surface = opened(1000);
    surface.view("pane-2");
    const { el, taps } = answer();
    surface.guard("q1", [el]); // the new view's first answer is part of opening it
    expect(el.hasAttribute(TAP_GUARD_ATTR)).toBe(false);
    tap(el);
    expect(taps()).toBe(1);
  });

  it("restarts the window when an off-screen answer first scrolls into view", () => {
    const surface = opened();
    const { el, taps } = answer();
    surface.guard("q1", [el]);
    intersect(el, false);
    now += 2000;
    intersect(el, true);
    expect(el.hasAttribute(TAP_GUARD_ATTR)).toBe(true);
    now += 300;
    tap(el);
    expect(taps()).toBe(0);
    now += TAP_GUARD_MS - 300;
    tap(el);
    expect(taps()).toBe(1);
    // leaving and re-entering the viewport does not restart it a second time
    intersect(el, false);
    intersect(el, true);
    tap(el);
    expect(taps()).toBe(2);
  });

  it("keeps its first window when the element was on screen the whole time", () => {
    const surface = opened();
    const { el, taps } = answer();
    surface.guard("q1", [el]);
    now += TAP_GUARD_MS + 10;
    intersect(el, true);
    tap(el);
    expect(taps()).toBe(1);
  });
});

describe("useTapGuard", () => {
  let host: HTMLDivElement;
  let root: Root;
  let surface: TapGuardSurface;

  beforeEach(() => {
    surface = opened();
    host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  function Guarded({ keyName }: { keyName: string }) {
    const { ref } = useTapGuard(surface, keyName, "pane-1", "button");
    return (
      <div ref={ref}>
        <button type="button" onClick={() => host.dataset.taps = String(Number(host.dataset.taps ?? 0) + 1)}>
          Allow
        </button>
      </div>
    );
  }

  it("swallows an early pointer click at the container and admits a later one", async () => {
    await act(async () => root.render(<Guarded keyName="q1" />));
    const button = host.querySelector("button")!;
    tap(button);
    expect(host.dataset.taps ?? "0").toBe("0");
    now += TAP_GUARD_MS;
    tap(button);
    expect(host.dataset.taps).toBe("1");
  });

  it("keeps keyboard clicks working while the window runs", async () => {
    await act(async () => root.render(<Guarded keyName="q1" />));
    host.querySelector("button")!.click();
    expect(host.dataset.taps).toBe("1");
  });

  it("re-arms on a key change but not on a redraw with the same key", async () => {
    await act(async () => root.render(<Guarded keyName="q1" />));
    now += 300;
    await act(async () => root.render(<Guarded keyName="q1" />));
    now += 200; // 500 ms after the first guard: a redraw did not restart it
    tap(host.querySelector("button")!);
    expect(host.dataset.taps).toBe("1");
    await act(async () => root.render(<Guarded keyName="q2" />));
    tap(host.querySelector("button")!);
    expect(host.dataset.taps).toBe("1");
    now += TAP_GUARD_MS;
    tap(host.querySelector("button")!);
    expect(host.dataset.taps).toBe("2");
  });
});
