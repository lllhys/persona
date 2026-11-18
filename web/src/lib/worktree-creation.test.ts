// @vitest-environment jsdom
import { webcrypto } from "node:crypto";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { credentialOwner, setToken } from "./api";
import { getState } from "./store";
import { setRosterDefault } from "./manager-choice";
import { creationSession, creationBody, createdWorkspace, openChildWorktree, unsupportedWorktreeParent, type CreationDraft } from "./worktree-creation";
import { onOpenNewWorktree, type WorktreePrepare, type WorktreeRequest } from "./worktree";

vi.mock("./workspace-catalog", () => ({ refreshWorkspaceCatalog: vi.fn().mockResolvedValue(undefined) }));

const prepared: WorktreePrepare = { root: "/projects/demo", repoName: "Demo", path: "/worktrees", branch: "feature/current",
  composer: { project: "Demo", base_ref: "origin/main", refs: ["origin/main", "feature/existing"], presets: [{ id: "small", name: "Web", directories: ["web"] }],
    capabilities: ["create", "branch_override", "source.github", "sparse_presets", "folder_parent"] } };
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const created = { workspace_id: "actual-runtime-id", path: "/worktrees/generated-2", branch: "feature/existing", terminal: "pane:keep-runtime-tab" };
let fetcher: ReturnType<typeof vi.fn>;
const request = (overrides: Partial<WorktreeRequest> = {}): WorktreeRequest => ({ target: { mid: "computer", workspaceId: "source", host: "orca", owner: credentialOwner(), manager: "assistant", label: "Source", cwd: "/projects/demo" }, changed: vi.fn(), ...overrides });
const draft = (): CreationDraft => ({ source: request().target, name: "", branch: "", path: "", pathEdited: false, automaticName: false, note: "", repositories: {} });

beforeEach(() => {
  localStorage.clear(); sessionStorage.clear();
  vi.stubGlobal("crypto", webcrypto);
  setToken(`synthetic-${Math.random()}`); setRosterDefault("assistant");
  Object.assign(getState(), { machines: [{ id: "computer", name: "Computer", online: true }] });
  fetcher = vi.fn().mockResolvedValue(response(created)); vi.stubGlobal("fetch", fetcher);
});
afterEach(() => vi.unstubAllGlobals());
const creates = () => fetcher.mock.calls.filter(([url]) => String(url).endsWith("/worktree/create"));

describe("creation intent transport and recovery", () => {
  it("keeps source/base independent of visual parent and preserves explicit branch/Chinese name without guessing a path", async () => {
    const parent = { machineId: "computer", host: "orca", workspaceId: "other-project-parent", cwd: "/collections/empty", owner: credentialOwner(), manager: "assistant" };
    const session = await creationSession(request({ parent }));
    session.update({ name: "中文名称", branch: "feature/existing", note: "Review UI" }); // i18n-ignore
    session.updateRepository({ baseRef: "origin/main", preset: "small", provider: { type: "issue", number: 42, title: "Issue", url: "https://github.com/example/demo/issues/42" } });
    await session.submit(prepared);
    expect(JSON.parse(creates()[0][1].body)).toEqual({ workspace_id: "source", kind: "orca", composer: true, name: "中文名称", branch_override: "feature/existing", base_ref: "origin/main", parent_workspace_id: "other-project-parent", note: "Review UI", sparse_preset_id: "small", source_id: "issue:42", source_url: "https://github.com/example/demo/issues/42" });
    expect(createdWorkspace(session.getSnapshot().intent!)).toEqual({ machineId: "computer", host: "orca", workspaceId: "actual-runtime-id", cwd: "/worktrees/generated-2", label: "generated-2", owner: credentialOwner(), manager: "assistant" });
    expect(session.getSnapshot().intent?.created?.terminal).toBe("pane:keep-runtime-tab");
    expect(new Headers(creates()[0][1].headers).get("X-Persona-Manager")).toBe("assistant");
  });
  it("lost success replays one checkout under exactly the same ID/body after close/reopen", async () => {
    const receipts = new Map<string, unknown>(); let checkouts = 0; let lose = true;
    fetcher.mockImplementation(async (url, init) => {
      if (String(url).includes("/ops/")) return response({ state: "done" });
      const id = new Headers(init.headers).get("X-Persona-Op-Id")!;
      if (!receipts.has(id)) { checkouts++; receipts.set(id, created); }
      if (lose) { lose = false; throw new TypeError("connection lost"); }
      return response(receipts.get(id));
    });
    const first = await creationSession(request()); await first.submit(prepared);
    expect(first.getSnapshot().intent?.phase).toBe("unknown");
    first.update({ name: "must not replace uncertain request" });
    const reopened = await creationSession(request()); await reopened.submit(prepared);
    expect(checkouts).toBe(1); expect(reopened.getSnapshot().intent?.phase).toBe("created");
    expect(new Headers(creates()[0][1].headers).get("X-Persona-Op-Id")).toMatch(/^[\da-f-]{36}$/);
    expect(new Headers(creates()[1][1].headers).get("X-Persona-Op-Id")).toBe(new Headers(creates()[0][1].headers).get("X-Persona-Op-Id"));
    expect(creates()[1][1].body).toBe(creates()[0][1].body);
    expect(JSON.parse(creates()[0][1].body)).not.toHaveProperty("name");
  });
  it("never treats pending as success and synchronously admits only one in-flight create", async () => {
    let answer!: (value: Response) => void;
    fetcher.mockImplementationOnce(() => new Promise((resolve) => { answer = resolve; }));
    const session = await creationSession(request()); const first = session.submit(prepared); const second = session.submit(prepared);
    expect(creates()).toHaveLength(1); answer(response({ state: "pending" }, 202)); await Promise.all([first, second]);
    expect(session.getSnapshot().intent).toMatchObject({ phase: "pending" }); expect(session.getSnapshot().intent?.created).toBeUndefined();
  });
  it("unknown receipts override timeout HTTP status and prohibit a fresh UUID or mutable intent", async () => {
    fetcher.mockImplementation(async (url) => String(url).includes("/ops/") ? response({ state: "unknown" }) : response({ error: "node timeout", delivered: "unknown" }, 409));
    const session = await creationSession(request()); await session.submit(prepared); const id = session.getSnapshot().intent!.id;
    session.update({ branch: "replacement" }); await session.submit(prepared);
    expect(session.getSnapshot().intent?.id).toBe(id); expect(session.getSnapshot().intent?.phase).toBe("unknown");
    expect(session.getSnapshot().draft.branch).toBe(""); expect(creates()).toHaveLength(1);
  });
  it("only confirmed failed receipts permit revised collision intent with a new ID", async () => {
    fetcher.mockImplementation(async (url) => String(url).includes("/ops/") ? response({ state: "failed" }) : response({ error: "branch collision" }, 409));
    const session = await creationSession(request()); session.update({ branch: "feature/existing" }); await session.submit(prepared);
    const id = session.getSnapshot().intent!.id; expect(session.getSnapshot().intent?.phase).toBe("refused");
    session.update({ name: "different", branch: "feature/new" }); fetcher.mockResolvedValue(response(created)); await session.submit(prepared);
    expect(session.getSnapshot().intent?.id).not.toBe(id); expect(JSON.parse(creates()[1][1].body).branch_override).toBe("feature/new");
  });
  it("does not dispatch when browser persistence fails", async () => {
    const session = await creationSession(request()); vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("quota"); });
    await session.submit(prepared); expect(creates()).toHaveLength(0); expect(session.getSnapshot().storageError).toBeTruthy(); vi.restoreAllMocks();
  });
  it("retains acknowledged worktree across reopen and repeated submit", async () => {
    const session = await creationSession(request()); await session.submit(prepared);
    const reopened = await creationSession(request()); await reopened.submit(prepared); await reopened.submit(prepared);
    expect(reopened.getSnapshot().intent?.created).toEqual(created); expect(creates()).toHaveLength(1);
  });
  it("isolates credentials even if they point to the same source path and blocks stale-owner submission", async () => {
    const session = await creationSession(request()); session.update({ note: "private draft" });
    setToken("another-synthetic-owner");
    await expect(session.submit(prepared)).rejects.toThrow();
    const other = await creationSession(request()); expect(other.getSnapshot().draft.note).toBe(""); expect(creates()).toHaveLength(0);
  });
  it("restores the exact persisted intent after module reload and uses same-ID receipt replay", async () => {
    fetcher.mockImplementation(async (url) => String(url).includes("/ops/") ? response({ state: "done" }) : Promise.reject(new TypeError("lost response")));
    const session = await creationSession(request()); await session.submit(prepared); const id = session.getSnapshot().intent!.id;
    vi.resetModules();
    const apiAgain = await import("./api"); const storeAgain = await import("./store");
    Object.assign(storeAgain.getState(), { machines: [{ id: "computer", name: "Computer", online: true }] });
    const reloaded = await import("./worktree-creation");
    const recovered = await reloaded.creationSession({ ...request(), target: { ...request().target, owner: apiAgain.credentialOwner() } });
    expect(recovered.getSnapshot().intent?.id).toBe(id);
    fetcher.mockImplementation(async (url) => response(String(url).includes("/ops/") ? { state: "done" } : created));
    await recovered.submit(prepared); expect(recovered.getSnapshot().intent?.phase).toBe("created");
    expect(new Headers(creates()[1][1].headers).get("X-Persona-Op-Id")).toBe(id);
  });
});

describe("capability and placement safety", () => {
  it("refuses unsupported advanced overrides without legacy downgrade", () => {
    for (const patch of [{ branch: "feature/custom" }, { note: "Note" }]) {
      const value = { ...draft(), ...patch };
      const info = patch.branch ? { ...prepared, composer: { ...prepared.composer!, capabilities: ["create"] } } : { ...prepared, composer: undefined };
      expect(() => creationBody(value, info)).toThrow();
    }
  });
  it("retains separate repo-specific base/source/preset drafts when switching Git sources", async () => {
    const session = await creationSession(request()); session.update({ name: "Draft", branch: "feature/intended" });
    session.updateRepository({ baseRef: "feature/existing", preset: "small" });
    const oldSource = session.getSnapshot().draft.source;
    session.update({ source: { ...oldSource, workspaceId: "another-source" } }); session.updateRepository({ baseRef: "origin/main" });
    session.update({ source: oldSource });
    expect(session.getSnapshot().draft).toMatchObject({ name: "Draft", branch: "feature/intended" });
    expect(creationBody(session.getSnapshot().draft, prepared)).toMatchObject({ base_ref: "feature/existing", sparse_preset_id: "small" });
  });
  it("refuses cross-machine parents and keeps explicit no-parent separate from the source default", () => {
    expect(creationBody({ ...draft(), parent: null }, prepared)).toMatchObject({ no_parent: true });
    expect(creationBody(draft(), prepared)).not.toHaveProperty("parent_workspace_id");
    expect(() => creationBody({ ...draft(), parent: { machineId: "other", host: "orca", workspaceId: "same", cwd: "/projects/demo", owner: credentialOwner(), manager: "assistant" } }, prepared)).toThrow();
  });
  it("preserves the old branch/path shape and gates legacy generated naming and parent choice", () => {
    const legacy = { ...prepared, composer: undefined };
    expect(creationBody({ ...draft(), branch: "feature/legacy", path: "/worktrees/manual", pathEdited: true }, legacy)).toEqual({ workspace_id: "source", kind: "orca", branch: "feature/legacy", path: "/worktrees/manual" });
    expect(() => creationBody({ ...draft(), automaticName: true }, legacy)).toThrow();
    expect(creationBody({ ...draft(), automaticName: true, parent: null }, { ...legacy, automatic_name: true, choose_parent: true })).toEqual({ workspace_id: "source", kind: "orca", auto_name: true, no_parent: true });
  });
  it("exports a child opener with independent canonical parent/source and acknowledgment callback", () => {
    const listen = vi.fn(); const stop = onOpenNewWorktree(listen); const onCreated = vi.fn();
    openChildWorktree({ machineId: "computer", host: "orca", workspaceId: "parent", cwd: "/collections" }, { source: { machineId: "computer", host: "orca", workspaceId: "source", cwd: "/projects/demo" }, onCreated });
    expect(listen).toHaveBeenCalledWith(expect.objectContaining({ target: expect.objectContaining({ workspaceId: "source", owner: credentialOwner() }), parent: expect.objectContaining({ workspaceId: "parent", owner: credentialOwner() }), onCreated })); stop();
  });
  it("accepts a source-default parent with actual legacy herdr capabilities", async () => {
    const source = { machineId: "computer", host: "herdr", workspaceId: "native-source", cwd: "/projects/demo", owner: credentialOwner(), manager: "assistant" };
    const listen = vi.fn(); const stop = onOpenNewWorktree(listen);
    openChildWorktree(source); stop();
    const session = await creationSession(listen.mock.calls[0][0]);
    session.update({ branch: "feature/child" });
    const legacy = { root: "/projects/demo", repoName: "Demo", branch: "main", path: "/worktrees/demo", automatic_name: true };
    const value = session.getSnapshot().draft;
    expect(unsupportedWorktreeParent(value, legacy)).toBe(false);
    expect(creationBody(value, legacy)).toEqual({ workspace_id: "native-source", kind: "herdr", branch: "feature/child", path: "/worktrees/demo/feature-child" });
    expect(() => creationBody({ ...value, parent: null }, legacy)).toThrow();
    expect(() => creationBody({ ...value, parent: { ...source, workspaceId: "other" } }, legacy)).toThrow();
    await session.submit(legacy);
    expect(JSON.parse(creates()[0][1].body)).not.toHaveProperty("parent_workspace_id");
  });
});

it("does not replace the UUID for an unchanged confirmed refusal", async () => {
  fetcher.mockImplementation(async (url) => String(url).includes("/ops/") ? response({ state: "failed" }) : response({ error: "branch collision" }, 409));
  const session = await creationSession(request()); session.update({ name: "same intent" }); await session.submit(prepared);
  const id = session.getSnapshot().intent?.id;
  session.update({ name: " same intent " }); await session.submit(prepared);
  expect(session.getSnapshot().intent?.id).toBe(id); expect(creates()).toHaveLength(1);
});

it("keeps basic suggested paths repository-bound while retaining intentionally edited paths", () => {
  const value = { ...draft(), branch: "feature/code", path: "/old-repository/feature-code" };
  const legacy = { ...prepared, composer: undefined, path: "/new-repository" };
  expect(creationBody(value, legacy).path).toBe("/new-repository/feature-code");
  expect(creationBody({ ...value, pathEdited: true }, legacy).path).toBe("/old-repository/feature-code");
});
