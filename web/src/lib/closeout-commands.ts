// Commands a closeout says it ran, checked only against the tool calls and results in that closeout's own exchange.
// Extraction and matching are deliberately conservative: prose is not a command, a shell echo is not a run, and a
// narrower test selection does not stand for the claimed one. The result restates the record; it never judges the claim.
import { buildTaskIndex } from "./tasks";
import { exchangeSteps, type Segment } from "./turns";
import type { Item } from "./types";

const MAX_CLAIMS = 8;
const MAX_RUNES = 300;
const COMMAND_TOOLS = new Set(["Bash", "exec_command", "shell", "PowerShell"]);
const HEAD = /^(?:(?:go|bun|bunx|npm|npx|pnpm|yarn|pytest|cargo|make|swift)(?:\s|$)|python3?\s|node\s|\.\/gradlew(?:\s|$)|\.\/scripts\/)/;
const START = /(?:^|(?<=[\s，。；;、：:（(]))(?=(?:go|bun|bunx|npm|npx|pnpm|yarn|pytest|cargo|make|swift|python3?\s|node\s|\.\/gradlew|\.\/scripts\/))/g;
const STOP = /[\p{Script=Han}，。；;、（）()：:→`",]/u;
const OUTCOME = /^(?:pass(?:ed|es|ing)?|ok|fail(?:ed|s|ing|ures?)?|green|succeeded|success(?:ful)?|clean|✓|✗)$/i;
const COUNT = /^\d+(?:\/\d+)?$/;
const INLINE_CODE = /`([^`\n]+)`/g;
const GO = /^go (?:test|build|vet|run|generate|mod|install|fmt|list|tool|work)(?:\s|$)/;
const PROSE_MAKE = /^make (?:sure|it|the|a|an|to|sense|this|that|up)(?:\s|$)/i;
const ASSIGNMENT = /^[A-Za-z_][A-Za-z0-9_]*=/;
const DURATION = /^[0-9]+(?:\.[0-9]+)?[smhd]?$/;
// Extra options are safe only when they cannot narrow the claimed test or package selection. Everything else is left unmatched.
const SAFE_EXTRA = /^(?:-v+|-q|-race|-cover|-coverprofile=\S+|-count=[1-9]\d*|-timeout=\S+|-parallel=\d+|-shuffle=\S+|-json|--verbose|--quiet|--watch(?:=\S+)?|--runInBand|--maxWorkers=\d+|--max-workers=\d+|--workers=\d+|--coverage|--ci|--color|--no-color|--silent|--bail(?:=\d+)?|-x|--fail-fast|--no-fail-fast|--show-output|--nocapture|-s)$/;
const SAFE_CI = /^CI=(?:0|1|true|false)$/i;
const CONDITIONAL = new Set(["if", "then", "elif", "else", "fi", "case", "for", "while", "until", "select", "do", "done"]);

function harmlessAssignment(word: string): boolean {
  if (SAFE_CI.test(word)) return true;
  if (!word.startsWith("GOFLAGS=")) return false;
  const flags = word.slice("GOFLAGS=".length).trim();
  return Boolean(flags) && flags.split(/\s+/).every((flag) => SAFE_EXTRA.test(flag));
}

export type CloseoutCommandState = "exit" | "error" | "background" | "running" | "not_found" | "unavailable";

export interface CloseoutCommandEvidence {
  claim: string;
  /** The whole recorded shell command, not the normalized words used for matching. */
  command?: string;
  state: CloseoutCommandState;
  exitCode?: number;
  isError?: boolean;
  background?: boolean;
  /** A known result exists but its text was cut, or no result record arrived. */
  reason?: "truncated" | "no_result";
}

const tidy = (text: string) => text.replace(/\s+/g, " ").trim();
const base = (word: string) => word.slice(word.lastIndexOf("/") + 1);

function dropOutcome(fragment: string): string {
  const words = fragment.split(" ");
  while (words.length > 1) {
    const last = words.at(-1)!;
    if (!OUTCOME.test(last) && !(COUNT.test(last) && !words.at(-2)!.startsWith("-"))) break;
    words.pop();
  }
  return words.join(" ");
}

/** Recognizable commands written without inline code. A command's own arguments do not open another candidate. */
function bareCommands(text: string): string[] {
  const found: string[] = [];
  const heredocs: string[] = [];
  for (const line of text.split("\n")) {
    if (heredocs.length) {
      if (line.replace(/\r$/, "") === heredocs[0]) heredocs.shift();
      continue;
    }
    for (const doc of line.matchAll(/<<-?\s*(['"]?)([A-Za-z0-9_]+)\1/g)) heredocs.push(doc[2]);
    let end = 0;
    for (const match of line.matchAll(START)) {
      const at = match.index!;
      if (at < end || /\becho\s+$/i.test(line.slice(0, at))) continue;
      let fragment = line.slice(at);
      const stop = fragment.search(STOP);
      if (stop >= 0) fragment = fragment.slice(0, stop);
      end = at + fragment.length;
      fragment = dropOutcome(tidy(fragment));
      if (!HEAD.test(`${fragment} `) || PROSE_MAKE.test(fragment) || (/^go(?:\s|$)/.test(fragment) && !GO.test(fragment))) continue;
      found.push(fragment);
    }
  }
  return found;
}

function recognizable(text: string): boolean {
  return commandSegments(text).some(({ words }) => {
    const command = words.join(" ");
    return HEAD.test(`${command} `) && !PROSE_MAKE.test(command) && (!/^go(?:\s|$)/.test(command) || GO.test(command));
  });
}

function validClaim(text: string): boolean {
  return text.length > 0 && [...text].length <= MAX_RUNES && !/[\p{Cc}\p{Cf}]/u.test(text);
}

/** Commands explicitly named by the closeout's free-text Verified line, in their written order. */
export function closeoutCommandClaims(verified: string): string[] {
  const quoted = [...verified.matchAll(INLINE_CODE)]
    .map((match) => tidy(match[1]!).replace(/^\$\s+/, ""))
    .filter((text) => validClaim(text) && recognizable(text));
  const candidates = quoted.length ? quoted : bareCommands(verified.replace(INLINE_CODE, " "));
  const claims: string[] = [];
  for (const candidate of candidates) {
    const text = tidy(candidate);
    if (claims.length === MAX_CLAIMS) break;
    if (validClaim(text) && recognizable(text) && !claims.includes(text)) claims.push(text);
  }
  return claims;
}

type ShellOutcome = "none" | "zero" | "direct";

interface DirectoryContext {
  path?: string;
  outcome: ShellOutcome;
}

interface CommandPart {
  words: string[];
  /** The static directory in effect for this command; an unscoped claim does not constrain it. */
  directory?: string;
  /** Whether the recorded result proves that the directory change actually happened. */
  directoryOutcome: ShellOutcome;
  /** Whether the tool result is this command's result, or proves it only when the whole shell exited zero. */
  outcome: ShellOutcome;
}

function nestedOutcome(outer: ShellOutcome, inner: ShellOutcome): ShellOutcome {
  if (outer === "none" || inner === "none") return "none";
  return outer === "direct" ? inner : "zero";
}

function outcomeProven(outcome: ShellOutcome, exitCode: number | undefined): boolean {
  return outcome === "direct" || (outcome === "zero" && exitCode === 0);
}

/** The simple commands of a shell line, retaining directory and outcome context and discarding harmless wrappers. */
function commandSegments(command: string, depth = 2, inherited: DirectoryContext = { outcome: "none" }): CommandPart[] {
  const out: CommandPart[] = [];
  let directory = inherited;
  let shellOptionsChanged = false;
  for (const raw of shellSegments(command)) {
    const words = commandWords(raw.words);
    if (!words.length || CONDITIONAL.has(base(words[0]))) return [];
    const script = shellScript(words);
    if (script !== undefined && depth > 0) {
      out.push(...commandSegments(script, depth - 1, directory).map((part) => ({
        ...part,
        directoryOutcome: nestedOutcome(raw.outcome, part.directoryOutcome),
        outcome: nestedOutcome(raw.outcome, part.outcome),
      })));
      continue;
    }
    if (["pushd", "popd", "dirs"].includes(base(words[0]))) return [];
    if (["set", "shopt", "setopt", "unsetopt"].includes(base(words[0]))) {
      shellOptionsChanged = true;
      continue;
    }
    if (base(words[0]) === "cd") {
      const target = withoutRedirections(words);
      if (target.length !== 2) return [];
      directory = { path: target[1], outcome: raw.outcome };
      continue;
    }
    const simple = withoutRedirections(words);
    if (simple.length) out.push({ words: simple, directory: directory.path, directoryOutcome: directory.outcome, outcome: raw.outcome });
  }
  return shellOptionsChanged ? out.map((part) => ({ ...part, directoryOutcome: "none", outcome: "none" })) : out;
}

function shellScript(words: string[]): string | undefined {
  if (words.length !== 3 || !["sh", "bash", "zsh", "dash"].includes(base(words[0]))) return undefined;
  return ["-c", "-lc", "-ic", "-lic"].includes(words[1]) ? words[2] : undefined;
}

function commandWords(input: string[]): string[] {
  let words = input;
  for (let guard = 0; words.length > 0 && guard < 20; guard++) {
    const first = base(words[0]);
    if (ASSIGNMENT.test(words[0])) {
      if (!harmlessAssignment(words[0])) return [];
      words = words.slice(1);
      continue;
    }
    if (first === "env") {
      words = words.slice(1);
      while (words.length && words[0].startsWith("-")) {
        const flag = words[0];
        if (flag === "--") { words = words.slice(1); break; }
        if (flag.startsWith("-C") || flag === "--chdir" || flag.startsWith("--chdir=")) return [];
        const valued = flag === "-u" || flag === "--unset";
        if (valued && words.length < 2) return [];
        words = words.slice(valued ? 2 : 1);
      }
      continue;
    }
    if (first === "command") { words = skipFlags(words.slice(1)); continue; }
    if (first === "time" || first === "nohup") { words = skipFlags(words.slice(1)); continue; }
    if (first === "nice") { words = skipFlags(words.slice(1), "-n", "--adjustment"); continue; }
    if (first === "timeout") {
      words = skipFlags(words.slice(1), "-s", "-k", "--signal", "--kill-after");
      if (words.length && DURATION.test(words[0])) words = words.slice(1);
      continue;
    }
    return words;
  }
  return [];
}

function skipFlags(words: string[], ...valued: string[]): string[] {
  while (words.length && words[0].startsWith("-")) {
    const flag = words[0];
    words = words.slice(1);
    if (flag === "--") break;
    if (valued.includes(flag) && words.length) words = words.slice(1);
  }
  return words;
}

/** Redirections affect output, not which packages or tests the command selects. Here-document bodies never reach this list. */
function withoutRedirections(words: string[]): string[] {
  const out: string[] = [];
  for (let i = 0; i < words.length; i++) {
    const word = words[i];
    if (/^(?:\d+)?(?:>>?|<|&>)$/.test(word)) { i++; continue; }
    if (/^(?:\d+)?(?:>>?|<|&>)\S+$/.test(word) || /^\d+>&\d+$/.test(word)) continue;
    out.push(word);
  }
  return out;
}

interface Heredoc { delim: string; strip: boolean }

interface ShellSegment {
  words: string[];
  outcome: ShellOutcome;
}

type ShellJoin = "start" | "sequence" | "and" | "pipeline";

/** Enough shell tokenization for comparison, not execution: quotes, separators, substitutions and here-documents. */
function shellSegments(command: string): ShellSegment[] {
  const segments: string[][] = [];
  const joins: ShellJoin[] = [];
  let join: ShellJoin = "start";
  let words: string[] = [];
  let word = "";
  let inWord = false;
  let subst = 0;
  let invalid = false;
  let incomplete = false;
  let pending: Heredoc[] = [];
  const flush = () => {
    if (!inWord) return;
    words.push(word);
    word = "";
    inWord = false;
    incomplete = false;
  };
  const end = () => {
    flush();
    if (!words.length) return false;
    segments.push(words);
    joins.push(join);
    words = [];
    subst = 0;
    return true;
  };
  const separate = (next: ShellJoin) => {
    if (!end()) { invalid = true; return; }
    join = next;
    incomplete = next === "and" || next === "pipeline";
  };
  const add = (text: string) => { word += text; inWord = true; };
  for (let i = 0; i < command.length; i++) {
    const c = command[i];
    if (c === "\\") {
      if (i + 1 < command.length) {
        i++;
        if (command[i] !== "\n") add(command[i]);
      }
    } else if (c === "'" || c === "`") {
      let close = command.indexOf(c, i + 1);
      if (close < 0) close = command.length;
      const text = command.slice(i + 1, close);
      add(c === "`" ? `\`${text}\`` : text);
      i = close;
    } else if (c === '"') {
      inWord = true;
      i = readDoubleQuoted(command, i + 1, (text) => { word += text; });
    } else if (c === "#" && !inWord) {
      const next = command.indexOf("\n", i);
      i = next < 0 ? command.length : next - 1;
    } else if (c === "\n") {
      if (words.length || inWord) separate("sequence");
      i = skipHeredocs(command, i + 1, pending) - 1;
      pending = [];
    } else if (c === " " || c === "\t" || c === "\r") {
      flush();
    } else if (c === ";" || c === "|") {
      if (c === "|" && command[i + 1] === "|") { invalid = true; i++; }
      else {
        separate(c === ";" ? "sequence" : "pipeline");
        if (c === "|" && command[i + 1] === "&") i++;
      }
    } else if (c === "&") {
      if (command[i + 1] === "&") { separate("and"); i++; }
      else if (command[i + 1] === ">" || word.endsWith(">") || word.endsWith("<")) add("&");
      else invalid = true;
    } else if (c === "<" && command[i + 1] === "<" && command[i + 2] !== "<") {
      flush();
      const doc = heredocDelimiter(command, i + 2);
      if (doc.value.delim) pending.push(doc.value);
      i = doc.next - 1;
    } else if (c === "$" && command[i + 1] === "(") {
      add("$("); subst++; i++;
    } else if (c === "(" || c === ")") {
      if (c === ")" && subst > 0) { add(")"); subst--; }
      else invalid = true;
    } else add(c);
  }
  end();
  if (incomplete) invalid = true;
  if (invalid) return [];
  const parsed = segments.map((words) => ({ words, outcome: "none" as ShellOutcome }));
  for (let i = parsed.length - 1; i >= 0; i--) {
    if (i === parsed.length - 1) {
      let first = i;
      while (first > 0 && joins[first] === "pipeline") first--;
      parsed[i].outcome = joins[first] === "and" ? "zero" : "direct";
    } else if (joins[i + 1] === "and") parsed[i].outcome = "zero";
    else if (joins[i + 1] === "sequence") break;
  }
  return parsed;
}

function readDoubleQuoted(text: string, start: number, add: (text: string) => void): number {
  for (let i = start; i < text.length; i++) {
    const c = text[i];
    if (c === '"') return i;
    if (c === "\\" && i + 1 < text.length && '"\\$`\n'.includes(text[i + 1])) {
      i++;
      if (text[i] !== "\n") add(text[i]);
    } else add(c);
  }
  return text.length - 1;
}

function heredocDelimiter(text: string, start: number): { value: Heredoc; next: number } {
  let i = start;
  const value = { delim: "", strip: false };
  if (text[i] === "-") { value.strip = true; i++; }
  while (i < text.length && (text[i] === " " || text[i] === "\t")) i++;
  for (; i < text.length && !" \t\r\n;&|<>()".includes(text[i]); i++) {
    if (!['"', "'", "\\"].includes(text[i])) value.delim += text[i];
  }
  return { value, next: i };
}

function skipHeredocs(text: string, start: number, pending: Heredoc[]): number {
  let i = start;
  for (const doc of pending) {
    while (i < text.length) {
      let line = text.slice(i).split("\n", 1)[0];
      const next = text.indexOf("\n", i);
      i = next < 0 ? text.length : next + 1;
      if (doc.strip) line = line.replace(/^\t+/, "");
      if (line.replace(/\r$/, "") === doc.delim) break;
    }
  }
  return i;
}

function sameWords(want: string[], got: string[]): boolean {
  if (!want.length || want.length > got.length || want.some((word, i) => word !== got[i])) return false;
  let extra = got.slice(want.length);
  if (extra[0] === "--") extra = extra.slice(1);
  return extra.every((word) => SAFE_EXTRA.test(word));
}

function samePart(want: CommandPart, got: CommandPart): boolean {
  return (want.directory === undefined || want.directory === got.directory) && sameWords(want.words, got.words);
}

interface MatchedPart {
  part: CommandPart;
  requireDirectory: boolean;
}

/** Claimed commands run in order; a directory named by the claim must be the directory in effect for the recorded command. */
function segmentsMatch(claim: CommandPart[], run: CommandPart[]): MatchedPart[] | undefined {
  if (!claim.length) return undefined;
  const matched: MatchedPart[] = [];
  let at = 0;
  for (const want of claim) {
    while (at < run.length && !samePart(want, run[at])) at++;
    if (at === run.length) return undefined;
    matched.push({ part: run[at], requireDirectory: want.directory !== undefined });
    at++;
  }
  return matched;
}

function commandText(input: Record<string, unknown> | undefined): string {
  const quote = (word: unknown) => {
    const text = String(word);
    return /^[A-Za-z0-9_./:=+,%@^-]+$/.test(text) ? text : `'${text.replace(/'/g, `'"'"'`)}'`;
  };
  if (Array.isArray(input?.command)) return input.command.map(quote).join(" ");
  const command = input?.command ?? input?.cmd;
  return typeof command === "string" ? command.trim() : "";
}

function numericExit(item: Item | undefined): number | undefined {
  const code = item?.outcome?.exit_code;
  return typeof code === "number" && Number.isSafeInteger(code) ? code : undefined;
}

/** Match only this segment's normalized calls. Folded or missing records are unavailable, never proof of a non-run. */
export function closeoutCommandEvidence(verified: string, segment: Segment | undefined): CloseoutCommandEvidence[] {
  const claims = closeoutCommandClaims(verified);
  if (!segment) return claims.map((claim) => ({ claim, state: "unavailable" }));
  if (segment.folds.length) return claims.map((claim) => ({ claim, state: "unavailable" }));
  const steps = exchangeSteps(segment);
  const calls = steps.flatMap((step) => step.kind === "tool" && COMMAND_TOOLS.has(step.use.tool_name ?? "") && commandText(step.use.tool_input) ? [{ use: step.use, result: step.result, command: commandText(step.use.tool_input), segments: commandSegments(commandText(step.use.tool_input)) }] : []);
  const items = steps.flatMap((step) => step.kind === "tool" ? [step.use, ...(step.result ? [step.result] : [])] : [step.item]);
  const tasks = buildTaskIndex(items);
  return claims.map((claim) => {
    const wanted = commandSegments(claim);
    let found: (typeof calls)[number] | undefined;
    let matched: MatchedPart[] | undefined;
    for (let i = calls.length - 1; i >= 0; i--) {
      const parts = segmentsMatch(wanted, calls[i].segments);
      if (parts) { found = calls[i]; matched = parts; break; }
    }
    if (!found || !matched) return { claim, state: "not_found" };
    const call = found.use.tool_use_id ?? "";
    const notes = tasks.notes.get(call) ?? [];
    const last = [...notes].reverse().find((note) => note.status !== "event");
    const background = found.result?.outcome?.status === "background" || found.result?.outcome?.status === "timed_out" || tasks.started.has(call);
    const running = last?.status === "running" || last?.status === "waiting";
    const exitCode = numericExit(found.result) ?? (typeof last?.exit_code === "number" && Number.isSafeInteger(last.exit_code) ? last.exit_code : undefined);
    const isError = typeof found.result?.is_error === "boolean" ? found.result.is_error : undefined;
    const attributed = matched.every(({ part, requireDirectory }) =>
      outcomeProven(part.outcome, exitCode) && (!requireDirectory || outcomeProven(part.directoryOutcome, exitCode)));
    const common = { claim, command: found.command, exitCode: attributed ? exitCode : undefined, isError: attributed ? isError : undefined, background: background || undefined };
    if (!found.result) return { ...common, state: "unavailable", reason: "no_result" };
    if (!attributed) return { ...common, state: "unavailable" };
    if (found.use.full_size || found.result.full_size) return { ...common, state: "unavailable", reason: "truncated" };
    if (exitCode !== undefined) return { ...common, state: "exit" };
    if (running) return { ...common, state: "running" };
    if (background) return { ...common, state: "background" };
    if (isError !== undefined) return { ...common, state: "error" };
    return { ...common, state: "unavailable" };
  });
}
