// @vitest-environment jsdom
// A search hit in a folded side session (#7944): with `roots=1` the hub returns the side's topmost parent's row naming the
// side; ⌘K's result opens the side's link, which App turns into the parent's page with that side shown.
import { afterEach, describe, expect, it } from "vitest";
import { setSideFold } from "../../lib/side-fold";
import type { RuntimeSearchSnapshot } from "../../lib/runtime-search";
import type { Session } from "../../lib/types";
import { mergeRuntimeHits } from "./RuntimeSearchResults";

const review = { machine_id: "mac", session_id: "review", title: "Review PR 42", cwd: "/code", status: "idle", state: "idle" } as Session;
const search = (session: Session, side?: string): RuntimeSearchSnapshot => ({ query: "merge", hits: [{ session, score: 50, side }], loaded: true, loading: false, loadingMore: false, error: "", moreError: "", hasMore: false, total: 1, expired: false, supported: true, retryable: true });

afterEach(() => setSideFold(false));

describe("mergeRuntimeHits", () => {
  it("opens a hit in a folded side at the side's link, and any other hit at its own", () => {
    setSideFold(true);
    expect(mergeRuntimeHits([], [], search(review, "btw")).hits.map((h) => h.to)).toEqual([{ name: "session", mid: "mac", sid: "btw" }]);
    expect(mergeRuntimeHits([], [], search(review)).hits.map((h) => h.to)).toEqual([{ name: "session", mid: "mac", sid: "review" }]);
  });
});
