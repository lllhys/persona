// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { initI18n, t } from "../../i18n";
import { MANAGER_HEADER } from "../../lib/manager-choice";
import type { ModelDraft, ModelProvider, ModelPurpose } from "../../lib/model-config";
import { DirectAPIEditor } from "./DirectAPIEditor";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const purpose: ModelPurpose = {
  id: "inbox.triage",
  name: "Triage",
  group: "Direct API",
  kind: "api",
  revision: 1,
  override: {},
  effective: {},
  fields: ["model", "effort"],
};
const provider: ModelProvider = {
  id: "launcher.gateway",
  name: "Gateway",
  kind: "api",
  api: "responses",
  model: "raw-default",
};
const models = [
  { id: "raw-a", name: "Alias A", efforts: ["low", "high"], default_effort: "low" },
  { id: "raw-b", name: "raw-b", efforts: [] },
];
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });

type Call = { path: string; init?: RequestInit; body: Record<string, unknown> };
let host: HTMLDivElement;
let root: Root;
let calls: Call[];
let draft: ModelDraft;
let patch: Partial<ModelDraft> | undefined;
let manager: string;
let currentProvider: ModelProvider;

function jsonFetch(answer: (call: Call) => Promise<Response> | Response) {
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).split("?")[0];
    const body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : {};
    const call = { path, init, body };
    calls.push(call);
    return answer(call);
  }));
}

function Harness() {
  const [, redraw] = useState(0);
  return (
    <DirectAPIEditor
      manager={manager}
      purpose={purpose}
      provider={currentProvider}
      draft={draft}
      disabled={false}
      onChange={(change) => {
        patch = change;
        draft = { ...draft, ...change };
        redraw((n) => n + 1);
      }}
    />
  );
}

const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
const input = () => host.querySelector<HTMLInputElement>("input[placeholder]")!;
const selects = () => [...host.querySelectorAll<HTMLSelectElement>("select")];
const effortSelect = () => selects()[1];
const testButton = () => host.querySelector<HTMLButtonElement>("button[aria-busy], button:not([aria-busy])")!;
const headerValue = (init: RequestInit | undefined, name: string) => new Headers(init?.headers).get(name);

async function render() {
  await act(async () => root.render(<Harness />));
  await flush();
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

async function typeModel(value: string) {
  await act(async () => {
    const element = input();
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(element, value);
    element.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

async function changeDraft(change: Partial<ModelDraft>) {
  draft = { ...draft, ...change };
  await render();
}

async function testProbe() {
  await act(async () => testButton().click());
}

beforeEach(async () => {
  await initI18n("en");
  calls = [];
  patch = undefined;
  manager = "manager-one";
  currentProvider = provider;
  draft = { provider: provider.id, model: "raw-a", effort: "low", runtime: "" };
  jsonFetch((call) => call.path.endsWith("/launchers/discover")
    ? response({ models })
    : response({ ok: true, ms: 12, effort: call.body.effort }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(async () => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  await initI18n("en");
});

it("leaves Use default to the saved connection instead of explicitly clearing its effort", async () => {
  draft = { ...draft, effort: "" };
  jsonFetch((call) => call.path.endsWith("/launchers/discover")
    ? response({ models })
    : response({ ok: true, ms: 12, effort: "high" }));
  await act(async () => root.render(
    <DirectAPIEditor
      manager={manager}
      purpose={{ ...purpose, id: "kb" }}
      provider={currentProvider}
      draft={draft}
      disabled={false}
      onChange={() => {}}
    />,
  ));
  await flush();
  await testProbe();
  const probe = calls.find((call) => call.path.endsWith("/launchers/test"))!;
  expect(probe.body).toEqual({ name: "gateway", model: "raw-a" });
  expect(host.textContent).toContain(t("settings-modelconfiguration.api_test_ok", { ms: 12 }));
  expect(draft.effort).toBe("");
});

describe("DirectAPIEditor", () => {
  it("discovers aliases but submits the raw model ID, requested effort and manager", async () => {
    await render();

    expect(calls[0]).toMatchObject({
      path: "/api/v1/launchers/discover",
      body: { name: "gateway" },
    });
    expect(headerValue(calls[0].init, MANAGER_HEADER)).toBe(manager);
    expect(selects()[0].textContent).toContain("Alias A (raw-a)");

    await act(async () => {
      selects()[0].value = "raw-a";
      selects()[0].dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(patch).toEqual({ model: "raw-a" });

    await testProbe();
    await flush();
    expect(calls.at(-1)).toMatchObject({
      path: "/api/v1/launchers/test",
      body: { name: "gateway", model: "raw-a", effort: "low" },
    });
    expect(headerValue(calls.at(-1)!.init, MANAGER_HEADER)).toBe(manager);
    expect(host.textContent).toContain(t("settings-modelconfiguration.api_test_ok", { ms: 12 }));
  });

  it("updates effort capability with model choice and preserves unsupported current values", async () => {
    await render();
    expect([...effortSelect().options].map((option) => option.value)).toEqual(["", "low", "high"]);

    await act(async () => {
      selects()[0].value = "raw-b";
      selects()[0].dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(host.textContent).toContain(t("settings-modelconfiguration.api_effort_unsupported"));

    draft = { ...draft, model: "raw-b", effort: "legacy-level" };
    await render();
    expect([...effortSelect().options].map((option) => option.value)).toContain("legacy-level");
    expect(effortSelect().disabled).toBe(false);

    currentProvider = { ...provider, api: "chat" };
    draft = { ...draft, effort: "legacy-level" };
    await render();
    expect(host.textContent).toContain(t("settings-modelconfiguration.api_effort_unsupported"));
    expect([...effortSelect().options].map((option) => option.value)).toContain("legacy-level");
  });

  it("offers testable protocol efforts when discovery has no model-specific metadata", async () => {
    draft = { ...draft, model: "unlisted-model", effort: "" };
    await render();
    expect([...effortSelect().options].map((option) => option.value)).toEqual(["", "off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"]);
    expect(host.textContent).toContain(t("settings-modelconfiguration.api_effort_unknown"));
    await act(async () => {
      effortSelect().value = "high";
      effortSelect().dispatchEvent(new Event("change", { bubbles: true }));
    });
    await testProbe();
    await flush();
    expect(calls.at(-1)!.body).toMatchObject({ model: "unlisted-model", effort: "high" });
    expect(host.textContent).toContain(t("settings-modelconfiguration.api_test_ok", { ms: 12 }));
  });

  it("keeps manual model entry usable after discovery failure", async () => {
    jsonFetch((call) => call.path.endsWith("/launchers/discover")
      ? response({ error: "list unavailable", models: [] })
      : response({ ok: true, ms: 1, effort: "low" }));
    await render();

    expect(host.textContent).toContain("Couldn't load models: list unavailable");
    await typeModel("typed-model");
    expect(input().value).toBe("typed-model");
    expect(patch).toEqual({ model: "typed-model" });
  });

  it("invalidates a verified result when the draft changes", async () => {
    await render();
    await testProbe();
    await flush();
    expect(host.textContent).toContain(t("settings-modelconfiguration.api_test_ok", { ms: 12 }));

    await typeModel("changed-model");
    expect(host.textContent).not.toContain(t("settings-modelconfiguration.api_test_ok", { ms: 12 }));
  });

  it("ignores A to B to A stale probe completion without clearing the newer probe", async () => {
    const old = deferred<Response>();
    const fresh = deferred<Response>();
    let probes = 0;
    jsonFetch((call) => call.path.endsWith("/launchers/discover")
      ? response({ models })
      : (++probes === 1 ? old.promise : fresh.promise));

    await render();
    await testProbe();
    await changeDraft({ model: "raw-b" });
    await changeDraft({ model: "raw-a" });
    await testProbe();
    expect(testButton().getAttribute("aria-busy")).toBe("true");

    await act(async () => old.resolve(response({ ok: true, ms: 1, effort: "low" })));
    await flush();
    expect(testButton().getAttribute("aria-busy")).toBe("true");
    expect(host.textContent).not.toContain(t("settings-modelconfiguration.api_test_ok", { ms: 1 }));

    await act(async () => fresh.resolve(response({ ok: true, ms: 2, effort: "low" })));
    await flush();
    expect(host.textContent).toContain(t("settings-modelconfiguration.api_test_ok", { ms: 2 }));
  });

  it("does not start duplicate probes on a rapid double click", async () => {
    const pending = deferred<Response>();
    jsonFetch((call) => call.path.endsWith("/launchers/discover")
      ? response({ models })
      : pending.promise);
    await render();

    await act(async () => {
      testButton().click();
      testButton().click();
    });
    expect(calls.filter((call) => call.path.endsWith("/launchers/test"))).toHaveLength(1);

    await act(async () => pending.resolve(response({ ok: true, ms: 4, effort: "low" })));
    await flush();
  });

  it("fails closed when an inherited provider, model or effort baseline is unresolved", async () => {
    const inheritedPurpose: ModelPurpose = {
      ...purpose,
      effective: { provider: provider.id, model: "inherited-model", effort: "medium" },
      override: { provider: provider.id, model: "saved-model", effort: "high" },
    };
    await act(async () => root.render(
      <DirectAPIEditor
        manager={manager}
        purpose={inheritedPurpose}
        provider={currentProvider}
        draft={{ provider: "", model: "", effort: "", runtime: "" }}
        disabled={false}
        onChange={() => {}}
      />,
    ));
    await flush();

    expect(host.textContent).toContain(t("settings-modelconfiguration.api_test_inheritance"));
    expect(testButton().disabled).toBe(true);
    expect(calls.filter((call) => call.path.endsWith("/launchers/discover"))).toHaveLength(0);
    await testProbe();
    expect(calls.filter((call) => call.path.endsWith("/launchers/test"))).toHaveLength(0);
  });

  it("ignores discovery from the previous provider after a provider remount", async () => {
    const first = deferred<Response>();
    const second = deferred<Response>();
    let discoveries = 0;
    jsonFetch((call) => call.path.endsWith("/launchers/discover")
      ? (++discoveries === 1 ? first.promise : second.promise)
      : response({ ok: true, ms: 1 }));

    await render();
    currentProvider = { ...provider, id: "launcher.other" };
    await render();
    await act(async () => second.resolve(response({ models: [{ id: "new", name: "New" }] })));
    await flush();
    await act(async () => first.resolve(response({ models: [{ id: "stale", name: "Stale" }] })));
    await flush();

    expect(selects()[0].textContent).toContain("New (new)");
    expect(selects()[0].textContent).not.toContain("Stale");
  });

  it("ignores probe and discovery responses after unmount and reopen", async () => {
    const oldProbe = deferred<Response>();
    const oldList = deferred<Response>();
    let probes = 0;
    let lists = 0;
    jsonFetch((call) => call.path.endsWith("/launchers/discover")
      ? (++lists === 1 ? oldList.promise : response({ models }))
      : (++probes === 1 ? oldProbe.promise : response({ ok: true, ms: 3, effort: "low" })));

    await render();
    await testProbe();
    await act(async () => root.render(null));
    await render();
    await testProbe();
    await flush();
    await act(async () => {
      oldProbe.resolve(response({ ok: true, ms: 99, effort: "low" }));
      oldList.resolve(response({ models: [{ id: "stale" }] }));
    });
    await flush();

    expect(host.textContent).toContain(t("settings-modelconfiguration.api_test_ok", { ms: 3 }));
    expect(host.textContent).not.toContain(t("settings-modelconfiguration.api_test_ok", { ms: 99 }));
    expect(selects()[0].textContent).not.toContain("stale");
  });

  it.each(["en", "zh-Hans", "ja"] as const)("localizes auth, model, timeout and other errors in %s", async (language) => {
    const cases = [
      ["auth", "401 unauthorized", "auth"],
      ["model_not_found", "model not found", "model"],
      ["timeout", "deadline exceeded", "timeout"],
      ["other", "gateway broke", "other"],
    ] as const;
    await initI18n(language);

    for (const [label, detail, kind] of cases) {
      jsonFetch((call) => call.path.endsWith("/launchers/discover")
        ? response({ models })
        : response({ ok: false, error: detail }));
      await render();
      await testProbe();
      await flush();
      expect(host.querySelector('[role="alert"]')?.textContent, `${language} ${label}`)
        .toContain(t(`settings-modelconfiguration.api_test_${kind}`));
    }
  });

  it.each([
    ["en", "This hub cannot verify"],
    ["zh-Hans", "此中枢无法验证"],
    ["ja", "このハブでは"],
  ])("does not verify a successful old-hub response without effort echo (%s)", async (language, expected) => {
    await initI18n(language);
    jsonFetch((call) => call.path.endsWith("/launchers/discover")
      ? response({ models })
      : response({ ok: true, ms: 5 }));
    await render();
    await testProbe();
    await flush();

    expect(host.querySelector('[role="alert"]')?.textContent).toContain(expected);
    expect(host.textContent).not.toContain(t("settings-modelconfiguration.api_test_ok", { ms: 5 }));
  });
});
