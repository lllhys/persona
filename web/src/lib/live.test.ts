import { afterEach, describe, expect, it } from "vitest";
import { LIVE_STALE_MS, liveEntry, receiveLive, resetLive, shownLive, type LiveEntry } from "./live";

afterEach(resetLive);

describe("shownLive", () => {
  const entry: LiveEntry = { live: { verb: "Frosting", tokens_down: 22_800 }, at: 1000 };

  it("shows the line while the session works and it is fresh", () => {
    expect(shownLive(entry, true, 1000 + LIVE_STALE_MS - 1)).toEqual(entry.live);
  });

  it("shows nothing once the session no longer works", () => {
    expect(shownLive(entry, false, 1000)).toBeNull();
  });

  it("shows nothing after 15 s without an update", () => {
    expect(shownLive(entry, true, 1000 + LIVE_STALE_MS)).toBeNull();
  });

  it("keeps the last line, however old, while the hub is out of reach", () => {
    expect(shownLive(entry, true, 1000 + 10 * LIVE_STALE_MS, false)).toEqual(entry.live);
    expect(shownLive(entry, false, 1000, false)).toBeNull();
  });

  it("shows nothing without a line", () => {
    expect(shownLive(undefined, true, 1000)).toBeNull();
  });
});

describe("receiveLive", () => {
  it("keeps the latest line per session and forgets it on live: null", () => {
    receiveLive({ machine_id: "m", session_id: "s", live: { verb: "A" } }, 5);
    receiveLive({ machine_id: "m", session_id: "s", live: { verb: "B" } }, 6);
    expect(liveEntry("m", "s")).toEqual({ live: { verb: "B" }, at: 6 });
    receiveLive({ machine_id: "m", session_id: "s", live: null });
    expect(liveEntry("m", "s")).toBeUndefined();
    expect(liveEntry("m", "other")).toBeUndefined();
  });

  it("forgets a stale line on a later push, even when its null never came", () => {
    receiveLive({ machine_id: "m", session_id: "old", live: { verb: "A" } }, 1000);
    receiveLive({ machine_id: "m", session_id: "new", live: { verb: "B" } }, 1000 + LIVE_STALE_MS - 1);
    expect(liveEntry("m", "old")).toBeDefined();
    receiveLive({ machine_id: "m", session_id: "new", live: { verb: "C" } }, 1000 + LIVE_STALE_MS);
    expect(liveEntry("m", "old")).toBeUndefined();
    expect(liveEntry("m", "new")?.live).toEqual({ verb: "C" });
  });
});
