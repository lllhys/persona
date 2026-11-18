import { join } from "node:path";
import { pathToFileURL } from "node:url";

// The island's page (#7669) comes from the shell's own scheme, not from file://: a packaged app's fuses
// (grantFileProtocolExtraPrivileges off, electron-builder.yml) keep file:// out of its asar, where the page would not
// load at all (ERR_FILE_NOT_FOUND) and the island's window would stay empty. No Electron here: tests run it as is.

export const ISLAND_SCHEME = "persona-island";
export const ISLAND_URL = `${ISLAND_SCHEME}://app/island.html`;

/** The page's three files, which copy-assets.mjs puts in dist/island/, and nothing else. */
const FILES = new Map([
  ["/island.html", "island.html"],
  ["/island.css", "island.css"],
  ["/island.cjs", "island.cjs"],
]);

/** The file behind a request of the island's scheme: only a GET or HEAD of one of the page's three files, so no path a
 *  page asks for reaches the filesystem or another file of the app. */
export function islandFile(raw: string, method = "GET"): string | undefined {
  if (method !== "GET" && method !== "HEAD") return undefined;
  let url: URL;
  try { url = new URL(raw); } catch { return undefined; }
  const name = FILES.get(url.pathname);
  if (url.protocol !== `${ISLAND_SCHEME}:` || url.hostname !== "app" || !name
    || url.username || url.password || url.port || url.search || url.hash || /[\\%]/.test(raw)) return undefined;
  return pathToFileURL(join(import.meta.dirname, "island", name)).href;
}
