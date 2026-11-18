// @vitest-environment jsdom
// Pictures in replies (#9475): with the hub's remote image route a reply's pictures load through it at once, with no
// gate and no request to their host; what other people wrote waits for a click (then the hub); a hub without the route
// keeps #8162's gate. Runs are one picture or squares, and the viewer pages through every picture of the reply.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { getState } from "../lib/store";
import { REMOTE_IMAGES_FEATURE } from "../lib/reply-images";
import { Markdown } from "./Markdown";
import { forgetMarkdown } from "./markdown-cache";
import { loadReplyImages } from "./reply-images-lazy";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
// the image viewer measures its stage; jsdom has no ResizeObserver
(globalThis as { ResizeObserver?: unknown }).ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

const reply = [
  "**Kitchen** (same post)",
  "![kitchen](https://img.example.com/a.jpg)",
  "![bath](https://img.example.com/b.jpg)",
  "It has a washer.",
  "",
  "![room](https://img.example.com/c.jpg)",
  "",
  "an icon ![icon](https://img.example.com/i.png) in a line",
].join("\n");

let host: HTMLDivElement;
let root: Root;
let srcSets: string[];
let srcDesc: PropertyDescriptor | undefined;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};

beforeEach(async () => {
  forgetMarkdown();
  await loadReplyImages();
  await import("./ReplyImages");
  await import("./ExternalImageGate");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  srcSets = [];
  srcDesc = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, "src");
  Object.defineProperty(HTMLImageElement.prototype, "src", {
    configurable: true,
    enumerable: srcDesc?.enumerable,
    get: srcDesc?.get,
    set(v: string) {
      srcSets.push(v);
      srcDesc?.set?.call(this, v);
    },
  });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  if (srcDesc) Object.defineProperty(HTMLImageElement.prototype, "src", srcDesc);
  Object.assign(getState(), { features: [] });
});

/** Until `ready` holds (the runs' chunk resolves at its own pace under a loaded test run), at most about 3 s. */
const until = async (ready: () => boolean) => {
  for (let i = 0; i < 300 && !ready(); i++) await act(async () => { await new Promise((r) => setTimeout(r, 10)); });
};
const draw = async (text: string, askImages = false) => {
  await act(async () => root.render(<Markdown text={text} lineBreaks askImages={askImages} />));
  await flush();
  await until(() => host.querySelector("[data-reply-images]") !== null);
};
const thirdParty = () => srcSets.filter((s) => s.includes("img.example.com") && !s.includes("/api/v1/remote-image"));

describe("with the hub's remote image route", () => {
  beforeEach(() => Object.assign(getState(), { features: [REMOTE_IMAGES_FEATURE] }));

  it("draws a reply's pictures at once through the hub, never from their host", async () => {
    await draw(reply);
    const runs = host.querySelectorAll("[data-reply-images]");
    expect(runs).toHaveLength(2); // the pair under the line of text, and the lone one
    expect(runs[0].querySelectorAll("[data-reply-picture]")).toHaveLength(2);
    expect(runs[1].querySelectorAll("[data-reply-picture]")).toHaveLength(1);
    expect(host.querySelector("[data-external-image]")).toBeNull();
    expect(thirdParty()).toEqual([]);
    expect(srcSets.filter((s) => s.startsWith("/api/v1/remote-image?url=https%3A%2F%2Fimg.example.com%2F"))).toHaveLength(4);
    // the text around them stays, cut where it was
    expect(host.textContent).toContain("Kitchen (same post)");
    expect(host.textContent).toContain("It has a washer.");
    // the icon stays in its line, the line's height
    const icon = host.querySelector<HTMLImageElement>('img[alt="icon"]');
    expect(icon?.style.height).toBe("1.2em");
  });

  it("draws eight as six squares, the sixth counting two more, and a lone one its fixed height", async () => {
    await draw(Array.from({ length: 8 }, (_, i) => `![p${i}](https://img.example.com/${i}.jpg)`).join("\n") + "\n\ntext\n\n![one](https://img.example.com/one.jpg)");
    const [squares, lone] = [...host.querySelectorAll("[data-reply-images]")];
    expect(squares.querySelectorAll("[data-reply-picture]")).toHaveLength(6);
    expect(squares.textContent).toBe("+2");
    const box = lone.querySelector<HTMLElement>("[data-reply-picture]")!;
    expect(box.style.height).toBe("300px"); // its final height before it arrives (jsdom drops the width's min())
  });

  it("opens the viewer on every picture of the reply, at the one clicked", async () => {
    await draw(reply);
    await act(async () => host.querySelectorAll<HTMLElement>("[data-reply-picture]")[2].click());
    await act(async () => { await import("../views/chat/ImageViewer"); });
    await until(() => (document.body.textContent ?? "").includes("3 / 4"));
    expect(document.body.textContent).toContain("3 / 4");
  });

  it("keeps what other people wrote waiting for a click, then loads it through the hub", async () => {
    await draw(reply, true);
    expect(srcSets).toEqual([]);
    const asked = host.querySelectorAll<HTMLElement>('[data-reply-picture="asked"]');
    expect(asked).toHaveLength(2);
    await act(async () => asked[0].click());
    await flush();
    expect(srcSets).toHaveLength(2);
    expect(srcSets.every((s) => s.startsWith("/api/v1/remote-image?"))).toBe(true);
    expect(host.querySelector("[data-external-image=gate]")).not.toBeNull(); // the inline icon keeps its gate
  });
});

describe("without the route (an older hub)", () => {
  it("loads nothing until asked, as #8162 had it", async () => {
    await draw(reply);
    expect(srcSets).toEqual([]);
    expect(host.querySelectorAll('[data-reply-picture="asked"]')).toHaveLength(2);
    expect(host.querySelector("[data-external-image=gate]")).not.toBeNull();
  });
});
