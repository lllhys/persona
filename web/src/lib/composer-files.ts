// A composer's files (#8361, docs/protocol/protocol.md "Sending files"; iOS FileAttachments): each picked file is uploaded
// to the conversation's machine, `TOGETHER` at a time, and waits as a tile above the input until the message goes with
// it as a `<persona-file kind="file">` tag after what was typed. Nothing goes into the text. Kept per draft key outside
// the composer, so an upload goes on when the box shows another conversation or closes, and a failed one stays to try
// again or remove.
import { useSyncExternalStore } from "react";
import { ApiError, credentialOwner, errorText, onCredentialChange } from "./api";
import { conversationDeletionScope, conversationKeyDeleted, onConversationDeleted } from "./conversation-deleted";
import { FILE_RE } from "./compatibility";
import { getState } from "./store";
import type { AttachedFile } from "./turns";
import { t } from "../i18n";
import { toast } from "../ui/atoms";

/** How many files one message takes. */
export const MAX_FILES = 10;
/** How many upload at once; the others wait their turn. */
export const TOGETHER = 3;
/** What the hub takes in one chat upload (proto.MaxUploadBytes). */
export const MAX_FILE_BYTES = 200 * 1024 * 1024;

export interface ComposerFile {
  id: string;
  name: string;
  size: number;
  sent: number;
  state: "waiting" | "uploading" | "ready" | "failed";
  /** Why it did not get there (failed). */
  error?: string;
  /** Set once it is on the machine: what the message carries. */
  file?: AttachedFile;
}

interface Job {
  key: string;
  owner: string;
  mid: string;
  blob: File;
  dest?: string;
  ctl?: AbortController;
}

const lists = new Map<string, ComposerFile[]>();
const jobs = new Map<string, Job>();
const owners = new Map<string, string>();
const currentOwner = () => JSON.stringify([typeof localStorage === "undefined" ? "" : credentialOwner(), conversationDeletionScope()]);
const listeners = new Set<() => void>();
const EMPTY: ComposerFile[] = [];
let seq = 0;

function emit() {
  listeners.forEach((l) => l());
}

function set(key: string, list: ComposerFile[]) {
  if (conversationKeyDeleted(key)) list = [];
  if (list.length) { lists.set(key, list); owners.set(key, currentOwner()); }
  else { lists.delete(key); owners.delete(key); }
  emit();
}

function update(key: string, id: string, change: Partial<ComposerFile>) {
  const list = lists.get(key);
  if (!list?.some((f) => f.id === id)) return;
  set(key, list.map((f) => (f.id === id ? { ...f, ...change } : f)));
}

export function getComposerFiles(key: string): ComposerFile[] {
  return !conversationKeyDeleted(key) && owners.get(key) === currentOwner() ? lists.get(key) ?? EMPTY : EMPTY;
}

export function useComposerFiles(key: string): ComposerFile[] {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => getComposerFiles(key),
    () => getComposerFiles(key),
  );
}

/** The files on the machine, in the order picked: what the message carries. */
export function readyFiles(key: string): AttachedFile[] {
  return getComposerFiles(key).flatMap((f) => (f.file ? [f.file] : []));
}

/** One still on its way: the message waits for it. */
export function filesBusy(key: string): boolean {
  return getComposerFiles(key).some((f) => f.state === "waiting" || f.state === "uploading");
}

/** Where a file goes: the composer's own destination (a health conversation's reports), else kept with the chat's
 *  pictures where the hub knows how or may (its features not in yet: an older hub refuses it and the file goes again
 *  without), else the temp directory as before. */
export function fileDestination(own: string | undefined, features: readonly string[]): string | undefined {
  return own ?? (!features.length || features.includes("upload_attachment") ? "attachment" : undefined);
}

/** Files picked for the composer `key` of a conversation on machine `mid`. Returns what was refused and why, for the
 *  composer to say: over the count, or too big. */
export function addFiles(key: string, mid: string, picked: File[], dest?: string): { tooMany: number; tooBig: string[] } {
  if (conversationKeyDeleted(key)) return { tooMany: 0, tooBig: [] };
  const room = Math.max(0, MAX_FILES - getComposerFiles(key).length);
  const tooBig = picked.slice(0, room).filter((f) => f.size > MAX_FILE_BYTES).map((f) => f.name);
  const taken = picked.slice(0, room).filter((f) => f.size <= MAX_FILE_BYTES);
  const where = fileDestination(dest, getState().features);
  const added = taken.map((blob) => {
    const id = `f${++seq}`;
    jobs.set(id, { key, mid, blob, dest: where, owner: currentOwner() });
    return { id, name: blob.name, size: blob.size, sent: 0, state: "waiting" as const };
  });
  if (added.length) set(key, [...getComposerFiles(key), ...added]);
  pump(key);
  return { tooMany: Math.max(0, picked.length - room), tooBig };
}

/** Cancels one on its way, or takes it off the message. */
export function removeFile(key: string, id: string) {
  jobs.get(id)?.ctl?.abort();
  jobs.delete(id);
  set(key, getComposerFiles(key).filter((f) => f.id !== id));
  pump(key);
}

/** A failed one goes again. */
export function retryFile(key: string, id: string) {
  if (!getComposerFiles(key).some((f) => f.id === id && f.state === "failed") || !jobs.has(id)) return;
  update(key, id, { state: "waiting", sent: 0, error: undefined });
  pump(key);
}

/** The message went with the ready ones; a failed one stays to try again or remove. */
export function takeReadyFiles(key: string): AttachedFile[] {
  const list = getComposerFiles(key);
  const ready = list.filter((f) => f.file);
  ready.forEach((f) => jobs.delete(f.id));
  if (ready.length) set(key, list.filter((f) => !f.file));
  return ready.map((f) => f.file!);
}

/** Files already on the machine, ready tiles again (a send that did not go, a queued message taken back), as many as
 *  fit; returns the ones that did not. */
export function restoreFiles(key: string, files: AttachedFile[]): AttachedFile[] {
  if (conversationKeyDeleted(key)) return files;
  const room = Math.max(0, MAX_FILES - getComposerFiles(key).length);
  const back = files.slice(0, room).map((file) => ({ id: `f${++seq}`, name: file.name, size: file.size ?? 0, sent: file.size ?? 0, state: "ready" as const, file }));
  if (back.length) set(key, [...back, ...getComposerFiles(key)]);
  return files.slice(room);
}

/** The words of a message whose `<persona-file>` tags were taken out, and those files (an edit of the last message). */
export function splitFileTags(text: string): { text: string; files: AttachedFile[] } {
  const files: AttachedFile[] = [];
  const rest = text.replace(FILE_RE, (_, attrs: string) => {
    const a: Record<string, string> = {};
    for (const m of attrs.matchAll(/(\w+)="([^"]*)"/g)) a[m[1]] = unescapeAttr(m[2]);
    if (!a.path) return "";
    const num = (v?: string) => (v !== undefined && Number.isFinite(Number(v)) ? Number(v) : undefined);
    files.push({ kind: a.kind || "file", path: a.path, name: a.name || a.path.split("/").pop() || a.path, size: num(a.size), duration: num(a.duration) });
    return "\n";
  });
  return { text: files.length ? rest.trim() : text, files };
}

function escAttr(s: string) {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function unescapeAttr(s: string) {
  return s.replace(/&quot;/g, '"').replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");
}

/** `<persona-file kind="file" path="…" name="…" size="…"/>`, as the phone writes it (iOS AttachedFile.xml). */
export function fileXML(f: AttachedFile): string {
  const d = f.duration !== undefined ? ` duration="${f.duration.toFixed(1)}"` : "";
  return `<persona-file kind="${escAttr(f.kind)}" path="${escAttr(f.path)}" name="${escAttr(f.name)}" size="${f.size ?? 0}"${d}/>`;
}

/** The message to send: what was typed, then a tag per file after a blank line. */
export function withFiles(text: string, files: AttachedFile[]): string {
  return files.length ? `${text}\n\n${files.map(fileXML).join("\n")}` : text;
}

/** The upload code, loaded on first use (it stays out of the first screen) and once for every upload. */
let loaded: Promise<typeof import("./file-ops")> | undefined;
const uploadCode = () =>
  (loaded ??= import("./file-ops").catch((e) => {
    loaded = undefined; // a chunk that did not load is asked for again next time
    throw e;
  }));

/** Starts the next waiting ones of `key` while fewer than `TOGETHER` are on their way. */
function pump(key: string) {
  let running = getComposerFiles(key).filter((f) => f.state === "uploading").length;
  for (const f of getComposerFiles(key)) {
    if (running >= TOGETHER) break;
    if (f.state !== "waiting" || !jobs.has(f.id)) continue;
    running++;
    update(key, f.id, { state: "uploading" });
    void run(f.id);
  }
}

async function run(id: string) {
  const job = jobs.get(id);
  if (!job) return;
  const ctl = new AbortController();
  job.ctl = ctl;
  const progress = (sent: number) => {
    if (jobs.get(id) === job) update(job.key, id, { sent });
  };
  try {
    const { uploadAttachment } = await uploadCode();
    if (jobs.get(id) !== job || job.owner !== currentOwner() || conversationKeyDeleted(job.key)) return;
    let out;
    try {
      out = await uploadAttachment(job.mid, job.blob, { dest: job.dest, signal: ctl.signal, onProgress: progress });
    } catch (e) {
      // an older hub refused the destination: the temp directory, as before
      if (job.dest !== "attachment" || !(e instanceof ApiError) || e.status !== 400 || !/unknown dest/.test(e.message)) throw e;
      job.dest = undefined;
      out = await uploadAttachment(job.mid, job.blob, { signal: ctl.signal, onProgress: progress });
    }
    if (jobs.get(id) !== job || job.owner !== currentOwner() || conversationKeyDeleted(job.key)) return;
    const size = out.size || job.blob.size;
    jobs.delete(id); // on the machine: the picked file is no longer needed
    update(job.key, id, { state: "ready", sent: size, size, file: { kind: "file", path: out.path, name: job.blob.name, size } });
  } catch (e) {
    if (jobs.get(id) !== job || ctl.signal.aborted) return;
    const error = e instanceof ApiError && e.code === "too_large" ? t("chat-composer.file_too_large") : errorText(e);
    update(job.key, id, { state: "failed", sent: 0, error });
    toast(t("chat-composer.upload_failed", { error }), "error");
  } finally {
    job.ctl = undefined;
    pump(job.key);
  }
}

// Retire the picked blobs as well as their tiles; an ignored abort must not restore the old conversation.
onConversationDeleted((mid, sid) => {
  const key = `${mid}/${sid}`;
  for (const [id, job] of jobs) if (job.key === key) { job.ctl?.abort(); jobs.delete(id); }
  set(key, []);
});
onCredentialChange(() => {
  for (const job of jobs.values()) job.ctl?.abort();
  jobs.clear(); lists.clear(); owners.clear(); emit();
});
