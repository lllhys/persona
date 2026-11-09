// @vitest-environment jsdom
// The upload dialog (#2404): nothing goes before there is an image and a name, a file the hub can't read or one too
// small is refused at once, the body carries the crop and the trimmed texts, a second click sends nothing more, the
// progress shows, and the hub's refusal is shown as it is.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../../i18n";
import { ApiError } from "../../lib/api";
import type { PickedImage, UploadAnswer, UploadBody } from "../../lib/look-upload";
import { LookUploadDialog, type LookUploadDeps } from "./LookUpload";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["setTimeout"] });
  vi.spyOn(Element.prototype, "getClientRects").mockReturnValue([{}] as unknown as DOMRectList);
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const image = (w: number, h: number): PickedImage => ({ source: {} as CanvasImageSource, size: { w, h }, release: vi.fn() });

interface Sent {
  body: UploadBody;
  progress?: (sent: number, total: number) => void;
  resolve: (a: UploadAnswer) => void;
  reject: (e: unknown) => void;
}

function setup(over: Partial<LookUploadDeps> = {}, size = { w: 800, h: 600 }) {
  const sent: Sent[] = [];
  const picked = image(size.w, size.h);
  const deps: LookUploadDeps = {
    read: vi.fn(async () => picked),
    render: vi.fn(() => ({ image: "Q1JPUA==", mime: "image/png", side: 600 })),
    send: vi.fn((body: UploadBody, opts: { onProgress?: (a: number, b: number) => void } = {}) =>
      new Promise<UploadAnswer>((resolve, reject) => sent.push({ body, progress: opts.onProgress, resolve, reject }))),
    ...over,
  };
  const onClose = vi.fn();
  const onSend = vi.fn();
  const onUploaded = vi.fn();
  act(() => root.render(<LookUploadDialog open onClose={onClose} onSend={onSend} onUploaded={onUploaded} deps={deps} />));
  return { deps, sent, onClose, onSend, onUploaded, picked };
}

const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]')!;
const button = (label: string) => [...dialog().querySelectorAll("button")].find((b) => b.textContent?.includes(label))!;
const uploadButton = () => button(t("assistant-lookupload.upload"));
const field = (label: string) => document.getElementById([...dialog().querySelectorAll("label")].find((l) => l.textContent === label)!.htmlFor) as HTMLInputElement;
const alertText = () => dialog().querySelector('[role="alert"]')?.textContent ?? "";

async function pick(file: File) {
  const input = dialog().querySelector<HTMLInputElement>('[data-testid="look-upload-file"]')!;
  Object.defineProperty(input, "files", { value: [file], configurable: true });
  await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));
}

function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

const png = () => new File(["x"], "me.png", { type: "image/png" });

describe("the upload dialog", () => {
  it("asks for an image first, then crops it with a zoom slider", async () => {
    const { deps } = setup();
    expect(dialog().querySelector('[data-testid="look-upload-drop"]')).not.toBeNull();
    expect(uploadButton().disabled).toBe(true);
    await pick(png());
    expect(deps.read).toHaveBeenCalledTimes(1);
    expect(dialog().querySelector('[data-testid="look-upload-crop"]')).not.toBeNull();
    const zoom = dialog().querySelector<HTMLInputElement>('input[type="range"]')!;
    expect(zoom.getAttribute("aria-label")).toBe(t("assistant-lookupload.zoom"));
    expect(Number(zoom.max)).toBe(8);
    // no name yet
    expect(uploadButton().disabled).toBe(true);
  });

  it("refuses a file the hub can't read and an image too small, before anything goes", async () => {
    const { deps } = setup({}, { w: 40, h: 300 });
    await pick(new File(["x"], "notes.txt", { type: "text/plain" }));
    expect(alertText()).toBe(t("assistant-lookupload.not_image"));
    expect(deps.read).not.toHaveBeenCalled();
    await pick(png());
    expect(alertText()).toBe(t("assistant-lookupload.too_small", { px: 64 }));
    expect(dialog().querySelector('[data-testid="look-upload-crop"]')).toBeNull();
  });

  it("says so when the browser can't decode the file", async () => {
    setup({ read: vi.fn(async () => Promise.reject(new Error("decode"))) });
    await pick(png());
    expect(alertText()).toBe(t("assistant-lookupload.not_image"));
  });

  it("sends the crop with the trimmed name and description once, shows the progress, and hands over the answer", async () => {
    const { deps, sent, onClose, onSend, onUploaded, picked } = setup();
    await pick(png());
    type(field(t("assistant-lookupload.name")), "  Doudou  ");
    type(field(t("assistant-lookupload.subtitle")), " A round one ");
    expect(uploadButton().disabled).toBe(false);
    await act(async () => uploadButton().click());
    await act(async () => uploadButton().click()); // a second click while it is busy sends nothing
    await act(async () => vi.advanceTimersByTime(50));
    // the picker hears as the request leaves, before the answer
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend.mock.invocationCallOrder[0]).toBeLessThan((deps.send as ReturnType<typeof vi.fn>).mock.invocationCallOrder[0]);
    expect(deps.render).toHaveBeenCalledTimes(1);
    expect((deps.render as ReturnType<typeof vi.fn>).mock.calls[0].slice(1)).toEqual([picked.size, { x: 400, y: 300, zoom: 1 }]);
    expect(sent).toHaveLength(1);
    expect(sent[0].body).toEqual({ name: "Doudou", subtitle: "A round one", image: "Q1JPUA==", choose: true });
    expect(uploadButton().disabled).toBe(true);
    expect(uploadButton().getAttribute("aria-busy")).toBe("true");

    act(() => sent[0].progress!(50, 100));
    expect(document.querySelector('[role="progressbar"]')?.getAttribute("aria-valuenow")).toBe("50");
    expect(document.body.textContent).toContain(t("assistant-lookupload.uploading", { percent: 50 }));
    act(() => sent[0].progress!(100, 100));
    expect(document.body.textContent).toContain(t("assistant-lookupload.making"));

    const answer = { look: { id: "u-1", source: "hub", name: "Doudou", app_icon: "persona", uploaded: true }, current: { id: "u-1", effective: "u-1" } } as UploadAnswer;
    await act(async () => sent[0].resolve(answer));
    expect(onUploaded).toHaveBeenCalledWith(answer);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("shows the hub's refusal as it is and lets the user try again", async () => {
    const { sent } = setup();
    await pick(png());
    type(field(t("assistant-lookupload.name")), "Doudou");
    await act(async () => uploadButton().click());
    await act(async () => vi.advanceTimersByTime(50));
    await act(async () => sent[0].reject(new ApiError(409, "Already 50 uploaded avatars", { error: "Already 50 uploaded avatars", code: "too_many" })));
    expect(alertText()).toBe("Already 50 uploaded avatars");
    expect(uploadButton().disabled).toBe(false);
    await act(async () => uploadButton().click());
    await act(async () => vi.advanceTimersByTime(50));
    expect(sent).toHaveLength(2);
    expect(alertText()).toBe("");
  });

  it("says so when the crop can't be drawn, and sends nothing", async () => {
    const { sent } = setup({ render: vi.fn(() => { throw new Error("canvas"); }) });
    await pick(png());
    type(field(t("assistant-lookupload.name")), "Doudou");
    await act(async () => uploadButton().click());
    await act(async () => vi.advanceTimersByTime(50));
    expect(alertText()).toBe(t("assistant-lookupload.export_failed", { error: "canvas" }));
    expect(sent).toHaveLength(0);
    expect(uploadButton().disabled).toBe(false);
  });

  it("holds a name past 40 characters back, and counts characters the way the hub does", async () => {
    setup();
    await pick(png());
    const name = field(t("assistant-lookupload.name"));
    type(name, "x".repeat(41));
    expect(uploadButton().disabled).toBe(true);
    expect(name.getAttribute("aria-invalid")).toBe("true");
    expect(document.getElementById(name.getAttribute("aria-describedby")!)?.textContent).toBe("41/40");
    type(name, "😀".repeat(40));
    expect(uploadButton().disabled).toBe(false);
  });

  it("stops the request when it is closed", async () => {
    let signal: AbortSignal | undefined;
    const { onClose, onUploaded } = setup({
      send: vi.fn((_: UploadBody, opts: { signal?: AbortSignal } = {}) => {
        signal = opts.signal;
        return new Promise<UploadAnswer>(() => {});
      }),
    });
    await pick(png());
    type(field(t("assistant-lookupload.name")), "Doudou");
    await act(async () => uploadButton().click());
    await act(async () => vi.advanceTimersByTime(50));
    await act(async () => button(t("common.cancel")).click());
    expect(signal?.aborted).toBe(true);
    expect(onClose).toHaveBeenCalled();
    expect(onUploaded).not.toHaveBeenCalled();
  });

  it("stops the request when it goes without closing (the page left under it)", async () => {
    let signal: AbortSignal | undefined;
    const { onUploaded, picked } = setup({
      send: vi.fn((_: UploadBody, opts: { signal?: AbortSignal } = {}) => {
        signal = opts.signal;
        return new Promise<UploadAnswer>(() => {});
      }),
    });
    await pick(png());
    type(field(t("assistant-lookupload.name")), "Doudou");
    await act(async () => uploadButton().click());
    await act(async () => vi.advanceTimersByTime(50));
    expect(signal?.aborted).toBe(false);
    act(() => root.unmount());
    root = createRoot(host);
    expect(signal?.aborted).toBe(true);
    expect(picked.release).toHaveBeenCalledTimes(1);
    expect(onUploaded).not.toHaveBeenCalled();
  });

  it("lets go of an image decoded after it went", async () => {
    let decoded: (img: PickedImage) => void = () => {};
    setup({ read: vi.fn(() => new Promise<PickedImage>((done) => (decoded = done))) });
    await pick(png());
    act(() => root.unmount());
    root = createRoot(host);
    const late = image(800, 600);
    await act(async () => decoded(late));
    expect(late.release).toHaveBeenCalledTimes(1);
  });
});
