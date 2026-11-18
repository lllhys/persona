import { beforeEach, describe, expect, it, vi } from "vitest";

const { state, request } = vi.hoisted(() => ({
  state: { assistant: { recent_task_chats: false } as { recent_task_chats?: boolean } | null },
  request: vi.fn(),
}));
vi.mock("./api", async (orig) => ({ ...(await orig<typeof import("./api")>()), api: (...args: unknown[]) => request(...args) }));
vi.mock("./store", () => ({
  getState: () => state,
  patchAssistant: (patch: { recent_task_chats?: boolean }) => {
    if (state.assistant) state.assistant = { ...state.assistant, ...patch };
  },
}));

import { saveRecentTaskChats } from "./recent-task-chats";

describe("Show scheduled task chats (#8324)", () => {
  beforeEach(() => {
    state.assistant = { recent_task_chats: false };
    request.mockReset();
  });

  it("shows the change before the hub answers, sends only that field, and keeps what the hub says", async () => {
    let answer: (v: unknown) => void = () => {};
    request.mockReturnValue(new Promise((r) => (answer = r)));
    const saving = saveRecentTaskChats(true);
    expect(state.assistant?.recent_task_chats).toBe(true);
    expect(request).toHaveBeenCalledWith("/assistant", { method: "PUT", json: { recent_task_chats: true } });
    answer({ recent_task_chats: true });
    await saving;
    expect(state.assistant?.recent_task_chats).toBe(true);
  });

  it("goes back when the hub says no, and says why", async () => {
    request.mockRejectedValue(new Error("offline"));
    await expect(saveRecentTaskChats(true)).rejects.toThrow("offline");
    expect(state.assistant?.recent_task_chats).toBe(false);
  });
});
