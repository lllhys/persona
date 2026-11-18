// @vitest-environment jsdom
// Mobile WebViews often skip pagehide when backgrounded or locked: visibilitychange to hidden flushes the pending
// draft at once, a hidden/visible cycle with nothing unsaved does no storage work, and unmount removes the listener.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Composer, type Outgoing } from "./Composer";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const render = (draftKey: string, onSend: (m: Outgoing) => Promise<boolean> = async () => true) =>
  act(() => root.render(<Composer draftKey={draftKey} placeholder="" onSend={onSend} />));

const typeInto = (text: string) =>
  act(() => {
    const el = host.querySelector("textarea")!;
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(el, text);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });

const setVisibility = (state: DocumentVisibilityState) => {
  Object.defineProperty(document, "visibilityState", { value: state, configurable: true });
  act(() => document.dispatchEvent(new Event("visibilitychange")));
};

const sleep = (ms: number) =>
  act(async () => {
    await new Promise((ok) => setTimeout(ok, ms));
  });

describe("Composer draft flush on visibilitychange", () => {
  it("persists the pending draft on hidden without the debounce or pagehide", () => {
    render("hv");
    typeInto("last words");
    expect(localStorage.getItem("draft:hv")).toBeNull(); // the 250 ms debounce is still pending
    setVisibility("hidden");
    expect(localStorage.getItem("draft:hv")).toBe("last words");
  });

  it("does not flush while becoming visible, only on hidden", () => {
    render("vis");
    typeInto("not yet");
    setVisibility("visible");
    expect(localStorage.getItem("draft:vis")).toBeNull();
    setVisibility("hidden");
    expect(localStorage.getItem("draft:vis")).toBe("not yet");
  });

  it("performs no storage write on a hidden/visible cycle when nothing is unsaved", async () => {
    render("idle");
    await sleep(300); // let the debounce settle: nothing unsaved remains
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const removeItem = vi.spyOn(Storage.prototype, "removeItem");
    setVisibility("hidden");
    setVisibility("visible");
    expect(setItem).not.toHaveBeenCalled();
    expect(removeItem).not.toHaveBeenCalled();
    setItem.mockRestore();
    removeItem.mockRestore();
  });

  it("removes its visibility listener when the box unmounts", () => {
    render("gone");
    typeInto("pending");
    act(() => root.unmount()); // unmount itself flushes the pending draft
    expect(localStorage.getItem("draft:gone")).toBe("pending");
    localStorage.clear();
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    setVisibility("hidden");
    expect(setItem).not.toHaveBeenCalled();
    setItem.mockRestore();
    root = createRoot(host); // afterEach unmounts again
  });
});
