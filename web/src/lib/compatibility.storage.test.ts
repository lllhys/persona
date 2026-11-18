// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { getToken, setToken } from "./api";
import { storedCredentials, storedValue, storeValue, removeStoredValue } from "./compatibility";
import bootstrap from "../../public/compatibility-bootstrap.js?raw";
import appHTML from "../../index.html?raw";
import adminHTML from "../../admin/index.html?raw";

afterEach(() => localStorage.clear());
const KEYS = [
  ["persona.theme", "jarvis.theme"],
  ["persona.agents.prefs", "jarvis.agents.prefs"],
  ["persona.agentGrouping", "jarvis.agentGrouping"],
  ["persona.agentFilter", "jarvis.agentFilter"],
  ["persona.files.view", "jarvis.files.view"],
  ["draft:persona-new", "draft:jarvis-new"],
];

describe("baseline browser persistence compatibility", () => {
  it("copies only the exact baseline keys on read, idempotently, with empty canonical values winning", () => {
    for (const [canonical, old] of KEYS) {
      localStorage.setItem(old, `old:${old}`);
      expect(storedValue(canonical)).toBe(`old:${old}`);
      expect(localStorage.getItem(canonical)).toBe(`old:${old}`);
      localStorage.setItem(canonical, "");
      expect(storedValue(canonical)).toBe("");
    }
    localStorage.setItem("jarvis.user-defined", "User data");
    expect(storedValue("persona.user-defined")).toBeNull();
    expect(localStorage.getItem("jarvis.user-defined")).toBe("User data");
  });

  it("writes and clears both exact aliases without rewriting user drafts or arbitrary keys", () => {
    for (const [canonical, old] of KEYS) {
      storeValue(canonical, "new value");
      expect(localStorage.getItem(old)).toBe("new value");
      removeStoredValue(canonical);
      expect(localStorage.getItem(canonical)).toBeNull();
      expect(localStorage.getItem(old)).toBeNull();
    }
    storeValue("draft:user-jarvis-project", "My jarvis project");
    expect(localStorage.getItem("draft:user-jarvis-project")).toBe("My jarvis project");
    expect(localStorage.getItem("draft:user-persona-project")).toBeNull();
    const count = localStorage.length;
    storeValue("constructor", "ordinary user key");
    expect(localStorage.length).toBe(count + 1);
    removeStoredValue("constructor");
    expect(localStorage.length).toBe(count);
  });

  it("reuses the baseline login and migrates its token id only with that token", () => {
    localStorage.setItem("jarvis.token", "old-token");
    localStorage.setItem("jarvis.tokenId", "old-device");
    expect(getToken()).toBe("old-token");
    expect(storedCredentials()).toEqual({ token: "old-token", tokenId: "old-device" });
    expect(localStorage.getItem("persona.tokenId")).toBe("old-device");
    localStorage.setItem("persona.token", "different-token");
    localStorage.removeItem("persona.tokenId");
    expect(storedCredentials()).toEqual({ token: "different-token", tokenId: "" });
    setToken("different-token");
    expect(localStorage.getItem("jarvis.tokenId")).toBeNull();
    expect(storedCredentials().tokenId).toBe("");
  });

  it("does not combine an orphan canonical id with a legacy token or revive a canonical empty login", () => {
    localStorage.setItem("jarvis.token", "old-token");
    localStorage.setItem("jarvis.tokenId", "old-device");
    localStorage.setItem("persona.tokenId", "orphan-device");
    expect(storedCredentials()).toEqual({ token: "", tokenId: "" });
    expect(storedValue("persona.token")).toBeNull();
    expect(storedValue("persona.tokenId")).toBe("orphan-device");
    localStorage.setItem("persona.token", "");
    expect(getToken()).toBe("");
    expect(storedCredentials().tokenId).toBe("");
    localStorage.setItem("persona.token", "old-token");
    localStorage.setItem("persona.tokenId", "");
    expect(storedCredentials()).toEqual({ token: "old-token", tokenId: "" });
  });

  it("saves a new credential pair together and logout clears both generations", () => {
    localStorage.setItem("jarvis.token", "old-token");
    localStorage.setItem("jarvis.tokenId", "old-device");
    setToken("new-token", "new-device");
    expect(storedCredentials()).toEqual({ token: "new-token", tokenId: "new-device" });
    expect(localStorage.getItem("jarvis.token")).toBe("new-token");
    expect(localStorage.getItem("jarvis.tokenId")).toBe("new-device");
    setToken("");
    for (const key of ["jarvis.token", "jarvis.tokenId", "persona.token", "persona.tokenId"]) expect(localStorage.getItem(key)).toBeNull();
    expect(getToken()).toBe("");
  });

  it("keeps serving a legacy credential pair when its canonical migration cannot be written", () => {
    for (const failKey of ["persona.token", "persona.tokenId"]) {
      const values = new Map<string, string>([["jarvis.token", "old-token"], ["jarvis.tokenId", "old-device"]]);
      const full = {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => {
          if (key === failKey) throw new DOMException("Synthetic storage full", "QuotaExceededError");
          values.set(key, value);
        },
        removeItem: (key: string) => values.delete(key),
      } as unknown as Storage;

      expect(() => storedCredentials(full)).not.toThrow();
      expect(storedCredentials(full)).toEqual({ token: "old-token", tokenId: "old-device" });
      expect(storedValue("persona.token", full)).toBe("old-token");
      expect(storedValue("persona.tokenId", full)).toBe("old-device");
      expect(values.get(failKey)).toBeUndefined();
    }

    const signedOut = {
      getItem: (key: string) => key === "persona.token" ? "" : key === "jarvis.token" ? "old-token" : null,
      setItem: () => { throw new DOMException("Synthetic storage full", "QuotaExceededError"); },
      removeItem() {},
    } as unknown as Storage;
    expect(storedCredentials(signedOut)).toEqual({ token: "", tokenId: "" });
    expect(storedValue("persona.token", signedOut)).toBe("");
  });

  it("keeps serving the legacy value on read when the alias restore write hits a full quota", () => {
    const quotaError = () => {
      const err = new Error("QuotaExceededError");
      err.name = "QuotaExceededError";
      throw err;
    };
    // storage holds only the legacy key and refuses every write: a read must not throw into the composer's mount
    const full = {
      getItem: (k: string) => (k === "draft:jarvis-new" ? "legacy words" : null),
      setItem: quotaError,
      removeItem() {},
    } as unknown as Storage;

    let value: string | null = null;
    expect(() => {
      value = storedValue("draft:persona-new", full);
    }).not.toThrow();
    expect(value).toBe("legacy words");
  });

  it("migrates the legacy theme before either HTML entry point applies its initial theme", () => {
    for (const html of [appHTML, adminHTML]) {
      expect(html.indexOf('src="compatibility-bootstrap.js"')).toBeLessThan(html.indexOf('localStorage.getItem("persona.theme")'));
    }
    localStorage.setItem("jarvis.theme", "dark");
    new Function("localStorage", bootstrap)(localStorage);
    expect(localStorage.getItem("persona.theme")).toBe("dark");
    localStorage.setItem("persona.theme", "");
    new Function("localStorage", bootstrap)(localStorage);
    expect(localStorage.getItem("persona.theme")).toBe("");
  });
});
