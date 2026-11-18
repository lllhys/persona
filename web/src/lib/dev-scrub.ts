// What Developing Persona's package may say (#9617, docs/operations/developing-persona.md "What is never included"):
// recorded lines carry only code-chosen words and numbers, and free text (an error's message, a console line) passes
// `scrubText` first. The same rules as iOS (apple/Persona/DevMode/DevScrub.swift); route patterns are the error
// reports' `pathPattern`, from a copy of its word list (dev-mode.test.ts keeps the two equal): importing it would add to
// the first screen, where lib/error-reports.ts lives, for a chunk that loads only while the switch is on.

// The words of the hub's API paths (lib/error-reports.ts API_WORDS); any other segment becomes `:id`.
export const DEV_API_WORDS = new Set(
  ("accounts agents archive assistant attach auth auto-upgrade before bind binding blob blocked botmux bots browser bulk catalog " +
    "channels chats check claude-dirs close code comments computer config connectors contributions conversations convert create " +
    "current data days decisions deploy diff dirs discover dismiss doc doctor documents done draft enabled engine error-reports " +
    "events extensions file files find fix flags fork fresh fs full git health herdr history hub image import inbox info install " +
    "interactions interrupt items kb kb-search lark launchers log login logout looks machines mail managers markets memory messages " +
    "metrics missing model-config modes moments move mutes notify oauth owner pair pair-codes panes people personas pinned pins posts " +
    "prefs prepare presets preview providers purposes queue quick quota raw reactions read reconnect refresh reject rename replies " +
    "reply resolve restore retry rewind route run runs screen secrets seen send sessions settings show skills spaces split start " +
    "status step suggestion tabs task-plans tasks thread thumb today todos token tokens translate trash tree triage ui unarchive " +
    "update upgrade upgrades usage windows workspace-board workspaces worktree").split(" "),
);

/** An API path as a pattern: no host, query or `/api/v1`, every segment that isn't one of the API's words `:id`. */
export function devPathPattern(path: string): string {
  const p = path.trim().replace(/^[a-z][a-z0-9+.-]*:\/\/[^/?#\s]*/i, "").replace(/[?#].*$/s, "").replace(/^\/api\/v1(?=\/|$)/, "");
  return p.split("/").map((seg) => (seg === "" || DEV_API_WORDS.has(seg) ? seg : ":id")).join("/") || "/";
}

/** The longest free text kept, in UTF-8 bytes. */
export const MAX_TEXT = 1024;

const RULES: [RegExp, string][] = [
  // A URL keeps its path only: hosts, queries and fragments (tokens, pairing codes) go.
  [/\b[a-z][a-z0-9+.-]*:\/\/[^\s/?#'"<>()]*([^\s?#'"<>()]*)[^\s'"<>()]*/gi, "$1"],
  // What is left of a query after a path.
  [/(\/[^\s'"<>()?#]*)[?#][^\s'"<>()]*/g, "$1"],
  [/\b(Bearer|Basic|Token)\s+[A-Za-z0-9._~+/=-]+/g, "$1 <redacted>"],
  [/\b(access_token|refresh_token|id_token|token|api_key|apikey|key|secret|client_secret|password|passwd|pwd|cookie|set-cookie|authorization|auth|session_token|signature|sig)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;&'"]+)/gi, "$1$2<redacted>"],
  [/\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g, "<redacted>"],
  [/[\w.+-]+@[\w-]+(?:\.[\w-]+)+/g, "<email>"],
  [/(?<![\w.])(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}(?![\w.])/g, "<ip>"],
  [/(?<![\w:])(?:[0-9a-f]{1,4}:){4,7}[0-9a-f]{1,4}(?![\w:])/gi, "<ip>"],
  [/(?<![\w:])[0-9a-f]{0,4}(?::[0-9a-f]{0,4})*::[0-9a-f]{0,4}(?::[0-9a-f]{0,4})*(?![\w:])/gi, "<ip>"],
  [/\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:com|net|org|io|cn|xyz|cloud|local|lan|internal|arpa)\b/gi, "<host>"],
  [/(\/Users|\/home)\/[^/\s'"]+/g, "~"],
  // Long runs of letters and digits together: keys, tokens, ids.
  [/(?<![A-Za-z0-9+/_-])(?=[A-Za-z0-9+/_-]*\d)(?=[A-Za-z0-9+/_-]*[A-Za-z])[A-Za-z0-9+/_-]{32,}={0,2}/g, "<redacted>"],
];

/** Free text as it may go into the package: URLs cut to their path, secrets, addresses and long keys replaced, home
 *  folders as `~`, at most MAX_TEXT bytes. */
export function scrubText(raw: string): string {
  let s = raw;
  for (const [rule, template] of RULES) s = s.replace(rule, template);
  return clip(s, MAX_TEXT);
}

/** `s` cut to at most `bytes` UTF-8 bytes on a character boundary, with an ellipsis when it was cut. */
export function clip(s: string, bytes: number): string {
  const enc = new TextEncoder();
  if (enc.encode(s).length <= bytes) return s;
  let out = "";
  let used = 0;
  for (const ch of s) {
    const n = enc.encode(ch).length;
    if (used + n > bytes - 3) break;
    out += ch;
    used += n;
  }
  return out + "…";
}

/** A code-chosen word (a socket message's type, a page's name) as it may be recorded: letters, digits and `_ - . :`
 *  only, at most 48 of them; anything else is `other`. */
export function devWord(raw: string): string {
  return raw && raw.length <= 48 && /^[A-Za-z0-9_.:-]+$/.test(raw) ? raw : "other";
}

/** A request's route as recorded: the method and the API path's pattern (no host, ids or query). */
export function devRoute(method: string, path: string): string {
  return `${devWord(method.toUpperCase())} ${devPathPattern(path)}`;
}
