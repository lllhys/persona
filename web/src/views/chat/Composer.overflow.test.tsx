// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { getComposerFiles } from "../../lib/composer-files";
import { sendDraft } from "../../lib/drafts";
import { Composer } from "./Composer";

type PendingUpload = { name: string; resolve: (path: string) => void };
const uploads: PendingUpload[] = [];
vi.mock("../../lib/file-ops", () => ({
  uploadAttachment: (_mid: string, file: File) => new Promise((resolve) => {
    uploads.push({ name: file.name, resolve: (path) => resolve({ path, name: file.name, size: file.size }) });
  }),
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  uploads.length = 0;
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const until = async (check: () => void) => {
  for (let i = 0; i < 200; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5));
    });
    try {
      return check();
    } catch {
      // Wait for the dynamic upload import and upload queue.
    }
  }
  check();
};

const pick = (files: File[]) => act(() => {
  const input = host.querySelector<HTMLInputElement>('input[type="file"]')!;
  Object.defineProperty(input, "files", { value: files, configurable: true });
  input.dispatchEvent(new Event("change", { bubbles: true }));
});
const resolveUpload = async (index: number, path: string) => {
  await until(() => expect(uploads.length).toBeGreaterThan(index));
  await act(async () => uploads[index].resolve(path));
};
const fillNextDraft = async (key: string) => {
  const offset = uploads.length;
  const next = Array.from({ length: 10 }, (_, i) => new File([`next-${i}`], `next-${i}.txt`, { type: "text/plain" }));
  await pick(next);
  for (let i = 0; i < 10; i++) await resolveUpload(offset + i, `/u/next-${i}.txt`);
  await until(() => expect(getComposerFiles(key).every((file) => file.state === "ready")).toBe(true));
};

it("keeps the original attachment recoverable when a failed send overflows the next draft", async () => {
  const key = "overflow-ui";
  let finishFirst!: (ok: boolean) => void;
  const onSend = vi.fn(() => new Promise<boolean>((resolve) => { finishFirst = resolve; }));
  act(() => root.render(<Composer draftKey={key} machineId="m1" placeholder="" onSend={onSend} />));

  await pick([new File(["first"], "original.txt", { type: "text/plain" })]);
  await resolveUpload(0, "/u/original.txt");
  act(() => host.querySelector<HTMLButtonElement>('button[data-testid="composer-send"]')!.click());
  expect(onSend).toHaveBeenCalledTimes(1);
  expect(host.querySelectorAll("[data-testid=composer-files] > span")).toHaveLength(0);

  await fillNextDraft(key);
  await act(async () => finishFirst(false));

  expect(host.querySelectorAll("[data-testid=composer-files] > span")).toHaveLength(10);
  const draftText = host.querySelector<HTMLTextAreaElement>("textarea")!.value;
  const recoverablePaths = [...getComposerFiles(key).map((file) => file.file?.path ?? ""), draftText];
  expect(recoverablePaths.join("\n")).toContain("/u/original.txt");
});

it("keeps edit overflow as file tags when the draft is already full", async () => {
  const key = "overflow-edit";
  act(() => root.render(<Composer draftKey={key} machineId="m1" placeholder="" onSend={vi.fn(async () => true)} />));
  await fillNextDraft(key);
  const tag = `<persona-file kind="file" path="/u/original.txt" name="original.txt" size="5"/>`;
  act(() => sendDraft(key, { type: "replace", text: `Edit this\n\n${tag}` }));
  expect(host.querySelector<HTMLTextAreaElement>("textarea")!.value).toContain(tag);
  expect(getComposerFiles(key)).toHaveLength(10);
});
