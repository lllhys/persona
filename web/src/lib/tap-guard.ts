// Mis-tap guard (#8161): an approval or question card that paints under a pointer already landing must not take the tap
// aimed at the previous screen. Its answer buttons swallow pointer and touch clicks for TAP_GUARD_MS after the card or
// its option set appears; the look never changes (an unstyled attribute is the only mark, and `disabled` is not used).
// Keyboard and programmatic clicks (MouseEvent.detail === 0, as element.click() and an Enter on a focused button send)
// always pass. A view the user deliberately opened is exempt for its open-grace window, so navigation never feels
// slow. The window restarts only when the guarded key changes (a new card or option set) or when an element that
// appeared off screen first scrolls into view; unrelated re-renders with the same key keep the window they had.
import { useEffect, useLayoutEffect, useRef, type Ref, type RefCallback, type RefObject } from "react";

export const TAP_GUARD_MS = 450;
/** Carries no style of its own: tests and debugging can see what is held without any visual change. */
export const TAP_GUARD_ATTR = "data-tap-guard";
/** Keys a surface remembers; the oldest are forgotten first. */
const KEY_LIMIT = 200;

type Viewport = "unknown" | "outside" | "seen";

type Entry = {
  until: number;
  /** Off screen when it appeared: the window starts again when it is first seen. */
  viewport: Viewport;
  els: Set<HTMLElement>;
};

let clock: () => number = () => performance.now();

/** Pin the clock in tests; production code always reads performance.now(). */
export function setTapGuardClockForTest(next: (() => number) | null): void {
  clock = next ?? (() => performance.now());
}

const active = (entry: Entry): boolean => entry.until > clock();

function newEntry(guarded: boolean): Entry {
  return guarded
    ? { until: clock() + TAP_GUARD_MS, viewport: "unknown", els: new Set() }
    : { until: 0, viewport: "seen", els: new Set() };
}

/**
 * One place answers can appear on their own (a chat's card stack, the overview). `view` names what the user opened;
 * the first keys seen inside the surface's open-grace window belong to that opening and are not guarded.
 */
export class TapGuardSurface {
  private opened: string | null = null;
  private openedAt = 0;
  private readonly keys = new Map<string, Entry>();
  private readonly held = new Map<HTMLElement, Entry>();
  private observer: IntersectionObserver | null = null;
  private readonly observed = new WeakMap<Element, Entry>();

  constructor(private readonly openGraceMs = 0) {}

  /** The user is looking at `id`; a different id is a view they deliberately opened. */
  view(id: string): void {
    if (id === this.opened) return;
    this.reset();
    this.opened = id;
    this.openedAt = clock();
  }

  /** Forget every answer this surface held: tests start with a clean, just-opened view. */
  reset(): void {
    for (const el of this.held.keys()) el.removeAttribute(TAP_GUARD_ATTR);
    this.held.clear();
    this.keys.clear();
    this.opened = null;
  }

  /** Holds `elements` for `key`, timed from when that key first appeared on this view. */
  guard(key: string, elements: Iterable<HTMLElement>): void {
    let entry = this.keys.get(key);
    if (!entry) {
      entry = newEntry(clock() - this.openedAt >= this.openGraceMs);
      this.keys.set(key, entry);
      if (this.keys.size > KEY_LIMIT) this.keys.delete(this.keys.keys().next().value!);
    }
    for (const el of elements) this.apply(el, entry);
  }

  /** True when a pointer or touch click lands on an answer this surface still holds. */
  blocks(event: MouseEvent): boolean {
    if (event.detail === 0) return false;
    const target = event.target;
    for (const [el, entry] of this.held) {
      if (!el.isConnected || !active(entry)) {
        this.release(el);
        continue;
      }
      if (target instanceof Node && el.contains(target)) return true;
    }
    return false;
  }

  private release(el: HTMLElement): void {
    this.held.delete(el);
    el.removeAttribute(TAP_GUARD_ATTR);
  }

  private hold(el: HTMLElement, entry: Entry): void {
    const left = entry.until - clock();
    if (left <= 0) {
      this.release(el);
      return;
    }
    this.held.set(el, entry);
    el.setAttribute(TAP_GUARD_ATTR, "");
    setTimeout(() => {
      if (this.held.get(el) === entry) this.hold(el, entry);
    }, Math.ceil(left));
  }

  private onViewport(records: IntersectionObserverEntry[]): void {
    for (const record of records) {
      const entry = this.observed.get(record.target);
      if (entry && entry.viewport !== "seen" && !record.isIntersecting) {
        entry.viewport = "outside";
        continue;
      }
      this.observed.delete(record.target);
      this.observer?.unobserve(record.target);
      if (!entry || entry.viewport === "seen") continue;
      const restart = entry.viewport === "outside";
      entry.viewport = "seen";
      if (!restart) continue;
      entry.until = clock() + TAP_GUARD_MS;
      for (const el of entry.els) if (el.isConnected) this.hold(el, entry);
    }
  }

  private watch(el: HTMLElement, entry: Entry): void {
    if (entry.viewport === "seen" || typeof IntersectionObserver === "undefined") return;
    this.observer ??= new IntersectionObserver((records) => this.onViewport(records));
    this.observed.set(el, entry);
    this.observer.observe(el);
  }

  private apply(el: HTMLElement, entry: Entry): void {
    for (const old of entry.els) if (!old.isConnected) entry.els.delete(old);
    entry.els.add(el);
    // an element rebuilt while its entry waits off screen keeps that entry's verdict
    if (entry.viewport === "unknown" || entry.viewport === "outside") this.watch(el, entry);
    if (active(entry)) this.hold(el, entry);
  }
}

export interface TapGuardHandle {
  ref: RefObject<HTMLDivElement | null>;
}

/** The address of the view the user deliberately opened, as a stable id per page; empty during static rendering. */
export function currentViewId(): string {
  return typeof location === "undefined" ? "" : location.pathname + location.search;
}

/**
 * Stamps the deliberately opened view when its page mounts, even before any answer is on screen: a row that arrives
 * later (the hub's first read is still landing) is then judged against the opening's grace, not mistaken for it.
 */
export function useTapGuardView(surface: TapGuardSurface, viewId: string): void {
  useEffect(() => {
    surface.view(viewId);
  }, [surface, viewId]);
}

/**
 * Guards the answer elements under one container for `key` on `surface`. The container is marked every render (a React
 * redraw may replace its buttons while the key stays the same); a capture-phase click listener swallows the taps the
 * surface still holds, before React's own click handling. Keyboard clicks carry detail 0 and pass untouched.
 */
export function useTapGuard(surface: TapGuardSurface, key: string, viewId: string, selector: string): TapGuardHandle {
  const ref = useRef<HTMLDivElement | null>(null);
  useLayoutEffect(() => {
    const root = ref.current;
    if (!root) return;
    surface.view(viewId);
    surface.guard(key, root.querySelectorAll<HTMLElement>(selector));
  });
  useEffect(() => {
    const root = ref.current;
    if (!root) return;
    const block = (event: MouseEvent) => {
      if (event.detail !== 0 && surface.blocks(event)) {
        event.preventDefault();
        event.stopPropagation();
      }
    };
    root.addEventListener("click", block, true);
    return () => root.removeEventListener("click", block, true);
  }, [surface]);
  return { ref };
}

/** Joins the refs several wrappers need on one element (the card's keyboard root and its tap-guard container). */
export function mergeRefs<T>(...refs: Array<Ref<T> | undefined>): RefCallback<T> {
  return (el) => {
    for (const r of refs) {
      if (typeof r === "function") r(el);
      else if (r) r.current = el;
    }
  };
}
