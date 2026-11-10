import { createContext, useContext, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { t } from "../i18n";
import { copyText, cx } from "./atoms";
import { Icon } from "./icons";

/** The reply around these blocks is still being written: its quotes do not fold yet (they would fold and open as the
 *  words came). Markdown provides it. */
export const ReplyStreaming = createContext(false);

/** A code block's title bar, as Telegram draws it (#2984): the fence's language on the left (none when it names
 *  none), Copy on the right, which takes the whole block. Copy is 24 px drawn, 44 px to the finger (`touch-hit`). */
export function CodeBar({ lang, onCopy }: { lang: string; onCopy: () => void }) {
  return (
    <div className="flex h-8 items-center gap-2 bg-ink/[0.05] pr-1.5 pl-3 text-[12px] text-ink-3" data-code-bar>
      {lang && <span className="min-w-0 truncate font-medium">{lang}</span>}
      <button
        type="button"
        aria-label={t("ui-markdown.copy_code")}
        onClick={onCopy}
        className="ml-auto flex h-6 touch-hit items-center gap-1 rounded-[6px] px-1.5 whitespace-nowrap transition-colors hover:bg-hover-2 hover:text-ink focus-visible:text-ink"
      >
        <Icon name="copy" size={13} />
        {t("ui-markdown.copy")}
      </button>
    </div>
  );
}

/** Inline code: a click copies it (#2984), as Telegram does. Not inside a link (a path that opens is a link, and the
 *  link goes first), and not at the end of a drag that selected words; Enter or Space with it focused copies too. */
export function copyInlineCode(e: React.MouseEvent<HTMLElement> | React.KeyboardEvent<HTMLElement>) {
  const el = e.currentTarget;
  if (el.closest("a")) return;
  if ("key" in e && e.key !== "Enter" && e.key !== " ") return;
  if (!("key" in e) && window.getSelection()?.toString()) return;
  e.preventDefault();
  void copyText(el.textContent ?? "");
}

/** About four lines of a quote show while it is folded. */
const FOLD_PX = 104;

/** A reply's quote, as Telegram draws it (#2984): a bar in the accent on the left of a faint accent card, a quote mark
 *  at its top right (styles.css `.prose-j blockquote`). A long one is folded to about four lines, fading out; a click on
 *  it opens it, its chevron folds it again. */
export function ReplyQuote({ children }: { children?: ReactNode }) {
  const streaming = useContext(ReplyStreaming);
  const body = useRef<HTMLDivElement>(null);
  const [long, setLong] = useState(false);
  const [open, setOpen] = useState(false);
  useLayoutEffect(() => {
    const el = body.current;
    if (el) setLong(el.scrollHeight > FOLD_PX + 24);
  }, [children]);
  const folds = long && !streaming;
  const folded = folds && !open;
  return (
    <blockquote className="relative" data-reply-quote>
      <div
        ref={body}
        onClick={folded ? () => setOpen(true) : undefined}
        className={cx(folded && "cursor-pointer overflow-hidden [mask-image:linear-gradient(to_bottom,black_65%,transparent)]")}
        style={folded ? { maxHeight: FOLD_PX } : undefined}
      >
        {children}
      </div>
      <Icon name="quote" size={14} className="absolute top-2 right-2 text-accent-ink" aria-hidden />
      {folds && (
        <button
          type="button"
          aria-expanded={open}
          aria-label={open ? t("ui-markdown.quote_less") : t("ui-markdown.quote_more")}
          onClick={() => setOpen((o) => !o)}
          className="absolute right-1 bottom-1 flex size-6 touch-hit items-center justify-center rounded-[6px] text-accent-ink transition-colors hover:bg-hover-2"
          data-quote-fold
        >
          <Icon name="chevron" size={14} className={cx(open && "rotate-180")} />
        </button>
      )}
    </blockquote>
  );
}

type HNode = { type: string; tagName?: string; properties?: Record<string, unknown>; children?: HNode[] };

/** A rehype plugin, last: inline code inside a link (a path that opens, a written link) is marked `data-linked`, so
 *  it stays the link's and is not a copy button of its own inside it (Markdown's `code`). */
export function rehypeLinkedCode() {
  return (tree: HNode) => mark(tree, false);
}

function mark(node: HNode, inLink: boolean) {
  for (const c of node.children ?? []) {
    if (c.type !== "element") continue;
    if (c.tagName === "code" && inLink) c.properties = { ...c.properties, dataLinked: true };
    mark(c, inLink || c.tagName === "a");
  }
}
