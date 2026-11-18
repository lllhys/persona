// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { setToken } from "./api";
import { chooseManager, setRosterDefault } from "./manager-choice";
import { assistantCatalogScope, assistantLaunchers, forgetModel, modelList, readAssistantLaunchers, readModels } from "./session-models";

const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
const catalog = (name: string) => ({ agent: "claude", models: [{ id: name }, { id: "typed-model", custom: true }] });
let pending: { path: string; method: string; manager: string | null; answer: (response: Response) => void }[];
beforeEach(() => {
  chooseManager(null);
  setRosterDefault("manager-a");
  pending = [];
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((answer) => {
    pending.push({ path: String(input), method: init?.method ?? "GET", manager: new Headers(init?.headers).get("X-Persona-Manager"), answer });
  })));
});
afterEach(() => { setToken(""); chooseManager(null); setRosterDefault("assistant"); vi.unstubAllGlobals(); });
const answer = async (manager: string, body: unknown, method = "GET") => {
  const at = pending.findIndex((p) => p.manager === manager && p.method === method);
  expect(at).toBeGreaterThanOrEqual(0);
  pending.splice(at, 1)[0].answer(json(body));
  await flush();
};
const load = async (mid: string, name: string, purpose?: "assistant") => {
  const loading = readModels(mid, "shared", { purpose });
  await flush();
  await answer(purpose === "assistant" ? "manager-b" : "manager-a", catalog(name));
  await loading;
};

it("pins assistant catalogs to the selected manager and keeps generic Agent catalogs on the default", async () => {
  chooseManager("manager-b");
  await load("scope-route", "agent-a");
  await load("scope-route", "assistant-b", "assistant");
  expect(modelList("scope-route", "shared").list?.models[0].id).toBe("agent-a");
  expect(modelList("scope-route", "shared", "assistant").list?.models[0].id).toBe("assistant-b");
  chooseManager("manager-c");
  expect(modelList("scope-route", "shared", "assistant").list).toBeUndefined();
  expect(modelList("scope-route", "shared").list?.models[0].id).toBe("agent-a");
});

it("keeps late launcher/default and model answers with the manager that was requested", async () => {
  chooseManager("manager-b");
  const before = assistantCatalogScope();
  const oldModels = readModels("scope-race", "shared", { purpose: "assistant" });
  const oldLaunchers = readAssistantLaunchers("scope-race");
  chooseManager("manager-c");
  expect(assistantCatalogScope()).not.toBe(before);
  const newModels = readModels("scope-race", "shared", { purpose: "assistant" });
  const newLaunchers = readAssistantLaunchers("scope-race");
  await flush();
  expect(pending.filter((p) => p.manager === "manager-b")).toHaveLength(2);
  expect(pending.filter((p) => p.manager === "manager-c")).toHaveLength(2);
  for (const manager of ["manager-c", "manager-b"]) {
    const list = pending.find((p) => p.manager === manager && p.path.includes("/launchers?"))!;
    pending.splice(pending.indexOf(list), 1);
    list.answer(json({ launchers: [{ name: "shared", assistant: true }], default: { provider: "shared", model: manager } }));
    await answer(manager, catalog(manager));
  }
  await Promise.all([oldModels, oldLaunchers, newModels, newLaunchers]);
  expect(modelList("scope-race", "shared", "assistant").list?.models[0].id).toBe("manager-c");
  expect(assistantLaunchers("scope-race").info?.def?.model).toBe("manager-c");
  chooseManager("manager-b");
  expect(modelList("scope-race", "shared", "assistant").list?.models[0].id).toBe("manager-b");
  expect(assistantLaunchers("scope-race").info?.def?.model).toBe("manager-b");
});

it("forgets only the originating manager's shared model when the current manager changes", async () => {
  chooseManager("manager-b");
  await load("scope-forget", "agent-a");
  await load("scope-forget", "assistant-b", "assistant");
  setRosterDefault("manager-b");
  const oldAgent = readModels("scope-forget", "shared");
  await flush();
  await answer("manager-b", catalog("agent-b"));
  await oldAgent;
  setRosterDefault("manager-a");
  chooseManager("manager-b");
  const deleted = forgetModel("scope-forget", "shared", "typed-model", "assistant");
  chooseManager("manager-c");
  await flush();
  expect(pending.find((p) => p.method === "DELETE")?.manager).toBe("manager-b");
  await answer("manager-b", {}, "DELETE");
  expect(await deleted).toBe("");
  expect(modelList("scope-forget", "shared").list?.models.some((m) => m.id === "typed-model")).toBe(true);
  chooseManager("manager-b");
  expect(modelList("scope-forget", "shared", "assistant").list?.models.some((m) => m.id === "typed-model")).toBe(false);
  setRosterDefault("manager-b");
  expect(modelList("scope-forget", "shared").list?.models.some((m) => m.id === "typed-model")).toBe(false);
});

it("does not reuse an earlier credential's catalog or send its pending deletion with replacement credentials", async () => {
  setToken("review-owner-a");
  chooseManager("manager-b");
  await load("scope-credentials", "old-owner", "assistant");
  const deleting = forgetModel("scope-credentials", "shared", "typed-model", "assistant");
  setToken("review-owner-b");
  await deleting;
  expect(pending.some((p) => p.method === "DELETE")).toBe(false);
  expect(modelList("scope-credentials", "shared", "assistant").list).toBeUndefined();
  await load("scope-credentials", "new-owner", "assistant");
  expect(modelList("scope-credentials", "shared", "assistant").list?.models[0].id).toBe("new-owner");
  setToken("");
});
