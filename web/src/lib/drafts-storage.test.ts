// Draft/quote writes against a nearly full localStorage: quota errors evict the oldest unmounted conversations'
// entries first, never the mounted conversation's keys, and a write that still fails stays quiet so typing never breaks.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { canonicalStorageKey, storedValue } from "./compatibility";
import { MAX_STORED_DRAFTS, onDraftEvent, sendDraft, storeDraft, storeQuotes } from "./drafts";

/** A Storage-shaped mock with a character capacity: setItem throws QuotaExceededError once it would overflow. */
class QuotaStorage {
  capacity: number;
  removed: string[] = [];
  private map = new Map<string, string>();

  constructor(capacity: number) {
    this.capacity = capacity;
  }

  get length() {
    return this.map.size;
  }

  get used() {
    let total = 0;
    this.map.forEach((v) => (total += v.length));
    return total;
  }

  key(i: number) {
    return [...this.map.keys()][i] ?? null;
  }

  getItem(k: string) {
    return this.map.has(k) ? this.map.get(k)! : null;
  }

  setItem(k: string, v: string) {
    const other = [...this.map.entries()].reduce((n, [ek, ev]) => n + (ek === k ? 0 : ev.length), 0);
    if (other + v.length > this.capacity) {
      const err = new Error("QuotaExceededError");
      err.name = "QuotaExceededError";
      throw err;
    }
    this.map.set(k, v);
  }

  removeItem(k: string) {
    this.removed.push(k);
    this.map.delete(k);
  }

  clear() {
    this.map.clear();
    this.removed.length = 0;
  }
}

/** Always refuses writes (and removals): nothing must escape into the save timer. */
class BlockedStorage {
  get length() {
    return 0;
  }
  key() {
    return null;
  }
  getItem() {
    return null;
  }
  setItem() {
    const err = new Error("QuotaExceededError");
    err.name = "QuotaExceededError";
    throw err;
  }
  removeItem() {
    throw new Error("storage blocked");
  }
  clear() {}
}

let unmount: (() => void) | null = null;

beforeEach(() => {
  unmount = null;
});

afterEach(() => {
  unmount?.();
  vi.unstubAllGlobals();
});

describe("quota-safe draft storage", () => {
  it("saves the mounted conversation after evicting older keys and never evicts its own", () => {
    const storage = new QuotaStorage(Number.MAX_SAFE_INTEGER);
    vi.stubGlobal("localStorage", storage);
    // older conversations first, so storage order is also oldest-first
    storage.setItem("draft:old1", "x".repeat(30));
    storage.setItem("quote:old1", JSON.stringify([{ id: "q1", text: "y".repeat(20) }]));
    storage.setItem("draft:old2", "x".repeat(30));
    storage.setItem("draft:old3", "x".repeat(30));
    // the composer on screen owns "active", including a quote list that must survive
    unmount = onDraftEvent("active", () => {});
    storage.setItem("quote:active", JSON.stringify([{ id: "q0", text: "keep me" }]));
    storage.capacity = storage.used + 10; // the new draft no longer fits

    const text = "z".repeat(20);
    expect(() => storeDraft("active", text)).not.toThrow();

    expect(storage.getItem("draft:active")).toBe(text);
    expect(storage.getItem("quote:active")).not.toBeNull();
    expect(storage.removed).not.toContain("draft:active");
    expect(storage.removed).not.toContain("quote:active");
    // the oldest conversation went first, draft and quote list together
    expect(storage.removed).toContain("draft:old1");
    expect(storage.removed).toContain("quote:old1");
    // one eviction was enough, so younger conversations stay
    expect(storage.getItem("draft:old2")).not.toBeNull();
    expect(storage.getItem("draft:old3")).not.toBeNull();
  });

  it("evicts older conversations before the target key when overwriting an old draft", () => {
    const storage = new QuotaStorage(Number.MAX_SAFE_INTEGER);
    vi.stubGlobal("localStorage", storage);
    storage.setItem("draft:loner", "x".repeat(30)); // oldest key, also the write target
    storage.setItem("draft:old1", "x".repeat(30));
    storage.setItem("draft:old2", "x".repeat(30));
    storage.capacity = storage.used + 10;

    // a bigger replacement no longer fits, so eviction must skip the target key itself
    const text = "z".repeat(50);
    expect(() => storeDraft("loner", text)).not.toThrow();
    expect(storage.removed).not.toContain("draft:loner");
    expect(storage.getItem("draft:loner")).toBe(text);
    expect(storage.removed).toContain("draft:old1");
  });

  it("keeps quote writes working through the same eviction path", () => {
    const storage = new QuotaStorage(Number.MAX_SAFE_INTEGER);
    vi.stubGlobal("localStorage", storage);
    storage.setItem("draft:old1", "x".repeat(40));
    storage.setItem("draft:old2", "x".repeat(40));
    unmount = onDraftEvent("active", () => {});
    storage.capacity = storage.used + 5;

    expect(() => storeQuotes("active", [{ id: "q", text: "quoted words" }])).not.toThrow();
    expect(storage.getItem("quote:active")).toContain("quoted words");
    expect(storage.removed).not.toContain("quote:active");
  });

  it("swallows a write that still fails after eviction so typing never throws", () => {
    vi.stubGlobal("localStorage", new BlockedStorage());
    expect(() => storeDraft("a", "words")).not.toThrow();
    expect(() => storeQuotes("a", [{ id: "q", text: "words" }])).not.toThrow();
    expect(() => storeDraft("a", "")).not.toThrow(); // removals are guarded too
  });

  it("bounds the retained drafts of unmounted conversations, oldest first", () => {
    const storage = new QuotaStorage(Number.MAX_SAFE_INTEGER);
    vi.stubGlobal("localStorage", storage);
    for (let i = 0; i < MAX_STORED_DRAFTS + 5; i++) storeDraft(`old${i}`, "x");

    const draftKeys = [...Array.from({ length: storage.length }, (_, i) => storage.key(i))].filter((k) => k.startsWith("draft:"));
    expect(draftKeys).toHaveLength(MAX_STORED_DRAFTS);
    expect(storage.getItem("draft:old0")).toBeNull();
    expect(storage.getItem(`draft:old${MAX_STORED_DRAFTS + 4}`)).toBe("x");
  });

  it("does not evict the target it just wrote when trimming a valid 51st unmounted draft", () => {
    const storage = new QuotaStorage(Number.MAX_SAFE_INTEGER);
    vi.stubGlobal("localStorage", storage);
    for (let i = 0; i < MAX_STORED_DRAFTS; i++) {
      storeDraft(i === MAX_STORED_DRAFTS - 1 ? "persona-new" : `old-${i}`, `draft-${i}`);
    }
    storage.setItem("quote:old-2", JSON.stringify([{ id: "q2", text: "keep this quote" }]));
    // A mounted composer is outside the bound, so leaving it produces a valid 51st unmounted conversation.
    unmount = onDraftEvent("mounted-extra", () => {});
    storeDraft("mounted-extra", "extra draft");
    unmount();
    unmount = null;

    expect(sendDraft("old-0", { type: "unsent", text: "recovered send" })).toBe(true);

    expect(storage.getItem("draft:old-0")).toBe("recovered send\ndraft-0");
    expect(storage.getItem("draft:old-1")).toBeNull();
    expect(storage.getItem("draft:old-2")).toBe("draft-2");
    expect(storage.getItem("quote:old-2")).toContain("keep this quote");
    expect(storage.getItem("draft:mounted-extra")).toBe("extra draft");
    expect(storage.getItem("draft:persona-new")).toBe(`draft-${MAX_STORED_DRAFTS - 1}`);
    expect(storage.getItem("draft:jarvis-new")).toBe(`draft-${MAX_STORED_DRAFTS - 1}`);
    const ids = new Set(
      [...Array.from({ length: storage.length }, (_, i) => storage.key(i)!)]
        .filter((k) => k.startsWith("draft:"))
        .map(canonicalStorageKey),
    );
    expect(ids).toHaveLength(MAX_STORED_DRAFTS);
  });

  it("never counts the mounted conversation out of the bound", () => {
    const storage = new QuotaStorage(Number.MAX_SAFE_INTEGER);
    vi.stubGlobal("localStorage", storage);
    unmount = onDraftEvent("kept", () => {});
    storeDraft("kept", "stay");
    for (let i = 0; i < MAX_STORED_DRAFTS + 5; i++) storeDraft(`other${i}`, "x");

    expect(storage.getItem("draft:kept")).toBe("stay");
    expect(storage.removed).not.toContain("draft:kept");
  });

  it("evicts a legacy alias pair as one conversation under quota, leaving no orphaned alias", () => {
    const storage = new QuotaStorage(Number.MAX_SAFE_INTEGER);
    vi.stubGlobal("localStorage", storage);
    storeDraft("persona-new", "p".repeat(20)); // writes draft:persona-new and its draft:jarvis-new alias
    storage.setItem("draft:old1", "x".repeat(30));
    storage.setItem("draft:old2", "x".repeat(30));
    storage.capacity = storage.used + 10; // the next draft no longer fits

    expect(() => storeDraft("active", "z".repeat(20))).not.toThrow();

    // the aliased pair is one evictable slot and both keys leave together
    expect(storage.removed).toContain("draft:persona-new");
    expect(storage.removed).toContain("draft:jarvis-new");
    expect(storage.getItem("draft:persona-new")).toBeNull();
    expect(storage.getItem("draft:jarvis-new")).toBeNull();
    // one slot was enough, so younger conversations stay and the active draft landed
    expect(storage.getItem("draft:old1")).not.toBeNull();
    expect(storage.getItem("draft:old2")).not.toBeNull();
    expect(storage.getItem("draft:active")).toBe("z".repeat(20));
  });

  it("counts a legacy alias pair once against the retained-draft bound", () => {
    const storage = new QuotaStorage(Number.MAX_SAFE_INTEGER);
    vi.stubGlobal("localStorage", storage);
    storeDraft("persona-new", "p"); // the aliased pair, oldest in storage
    for (let i = 0; i < MAX_STORED_DRAFTS - 2; i++) storeDraft(`old${i}`, "x");

    expect(() => storeDraft("young", "x")).not.toThrow(); // exactly MAX conversations now

    // one pair, one bound slot: the bound sweep evicts neither alias key nor any ordinary conversation
    expect(storage.getItem("draft:persona-new")).toBe("p");
    expect(storage.getItem("draft:jarvis-new")).toBe("p");
    expect(storage.getItem("draft:young")).toBe("x");
    const ids = new Set(
      [...Array.from({ length: storage.length }, (_, i) => storage.key(i)!)]
        .filter((k) => k.startsWith("draft:"))
        .map(canonicalStorageKey),
    );
    expect(ids).toHaveLength(MAX_STORED_DRAFTS);
  });

  it("returns the legacy alias on a remount read while storage stays full, without throwing or resurrecting the canonical key", () => {
    const storage = new QuotaStorage(Number.MAX_SAFE_INTEGER);
    vi.stubGlobal("localStorage", storage);
    // an old build left only the legacy key; storage is now full, so the canonical restore write cannot fit
    storage.setItem("draft:jarvis-new", "unsent words");
    storage.capacity = storage.used;
    expect(storage.getItem("draft:persona-new")).toBeNull();

    let read: string | null = null;
    expect(() => {
      read = storedValue("draft:persona-new"); // a Composer mounting again runs exactly this read
    }).not.toThrow();
    expect(read).toBe("unsent words"); // the words still reach the composer
    expect(storage.getItem("draft:persona-new")).toBeNull(); // no failed restore resurrected the canonical key
    expect(storage.getItem("draft:jarvis-new")).toBe("unsent words");
  });
});
