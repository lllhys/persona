// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { messageView } from "./render";

const t = (key: string) => key;
describe("public message rendering", () => {
  it("keeps markup, links and scripts inert as plain text", () => {
    const hostile = '<img src=x onerror="window.hit=1"><a href="https://example.test">link</a><script>window.hit=2</script> https://example.test';
    const view = messageView({ message_id: "m-1", role: "assistant", text: hostile, execution: [{ kind: "tool", tool_id: "w-1", name: "<b>tool</b>", arguments: "<iframe src=x>", results: [{ text: "<svg onload=alert(1)>" }] }, { kind: "agent", tool_id: "w-2", text: "<video src=x>" }] }, t);
    document.body.replaceChildren(view);
    expect(document.querySelector("img,a,script,iframe,svg,video,b")).toBeNull();
    expect(view.querySelector(".share-text")?.textContent).toBe(hostile);
    expect(view.textContent).toContain("<iframe src=x>");
    expect((window as unknown as { hit?: number }).hit).toBeUndefined();
  });
  it("collapses work details by default and only opens what the reader opened", () => {
    const message = { message_id: "m-2", role: "assistant" as const, text: "Synthetic result", execution: [{ kind: "text" as const, text: "Synthetic narration" }] };
    expect(messageView(message, t).querySelector("details")?.open).toBe(false);
    expect(messageView(message, t, true).querySelector("details")?.open).toBe(true);
    expect(messageView({ message_id: "m-3", role: "user", text: "Synthetic input" }, t).querySelector("details")).toBeNull();
  });
});
