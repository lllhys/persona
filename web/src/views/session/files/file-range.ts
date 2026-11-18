// Reads a bounded byte range for the iWork preview, including older servers that ignore Range.
import { fileDownloadURL } from "../../../lib/api";
import { base64Bytes, type FileContent } from "../../../lib/files";
import type { ReadRange } from "../../../lib/zip-range";

const MAX_RANGE = (64 << 20) + 30 + 1024; // maximum compressed entry plus local header
const MAX_FULL_RESPONSE = 100 << 20; // do not download an arbitrarily large file when Range is ignored

export function rangeReader(mid: string, f: FileContent, cwd: string | undefined, signal: AbortSignal): ReadRange {
  if (!f.stream && typeof f.data === "string") {
    const all = base64Bytes(f.data);
    return async (s, e) => all.slice(s, e);
  }
  return async (s, e) => {
    if (!Number.isSafeInteger(f.size) || f.size < 0 || !Number.isSafeInteger(s) || !Number.isSafeInteger(e) || s < 0 || e < s || e > f.size || e - s > MAX_RANGE) {
      throw new Error("Invalid file range");
    }
    if (s === e) return new Uint8Array();
    const res = await fetch(fileDownloadURL(mid, f.path, cwd), { headers: { Range: `bytes=${s}-${e - 1}` }, signal });
    const declared = res.headers.get("Content-Length");
    const range = res.headers.get("Content-Range");
    const expected = res.status === 206 ? e - s : f.size;
    const badLength = declared !== null && (!/^\d+$/.test(declared) || Number(declared) !== expected);
    const badRange = res.status === 206 && range !== `bytes ${s}-${e - 1}/${f.size}`;
    if ((res.status !== 206 && res.status !== 200) || badLength || badRange || (res.status === 200 && f.size > MAX_FULL_RESPONSE)) {
      try { await res.body?.cancel(); } catch { /* Preserve the original error. */ }
      throw new Error("Invalid file range response");
    }
    const reader = res.body?.getReader();
    if (!reader) throw new Error("Missing file range response body");
    const abort = () => { void reader.cancel(signal.reason).catch(() => {}); };
    signal.addEventListener("abort", abort, { once: true });
    const result = new Uint8Array(e - s);
    let seen = 0;
    let complete = false;
    try {
      while (true) {
        if (signal.aborted) throw new DOMException("Aborted", "AbortError");
        const { done, value } = await reader.read();
        if (signal.aborted) throw new DOMException("Aborted", "AbortError");
        if (done) break;
        const from = res.status === 200 ? s : 0;
        const until = res.status === 200 ? e : e - s;
        const next = seen + value.length;
        if (res.status === 206 && next > until) throw new Error("File range response is too long");
        const copyStart = Math.max(seen, from);
        const copyEnd = Math.min(next, until);
        if (copyEnd > copyStart) result.set(value.subarray(copyStart - seen, copyEnd - seen), copyStart - from);
        seen = next;
        if (res.status === 200 && seen >= e) {
          await reader.cancel(); // no need to retain or download the rest of the full response
          complete = true;
          break;
        }
      }
      if (seen < (res.status === 206 ? e - s : e)) throw new Error("File range response is too short");
      complete = true;
      return result;
    } finally {
      signal.removeEventListener("abort", abort);
      if (!complete) { try { await reader.cancel(); } catch { /* Preserve the original error. */ } }
      reader.releaseLock();
    }
  };
}
