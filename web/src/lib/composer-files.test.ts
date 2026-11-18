// @vitest-environment jsdom
// A composer's files (#8361): uploaded three at a time, ready ones go with the message as tags after the words, a
// failed one stays to retry, an older hub's refusal of dest=attachment falls back to the temp directory.
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./api";
import { clearConversationDeletions, rememberConversationDeleted } from "./conversation-deleted";
import { parseUserText } from "./turns";

type Upload = { name: string; dest?: string; resolve: (v: { path: string; name: string; size: number }) => void; reject: (e: unknown) => void };
const uploads: Upload[] = [];
const features: string[] = [];

vi.mock("./file-ops", () => ({
  uploadAttachment: (_mid: string, file: File, opts: { dest?: string }) =>
    new Promise((resolve, reject) => uploads.push({ name: file.name, dest: opts.dest, resolve, reject })),
}));
vi.mock("./store", () => ({ getState: () => ({ features }) }));
vi.mock("../ui/atoms", () => ({ toast: vi.fn() }));

const {
  addFiles, fileDestination, fileXML, filesBusy, getComposerFiles, MAX_FILES, readyFiles, removeFile, restoreFiles, retryFile,
  splitFileTags, takeReadyFiles, TOGETHER, withFiles,
} = await import("./composer-files");

const file = (name: string, size = 7) => new File([new Uint8Array(size)], name);
// a few turns of the event loop: the upload code is imported on first use, then each upload settles
const settle = async () => {
  for (let i = 0; i < 20; i++) await new Promise((r) => setTimeout(r, 0));
};
let n = 0;
let key = "";

// the composer imports the upload code on first use: loaded here once, so each test waits only for its own uploads
beforeAll(async () => {
  await import("./file-ops");
});

beforeEach(() => {
  clearConversationDeletions();
  uploads.length = 0;
  features.length = 0;
  key = `m/s${++n}`;
});

afterEach(() => getComposerFiles(key).forEach((f) => removeFile(key, f.id)));

describe("composer files", () => {
  it("writes the words as typed, then a tag per file, which the chat reads back as cards", () => {
    const files = [
      { kind: "file", path: "/home/me/.persona/uploads/2026-11-15/q3 report.pdf", name: "q3 report.pdf", size: 2411724 },
      { kind: "file", path: "/home/me/.persona/uploads/2026-11-15/a&b.xlsx", name: "a&b.xlsx", size: 86 },
    ];
    const typed = "Compare these, the third quarter first";
    const message = withFiles(typed, files);
    expect(message).toBe(`${typed}\n\n${fileXML(files[0])}\n${fileXML(files[1])}`);
    expect(fileXML(files[1])).toBe('<persona-file kind="file" path="/home/me/.persona/uploads/2026-11-15/a&amp;b.xlsx" name="a&amp;b.xlsx" size="86"/>');
    const read = parseUserText(message);
    expect(read.text).toBe(typed);
    expect(read.files).toEqual(files.map((f) => ({ ...f, duration: undefined })));
    expect(splitFileTags(message)).toEqual({ text: typed, files: read.files });
    expect(withFiles(typed, [])).toBe(typed);
  });

  it("keeps files where the hub can, or may, and a health conversation's in its reports", () => {
    expect(fileDestination(undefined, ["file_upload", "upload_attachment"])).toBe("attachment");
    expect(fileDestination(undefined, [])).toBe("attachment");
    expect(fileDestination(undefined, ["file_upload"])).toBeUndefined();
    expect(fileDestination("health", ["upload_attachment"])).toBe("health");
  });

  it("uploads three at a time, the rest waiting, and hands the ready ones to the message", async () => {
    features.push("file_upload", "upload_attachment");
    addFiles(key, "m", ["a.pdf", "b.xlsx", "c.zip", "d.docx"].map((x) => file(x)));
    await settle(); // the upload code is loaded on first use
    expect(uploads.map((u) => u.name)).toEqual(["a.pdf", "b.xlsx", "c.zip"]);
    expect(uploads.every((u) => u.dest === "attachment")).toBe(true);
    expect(getComposerFiles(key).map((f) => f.state)).toEqual(["uploading", "uploading", "uploading", "waiting"]);
    expect(filesBusy(key)).toBe(true);
    uploads[0].resolve({ path: "/u/a.pdf", name: "a.pdf", size: 7 });
    await settle();
    expect(uploads.map((u) => u.name)).toEqual(["a.pdf", "b.xlsx", "c.zip", "d.docx"]);
    for (const u of uploads.slice(1)) u.resolve({ path: `/u/${u.name}`, name: u.name, size: 7 });
    await settle();
    expect(filesBusy(key)).toBe(false);
    expect(readyFiles(key).map((f) => f.path)).toEqual(["/u/a.pdf", "/u/b.xlsx", "/u/c.zip", "/u/d.docx"]);
    expect(takeReadyFiles(key)).toHaveLength(4);
    expect(getComposerFiles(key)).toEqual([]);
  });

  it("keeps a failed one to try again, and goes to the temp directory when an older hub refuses dest=attachment", async () => {
    addFiles(key, "m", [file("a.pdf"), file("b.pdf")]);
    await settle();
    uploads[0].reject(new ApiError(500, "no space left on device", {}));
    uploads[1].reject(new ApiError(400, "unknown dest", {}));
    await settle();
    expect(uploads[2]).toMatchObject({ name: "b.pdf", dest: undefined });
    uploads[2].resolve({ path: "/tmp/b-20261115-101010.pdf", name: "b-20261115-101010.pdf", size: 7 });
    await settle();
    const [a, b] = getComposerFiles(key);
    expect(a.state).toBe("failed");
    expect(b.file).toMatchObject({ path: "/tmp/b-20261115-101010.pdf", name: "b.pdf" });
    retryFile(key, a.id);
    expect(getComposerFiles(key)[0].state).toBe("uploading");
    await settle();
    uploads[3].resolve({ path: "/u/a.pdf", name: "a.pdf", size: 7 });
    await settle();
    expect(readyFiles(key).map((f) => f.name)).toEqual(["a.pdf", "b.pdf"]);
  });

  it("takes at most ten, refuses one over 200 MB, and puts back as many as fit", async () => {
    const big = file("big.mov");
    Object.defineProperty(big, "size", { value: 300 * 1024 * 1024 });
    const r = addFiles(key, "m", [big, ...Array.from({ length: 11 }, (_, i) => file(`${i}.txt`))]);
    expect(r.tooBig).toEqual(["big.mov"]);
    expect(r.tooMany).toBe(2);
    expect(getComposerFiles(key)).toHaveLength(MAX_FILES - 1);
    await settle();
    expect(uploads).toHaveLength(TOGETHER);
    const left = restoreFiles(key, [{ kind: "file", path: "/u/x", name: "x" }, { kind: "file", path: "/u/y", name: "y" }]);
    expect(left.map((f) => f.name)).toEqual(["y"]);
    expect(getComposerFiles(key)).toHaveLength(MAX_FILES);
  });
});

// Deleted conversations cannot regain file tiles from an in-flight upload or a refused send.
describe("deleted conversation files", () => {
  it("clears pending and ready files and rejects a late upload completion", async () => {
    addFiles(key, "m", [file("kept-ready.pdf"), file("pending-private.pdf")]);
    await settle();
    uploads[0].resolve({ path: "/synthetic/kept-ready.pdf", name: "kept-ready.pdf", size: 7 });
    await settle();
    expect(readyFiles(key)).toHaveLength(1);
    rememberConversationDeleted("m", key.slice(2));
    uploads[1].resolve({ path: "/synthetic/pending-private.pdf", name: "pending-private.pdf", size: 7 });
    await settle();
    expect(getComposerFiles(key)).toEqual([]);
    expect(readyFiles(key)).toEqual([]);
  });
  it("rejects a late restore and a new pick for a deleted source", async () => {
    rememberConversationDeleted("m", key.slice(2));
    restoreFiles(key, [{ kind: "file", path: "/synthetic/private.pdf", name: "private.pdf", size: 7 }]);
    addFiles(key, "m", [file("new-private.pdf")]);
    await settle();
    expect(getComposerFiles(key)).toEqual([]);
    expect(uploads).toEqual([]);
  });
});
