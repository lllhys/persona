// @vitest-environment jsdom
// Back from an Inbox item (#3329): ‹ and Done go back through history to the list the item was opened from, however many
// items were opened on the way; an item with no list under it puts the list in its place instead of adding an entry.
import { afterEach, describe, expect, it } from "vitest";
import { openOverlay } from "./back-close";
import { backToInboxList, openFromInbox, stepsToInbox } from "./inbox-nav";
import { navigate } from "./router";

/** Runs `go`, then waits for the Back it causes (history.go() is asynchronous). */
const popped = (go: () => void) =>
  new Promise<void>((done, fail) => {
    const timer = setTimeout(() => fail(new Error("no Back happened")), 1_000);
    addEventListener("popstate", () => (clearTimeout(timer), done()), { once: true });
    go();
  });

const atList = () => {
  history.pushState(null, "", "/inbox");
  return history.length;
};

afterEach(() => history.replaceState(null, "", "/"));

describe("Back from an Inbox item", () => {
  it("goes back to the list it was opened from, through the items opened on the way", async () => {
    const start = atList();
    openFromInbox({ name: "inbox", id: "a" });
    expect(stepsToInbox(history.state)).toBe(1);
    openFromInbox({ name: "inbox", id: "b", manager: "m2" }); // a related item, or another row on a wide screen
    expect(stepsToInbox(history.state)).toBe(2);
    await popped(backToInboxList);
    expect(location.pathname).toBe("/inbox");
    expect(stepsToInbox(history.state)).toBe(0);
    expect(history.length - start).toBe(2); // the two items, still ahead; no copy of the list on top
  });

  it("puts the list in place of an item with no list under it (a link, a notification, ⌘K)", () => {
    history.pushState(null, "", "/inbox/x");
    const start = history.length;
    navigate({ name: "inbox", id: "y" }); // ⌘K from the item: no list under it either
    expect(stepsToInbox(history.state)).toBe(0);
    backToInboxList();
    expect(location.pathname).toBe("/inbox");
    expect(history.length - start).toBe(1);
  });

  it("goes back past a menu open over the item, and the menu closes", async () => {
    atList();
    openFromInbox({ name: "inbox", id: "a" });
    let closed = false;
    openOverlay(() => (closed = true));
    expect(stepsToInbox(history.state)).toBe(1); // the menu's entry carries the item's way back
    await popped(backToInboxList);
    expect(location.pathname).toBe("/inbox");
    expect(closed).toBe(true);
  });

  it("an item opened over a menu takes the menu's entry, one step on from the item under it", () => {
    atList();
    openFromInbox({ name: "inbox", id: "a" });
    openOverlay(() => {});
    const length = history.length;
    openFromInbox({ name: "inbox", id: "b" });
    expect(history.length).toBe(length);
    expect(stepsToInbox(history.state)).toBe(2);
  });

  it("carries nothing to a page that is not an item (a conversation, a to-do)", () => {
    atList();
    openFromInbox({ name: "inbox", id: "a" });
    openFromInbox({ name: "session", mid: "m", sid: "s" });
    expect(history.state).toBeNull();
  });
});
