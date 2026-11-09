// A reply's lines as a chat shows them (#2088), the same rule as the Apple apps' `ReplyBreaks` (PersonaKit).
//
// `replyBreaks`: CommonMark lets only a list that starts at 1 interrupt a paragraph, so steps going on at "5." right
// under a line of text joined that paragraph. A blank line goes in where a line of a paragraph is directly followed by
// an ordered list item (any number) or a heading; nothing changes inside a fenced code block, in indented code, or inside
// a list (a blank line there would make it loose or end it).
//
// `remarkSoftBreaks`: a single line break inside a paragraph is a line break, as in the terminal and Claude's own web
// chat, not a space (remark-breaks' rule; code and formulas are their own nodes and keep their lines).

interface Fence {
  char: string;
  count: number;
}

// The fence a trimmed line opens: three or more backticks or tildes; a backtick run with another backtick after it is inline code.
function opening(line: string): Fence | null {
  if (!line.startsWith("```") && !line.startsWith("~~~")) return null;
  const char = line[0];
  let count = 0;
  while (line[count] === char) count++;
  if (char === "`" && line.slice(count).includes("`")) return null;
  return { char, count };
}

function closes(fence: Fence, line: string): boolean {
  let run = 0;
  while (line[run] === fence.char) run++;
  return run >= fence.count && /^[ \t]*$/.test(line.slice(run));
}

// Leading spaces, a tab counting as four.
function indent(line: string): number {
  let n = 0;
  for (const c of line) {
    if (c === " ") n += 1;
    else if (c === "\t") n += 4;
    else break;
  }
  return n;
}

const ORDERED = /^\d{1,9}[.)](?:[ \t]|$)/;
const BULLET = /^[-*+](?:[ \t]|$)/;
const HEADING = /^#{1,6}(?:[ \t]|$)/;

export function replyBreaks(text: string): string {
  if (!text.includes("\n")) return text;
  const out: string[] = [];
  let fence: Fence | null = null;
  let inList = false; // since a list item, up to a blank line: the lines in between are that item's
  let afterText = false; // the previous line is a line of text (a paragraph's, a table row, a quote)
  let changed = false;
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (fence) {
      if (closes(fence, trimmed)) fence = null;
      out.push(line);
      afterText = false;
      continue;
    }
    if (!trimmed) {
      out.push(line);
      inList = false;
      afterText = false;
      continue;
    }
    const top = indent(line) < 4;
    const open = top ? opening(trimmed) : null;
    if (open) {
      fence = open;
      out.push(line);
      afterText = false;
      continue;
    }
    const ordered = top && ORDERED.test(trimmed);
    const bullet = top && BULLET.test(trimmed);
    const heading = top && HEADING.test(trimmed);
    if ((ordered || heading) && afterText && !inList) {
      out.push("");
      changed = true;
    }
    out.push(line);
    if (ordered || bullet) inList = true;
    if (heading) inList = false;
    afterText = !(ordered || bullet || heading || !top);
  }
  return changed ? out.join("\n") : text;
}

interface MdNode {
  type: string;
  value?: string;
  children?: MdNode[];
}

export function remarkSoftBreaks() {
  const walk = (node: MdNode) => {
    if (!node.children) return;
    const out: MdNode[] = [];
    for (const c of node.children) {
      if (c.type === "text" && c.value?.includes("\n")) {
        c.value.split(/\r?\n/).forEach((part, i) => {
          if (i) out.push({ type: "break" });
          if (part) out.push({ type: "text", value: part });
        });
      } else {
        walk(c);
        out.push(c);
      }
    }
    node.children = out;
  };
  return (tree: MdNode) => walk(tree);
}
