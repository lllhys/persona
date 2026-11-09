// A sign-in layer in front of the hub (a gateway or proxy with its own login) that has expired answers the hub's own
// requests with a redirect or its login page. The service worker already hands such a page load to the browser
// (lib/sw-shell.ts); this is the running page's side: an /api call, or the probe after the socket fails, that sees one
// reloads the page once so the layer takes over and sends the browser to its sign-in. Never a retry loop: one reload per
// RELOAD_GUARD_MS, and a plain network failure (offline) is not an interception.
import { BASE } from "./base";

export const RELOAD_GUARD_MS = 60_000;
const KEY = "persona.edge-reload";

/** True for what an intercepting layer answers with instead of the hub: a (manual-mode) redirect, a followed redirect,
 *  status 0, or an HTML page where the hub only speaks JSON. */
export function intercepted(r: Pick<Response, "type" | "redirected" | "status" | "headers">): boolean {
  if (r.type === "opaqueredirect" || r.redirected || r.status === 0 || (r.status >= 300 && r.status < 400)) return true;
  return [200, 401, 403, 407, 511].includes(r.status) && (r.headers?.get("Content-Type") ?? "").toLowerCase().startsWith("text/html");
}

/** Reloads the page once (within the guard window) so the layer in front can redirect to its sign-in. */
export function reloadForSignIn(now = Date.now(), storage: Pick<Storage, "getItem" | "setItem"> | undefined = safeStorage(), reload: () => void = () => location.reload()): boolean {
  const last = Number(storage?.getItem(KEY) ?? 0);
  if (last && now - last < RELOAD_GUARD_MS) return false;
  try {
    storage?.setItem(KEY, String(now));
  } catch {
    return false; // no way to guard it: never risk a loop
  }
  reload();
  return true;
}

function safeStorage(): Storage | undefined {
  try {
    return typeof sessionStorage === "undefined" ? undefined : sessionStorage;
  } catch {
    return undefined;
  }
}

let probing = false;
/** After the live socket failed: one request to the hub that follows no redirect. An interception reloads the page;
 *  anything else (the hub answered, or the network is down) changes nothing. */
export async function probeEdge(fetcher: typeof fetch = fetch): Promise<boolean> {
  if (probing) return false;
  probing = true;
  try {
    const r = await fetcher(`${BASE}/api/v1/health`, { redirect: "manual", cache: "no-store" });
    if (!intercepted(r)) return false;
    return reloadForSignIn();
  } catch {
    return false;
  } finally {
    probing = false;
  }
}
