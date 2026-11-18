import { contextBridge, ipcRenderer } from "electron";

// The island page's fixed operations (#7669): what to draw comes in; the pointer's presence and a clicked row's route go
// out. No hub, token, path or other IPC.
const island = Object.freeze({
  onView(callback: (view: unknown) => void): void {
    ipcRenderer.on("island:view", (_event: Electron.IpcRendererEvent, view: unknown) => callback(view));
  },
  hover(inside: boolean) { ipcRenderer.send("island:hover", inside === true); },
  open(route: string) { ipcRenderer.send("island:open", String(route)); },
});

contextBridge.exposeInMainWorld("islandShell", island);
