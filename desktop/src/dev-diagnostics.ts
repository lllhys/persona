// Developing Persona in the desktop shell (#9675, docs/operations/developing-persona.md "The desktop app"): while the
// page's switch is on (it tells the shell through setDevDiagnostics), the main process records its processes gone,
// windows unresponsive, power and thermal changes and each process type's CPU and memory a minute
// (dev-diagnostics-core.ts), and keeps crash dumps on this computer only (crashReporter, uploadToServer false). The page
// reads the lines back for its export. Off: nothing is listened to and what was kept is deleted.
import { app, BrowserWindow, crashReporter, ipcMain, powerMonitor } from "electron";
import { appendFile, mkdir, readdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { metricsFields, readerAllowed, ShellDiagnostics, word, type DiagnosticsFiles } from "./dev-diagnostics-core.js";

const MINUTE_MS = 60_000;
const ON_FILE = "on";

function nodeFiles(dir: string): DiagnosticsFiles {
  return {
    async append(name, text) {
      await mkdir(dir, { recursive: true });
      await appendFile(join(dir, name), text, "utf8");
    },
    async size(name) {
      return (await stat(join(dir, name)).catch(() => undefined))?.size ?? 0;
    },
    read: (name) => readFile(join(dir, name), "utf8"),
    rename: (from, to) => rename(join(dir, from), join(dir, to)),
    removeAll: () => rm(dir, { recursive: true, force: true }),
  };
}

/** Installs the two bridge operations and, when the switch was left on, starts recording. Once, at start-up. */
export function installDevDiagnostics(): void {
  const dir = join(app.getPath("userData"), "dev-diagnostics");
  const record = new ShellDiagnostics(nodeFiles(dir));
  let stop: (() => void) | null = null;
  let crashes = false;

  const start = () => {
    if (stop) return;
    if (!crashes) {
      // Local only, and it cannot be stopped until the app quits: dumps stay in the system's crash folder for this app.
      crashReporter.start({ uploadToServer: false, compress: true });
      crashes = true;
    }
    void record.note("shell", { event: "recording", version: app.getVersion(), electron: process.versions.electron, platform: process.platform });
    const watched = new WeakSet<BrowserWindow>();
    const watch = (w: BrowserWindow) => {
      if (watched.has(w)) return;
      watched.add(w);
      w.on("unresponsive", () => void record.note("window", { event: "unresponsive" }));
      w.on("responsive", () => void record.note("window", { event: "responsive" }));
    };
    BrowserWindow.getAllWindows().forEach(watch);
    const onWindow = (_e: unknown, w: BrowserWindow) => watch(w);
    const onRenderer = (_e: unknown, _w: unknown, d: Electron.RenderProcessGoneDetails) =>
      void record.note("process_gone", { process: "renderer", reason: word(d.reason), exit_code: d.exitCode });
    const onChild = (_e: unknown, d: Electron.Details) =>
      void record.note("process_gone", { process: word(d.type), name: word(d.name ?? ""), reason: word(d.reason), exit_code: d.exitCode });
    const power = (event: string) => () => void record.note("power", { event, on_battery: powerMonitor.isOnBatteryPower() });
    const powerEvents = ["suspend", "resume", "on-ac", "on-battery", "lock-screen", "unlock-screen"] as const;
    const powerHandlers = powerEvents.map((e) => [e, power(e)] as const);
    const onThermal = (details: { state: string }) => void record.note("thermal", { state: word(details.state) });
    app.on("browser-window-created", onWindow);
    app.on("render-process-gone", onRenderer);
    app.on("child-process-gone", onChild);
    for (const [e, h] of powerHandlers) powerMonitor.on(e as "suspend", h);
    powerMonitor.on("thermal-state-change" as "suspend", onThermal as unknown as () => void);
    const timer = setInterval(() => void record.note("shell_minute", metricsFields(app.getAppMetrics(), powerMonitor.isOnBatteryPower())), MINUTE_MS);
    stop = () => {
      clearInterval(timer);
      app.removeListener("browser-window-created", onWindow);
      app.removeListener("render-process-gone", onRenderer);
      app.removeListener("child-process-gone", onChild);
      for (const [e, h] of powerHandlers) powerMonitor.removeListener(e as "suspend", h);
      powerMonitor.removeListener("thermal-state-change" as "suspend", onThermal as unknown as () => void);
    };
  };

  const allowed = (event: Electron.IpcMainInvokeEvent) =>
    readerAllowed(event.senderFrame?.url, event.senderFrame === event.sender.mainFrame);

  ipcMain.handle("desktop:dev-diagnostics-set", async (event, on: unknown) => {
    if (!allowed(event)) return { on: false };
    if (typeof on !== "boolean") throw new TypeError("Developing Persona expects a boolean");
    if (on) {
      await mkdir(dir, { recursive: true });
      await writeFile(join(dir, ON_FILE), new Date().toISOString(), "utf8");
      start();
    } else {
      stop?.();
      stop = null;
      await record.clear();
    }
    return { on };
  });

  ipcMain.handle("desktop:dev-diagnostics-read", async (event) => {
    if (!allowed(event)) return { lines: [], crashDumps: 0 };
    const since = Date.parse(await readFile(join(dir, ON_FILE), "utf8").catch(() => "")) || 0;
    return { lines: await record.read(), crashDumps: await countDumps(since) };
  });

  void readFile(join(dir, ON_FILE), "utf8").then(() => app.whenReady()).then(start, () => {});
}

/** The crash dumps written since the switch went on: counted, never read. */
async function countDumps(since: number): Promise<number> {
  const root = app.getPath("crashDumps");
  let n = 0;
  const walk = async (d: string, depth: number) => {
    for (const entry of await readdir(d, { withFileTypes: true }).catch(() => [])) {
      const p = join(d, entry.name);
      if (entry.isDirectory() && depth < 3) await walk(p, depth + 1);
      else if (entry.name.endsWith(".dmp") && ((await stat(p).catch(() => undefined))?.mtimeMs ?? 0) >= since) n += 1;
    }
  };
  await walk(root, 0);
  return n;
}
