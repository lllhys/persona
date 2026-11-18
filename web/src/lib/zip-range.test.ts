import JSZip from "jszip";
import { describe, expect, it } from "vitest";
import { IWORK_PREVIEWS, readZipEntry } from "./zip-range";

async function pages(files: Record<string, Uint8Array | string>, compression: "STORE" | "DEFLATE" = "STORE") {
  const z = new JSZip();
  for (const [name, data] of Object.entries(files)) z.file(name, data, { compression });
  return z.generateAsync({ type: "uint8array" });
}

function reader(bytes: Uint8Array) {
  const calls: [number, number][] = [];
  return {
    calls,
    read: async (s: number, e: number) => {
      calls.push([s, e]);
      return bytes.slice(s, e);
    },
  };
}

describe("readZipEntry", () => {
  it("reads the best preview with a few ranges, not the whole file", async () => {
    const big = new Uint8Array(400_000).map((_, i) => (i * 7) & 255); // the document's own data, not read
    const jpg = new Uint8Array([0xff, 0xd8, 0xff, 1, 2, 3]);
    const bytes = await pages({ "Index/Document.iwa": big, "preview.jpg": jpg, "preview-micro.jpg": new Uint8Array([9]) });
    const r = reader(bytes);
    const got = await readZipEntry(r.read, bytes.length, IWORK_PREVIEWS);
    expect(got?.name).toBe("preview.jpg");
    expect([...got!.data]).toEqual([...jpg]);
    expect(r.calls.length).toBeLessThanOrEqual(3);
    expect(r.calls.reduce((n, [s, e]) => n + e - s, 0)).toBeLessThan(bytes.length / 3);
  });

  it("prefers the PDF of every page, inflates deflated entries, matches names without case", async () => {
    const pdf = new TextEncoder().encode("%PDF-1.4 " + "x".repeat(5000));
    const bytes = await pages({ "QuickLook/preview.PDF": pdf, "preview.jpg": "jpg" }, "DEFLATE");
    const got = await readZipEntry(reader(bytes).read, bytes.length, IWORK_PREVIEWS);
    expect(got?.name).toBe("QuickLook/preview.PDF");
    expect(new TextDecoder().decode(got!.data)).toBe(new TextDecoder().decode(pdf));
  });

  it("bounds actual inflated bytes even when the directory understates their size", async () => {
    const bytes = await pages({ "preview.jpg": "x".repeat(1 << 20) }, "DEFLATE");
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const end = bytes.length - 22;
    const central = view.getUint32(end + 16, true);
    view.setUint32(central + 24, 1, true); // uncompressed size, forged in the directory
    const limit = 16 << 10;
    expect(bytes.length).toBeLessThan(limit);
    expect(await readZipEntry(reader(bytes).read, bytes.length, IWORK_PREVIEWS, limit)).toBeNull();
  });

  it("accepts a deflated preview exactly at the limit", async () => {
    const limit = 16 << 10;
    const bytes = await pages({ "preview.jpg": "x".repeat(limit) }, "DEFLATE");
    const result = await readZipEntry(reader(bytes).read, bytes.length, IWORK_PREVIEWS, limit);
    expect(result?.data.length).toBe(limit);
  });

  it("rejects an oversized directory before requesting it", async () => {
    const tail = new Uint8Array(22);
    const view = new DataView(tail.buffer);
    view.setUint32(0, 0x06054b50, true);
    view.setUint32(12, (64 << 20) + 1, true);
    view.setUint32(16, 1, true);
    const calls: [number, number][] = [];
    const read = async (start: number, end: number) => {
      calls.push([start, end]);
      return tail;
    };
    expect(await readZipEntry(read, 100 << 20, IWORK_PREVIEWS)).toBeNull();
    expect(calls).toHaveLength(1);
  });

  it("says null for no preview or no zip", async () => {
    const bytes = await pages({ "Index/Document.iwa": "x" });
    expect(await readZipEntry(reader(bytes).read, bytes.length, IWORK_PREVIEWS)).toBeNull();
    const junk = new TextEncoder().encode("not a zip at all");
    expect(await readZipEntry(reader(junk).read, junk.length, IWORK_PREVIEWS)).toBeNull();
  });
});
