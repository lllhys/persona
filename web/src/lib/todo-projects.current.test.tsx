// @vitest-environment jsdom
// The To-dos page's projects are the current assistant's knowledge base's: another assistant made current reads its
// own, and the last one's are never shown for it meanwhile (docs/assistant/managers.md "The current assistant").
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { chooseManager, resetManagerChoice } from "./manager-choice";
import { resetRoster, setRoster } from "./managers";
import { useTodoProjects, type TodoProject } from "./todo-projects";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

afterEach(() => {
  vi.unstubAllGlobals();
  resetRoster();
  resetManagerChoice();
});

it("reads the projects again for another assistant made current, showing none of the last one's meanwhile", async () => {
  const asked: string[] = [];
  const pending: (() => void)[] = [];
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init: RequestInit = {}) => {
    const manager = new Headers(init.headers).get("X-Persona-Manager") ?? "default";
    const path = new URL(String(input), "http://hub.example.com").pathname;
    // the store reads the new assistant's page too when the pick changes (lib/store.ts): not this list's
    if (path !== "/api/v1/kb/projects") return Promise.resolve(new Response("{}", { headers: { "Content-Type": "application/json" } }));
    asked.push(`${path} ${manager}`);
    const body = JSON.stringify({ items: [{ id: `p-${manager}`, name: `Project of ${manager}`, extra: "short · active" }] });
    const answer = () => new Response(body, { headers: { "Content-Type": "application/json" } });
    // the second assistant's answer waits until released
    return manager === "m-2" ? new Promise<Response>((resolve) => pending.push(() => resolve(answer()))) : Promise.resolve(answer());
  }));
  setRoster({ default: "assistant", managers: [{ id: "assistant", name: "助手", online: true }, { id: "m-2", name: "工作", online: true }] });
  const shown: TodoProject[][] = [];
  function Probe() {
    shown.push(useTodoProjects());
    return null;
  }
  const root = createRoot(document.createElement("div"));
  const settle = () => act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
  await act(async () => root.render(<Probe />));
  await settle();
  expect(asked).toEqual(["/api/v1/kb/projects default"]);
  expect(shown.at(-1)!.map((p) => p.slug)).toEqual(["p-default"]);

  await act(async () => chooseManager("m-2"));
  await settle();
  expect(asked).toEqual(["/api/v1/kb/projects default", "/api/v1/kb/projects m-2"]);
  expect(shown.at(-1)).toEqual([]);
  await act(async () => pending.forEach((go) => go()));
  await settle();
  expect(shown.at(-1)!.map((p) => p.slug)).toEqual(["p-m-2"]);
  await act(async () => root.unmount());
});
