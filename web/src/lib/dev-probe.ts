// Developing Persona's probe (#9617, docs/operations/developing-persona.md): loaded with lib/error-reports-start.ts,
// which runs before the app opens its socket and is not part of the first screen's budget. While the switch is on it
// wraps the page's WebSocket so the recorder (lib/dev-mode.ts, its own chunk) hears the hub socket's frames by type,
// its connects and drops; everything else the recorder watches itself. iOS's twin is apple/Shared/DevProbe.swift.

/** Where this browser keeps the switch: the moment it was turned on (ISO), absent while off. */
export const DEV_MODE_KEY = "persona.devMode";

export interface DevSink {
  frame(type: string, bytes: number): void;
  socket(event: "connected" | "dropped"): void;
}

let sink: DevSink | null = null;

/** The recorder's sink, or null to stop. */
export function setDevSink(next: DevSink | null) {
  sink = next;
}

/** When the switch was turned on, or null while it is off. */
export function devModeSince(): string | null {
  try {
    return localStorage.getItem(DEV_MODE_KEY);
  } catch {
    return null;
  }
}

/** A frame's message type, read without parsing the whole frame. */
export function frameType(data: string): string {
  return /"type"\s*:\s*"([^"]{1,64})"/.exec(data)?.[1] ?? "";
}

let watching = false;

/** Wraps the page's WebSocket once: frames, the hello that makes a socket live, and a live socket's close go to the
 *  recorder's sink (none while it has not loaded yet, or after the switch went off). The app's own handlers are untouched. */
export function watchSockets() {
  if (watching || typeof window === "undefined" || typeof window.WebSocket !== "function") return;
  watching = true;
  const Native = window.WebSocket;
  class Watched extends Native {
    constructor(url: string | URL, protocols?: string | string[]) {
      super(url, protocols);
      if (!String(url).includes("/api/v1/ws")) return;
      let live = false;
      this.addEventListener("message", (ev: MessageEvent) => {
        if (typeof ev.data !== "string") return;
        const type = frameType(ev.data);
        sink?.frame(type, ev.data.length);
        if (type === "hello" && !live) {
          live = true;
          sink?.socket("connected");
        }
      });
      this.addEventListener("close", () => {
        if (live) sink?.socket("dropped");
      });
    }
  }
  window.WebSocket = Watched;
}
