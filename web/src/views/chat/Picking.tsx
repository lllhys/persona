// Picking messages at once (#3838, #9469; iOS PickRow / PickBar, and Inbox's Select, views/inbox/InboxList.tsx): a
// checkbox over each message with a Forward (PickRow, drawn by MessageActions in the row's first place) and the bar in the
// composer's place (PickBar): Cancel, how many are picked, Include questions when a picked reply's question is not picked,
// then Forward, Copy and Export image, off while none is picked; Escape cancels too, and Cancel leaves the chat as it was,
// the composer back with its draft. A question a picked reply brings has a lighter tick. What would send more than
// FORWARD_MOST is refused with a toast from the bar. A chunk of its own: the message menu's Select messages (startPick)
// and Forward to's Select messages… hand these to the chat (picking-parts.ts) as it starts picking, so nothing of it is on
// the first screen. Export image's dialog is a chunk of its own again (ExportImage.tsx), loaded on its first press.
import { lazy, Suspense, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { t } from "../../i18n";
import { forwardSummary, openForward, type ForwardChat, type ForwardPick } from "../../lib/forward";
import { forwardDraftOf } from "../../lib/forward-draft";
import { FORWARD_MOST, messagePick, questionsOf, type MessagePick } from "../../lib/message-pick";
import { copiedText, exportMessages, pickQuestions } from "../../lib/pick-export";
import { errorText } from "../../lib/api";
import { Button, copyText, cx, toast } from "../../ui/atoms";
import { CheckMark } from "../../ui/CheckMark";
import { Icon, type IconName } from "../../ui/icons";
import { NARROW, useMedia } from "../../ui/media-query";
import { pickParts } from "./picking-parts";

const ExportImage = lazy(() => import("./ExportImage"));

/** Forward to's Select messages… and the message menu's Select messages (#9469): what the chat draws while it picks goes
 *  to `pickParts`, and a pick starts with `first` (the bar reads its replies' questions from the chat). */
export function startPick(first: ForwardPick): MessagePick {
  pickParts.Row = PickRow;
  pickParts.Bar = PickBar;
  const pick = messagePick();
  pick.start(first);
  return pick;
}

export interface PickRowProps {
  pick: MessagePick;
  /** The message, as Forward would forward it. */
  message: ForwardPick;
  /** Your own bubble: the tick goes before it, outside it (its row is the bubble's width). */
  side?: boolean;
}

/** Over a message while the chat picks: a transparent checkbox the size of the message (what is under it is inert),
 *  the tick before it, and a tint around it on hover and once picked, as Inbox's picked cards. A question a picked reply
 *  brings (Include questions) has a lighter tick. Its message's row is `relative isolate` (and leaves the tick room
 *  unless `side`). */
export function PickRow({ pick, message, side }: PickRowProps) {
  const read = () => pick.has(message.itemId);
  const on = useSyncExternalStore(pick.subscribe, read, read);
  const readBrought = () => !pick.has(message.itemId) && pick.isBrought(message.itemId);
  const brought = useSyncExternalStore(pick.subscribe, readBrought, readBrought);
  const name = forwardSummary(message.text).replace(/\n/g, " ").slice(0, 120);
  return (
    <>
      <button
        type="button"
        role="checkbox"
        aria-checked={on}
        aria-label={brought ? `${name}, ${t("chat-forwardto.goes_with_answer")}` : name}
        onClick={() => void pick.toggle(message)}
        className="peer absolute -inset-1.5 z-10 cursor-pointer rounded-[16px]"
        data-pick={on ? "on" : brought ? "brought" : "off"}
      >
        {brought ? (
          <span aria-hidden className={cx("absolute flex size-4 items-center justify-center rounded-chip bg-accent-tint text-accent", side ? "top-5 right-full mr-1.5" : "top-2.5 left-1.5")}>
            <Icon name="check" size={10} stroke={3} />
          </span>
        ) : (
          <CheckMark on={on} className={cx("absolute", side ? "top-5 right-full mr-1.5" : "top-2.5 left-1.5")} />
        )}
      </button>
      <span aria-hidden className={cx("pointer-events-none absolute -inset-1.5 -z-10 rounded-[16px] transition-colors duration-150", on ? "bg-accent-tint" : "[@media(hover:hover)]:peer-hover:bg-hover")} />
    </>
  );
}

/** `picks` in the chat's order (several.json "three_then_a_note": the blocks go as the chat has them, not as they were
 *  picked): by where each message is in `items`, the transcript; one it no longer holds goes after, as it was picked. */
export function inChatOrder(picks: readonly ForwardPick[], items: readonly { id: string }[]): ForwardPick[] {
  const at = new Map<string, number>();
  items.forEach((it, i) => {
    if (!at.has(it.id)) at.set(it.id, i);
  });
  const place = (p: ForwardPick) => at.get(p.itemId) ?? Number.MAX_SAFE_INTEGER;
  return picks
    .map((p, i) => ({ p, i }))
    .sort((a, b) => place(a.p) - place(b.p) || a.i - b.i)
    .map((x) => x.p);
}

/** What goes: the picks and, with Include questions, the questions they bring, in the chat's order. */
function going(from: ForwardChat, pick: MessagePick, include = pick.include()): ForwardPick[] {
  const picks = pick.picks();
  return inChatOrder(include ? [...picks, ...questionsOf(picks, pickQuestions([...from.items]))] : picks, from.items);
}

/** Forward: Forward to with what goes as drafts, in the chat's order; the chat drops them once they went. */
function forward(from: ForwardChat, pick: MessagePick, sent: () => void) {
  const ordered = going(from, pick);
  if (ordered.length) openForward({ drafts: ordered.map((p) => forwardDraftOf(from, p)), sent });
}

export interface PickBarProps {
  pick: MessagePick;
  /** The chat they are picked in, as it is when an action is pressed (its transcript gives their order). */
  chat: () => ForwardChat;
  /** Cancel, Escape, or the picks went: the chat drops them. */
  onEnd: () => void;
}

/** What Export image draws: the chat, and what goes without and with the questions the picks bring, in its order. */
export interface ExportRequest {
  chat: ForwardChat;
  without: ForwardPick[];
  with: ForwardPick[];
}

/** The bar in the composer's place while the chat picks: Cancel, "2 selected" and Include questions, then Forward, Copy
 *  and Export image, off with none picked: one row, or on a phone two, the actions as the iPhone draws them. */
export function PickBar({ pick, chat, onEnd }: PickBarProps) {
  const count = useSyncExternalStore(pick.subscribe, pick.count, pick.count);
  const refusals = useSyncExternalStore(pick.subscribe, pick.refusals, pick.refusals);
  const offered = useSyncExternalStore(pick.subscribe, pick.offered, pick.offered);
  const include = useSyncExternalStore(pick.subscribe, pick.include, pick.include);
  const narrow = useMedia(NARROW);
  const [exporting, setExporting] = useState<ExportRequest | null>(null);
  const [copying, setCopying] = useState(false);
  const told = useRef(refusals);
  // Include questions: each picked reply's questions, read from the chat's transcript as it is at each pick
  useEffect(() => pick.setQuestions(() => pickQuestions([...chat().items])), [pick, chat]);
  useEffect(() => {
    if (refusals === told.current) return;
    told.current = refusals;
    toast(t("chat-forwardto.at_most", { count: FORWARD_MOST }), "error");
  }, [refusals]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // a dialog or menu over the chat takes its own Escape
      if (e.key !== "Escape" || e.defaultPrevented || document.querySelector("[role=dialog], [role=menu]")) return;
      onEnd();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onEnd]);

  const copy = async () => {
    const from = chat();
    setCopying(true);
    try {
      const messages = await exportMessages(going(from, pick).map((p) => forwardDraftOf(from, p)));
      if (await copyText(copiedText(messages))) onEnd();
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setCopying(false);
    }
  };
  const exportImage = () => {
    const from = chat();
    setExporting({ chat: from, without: going(from, pick, false), with: going(from, pick, true) });
  };
  const actions: { key: string; label: string; icon: IconName; run: () => void; busy?: boolean; primary?: boolean }[] = [
    { key: "forward", label: t("chat-forward.forward"), icon: "forward", run: () => forward(chat(), pick, onEnd) },
    { key: "copy", label: t("chat-messageactions.copy"), icon: "copy", run: () => void copy(), busy: copying },
    { key: "export", label: t("chat-forwardto.export_image"), icon: "image", run: exportImage, primary: true },
  ];
  const questions = offered && (
    <button
      type="button"
      role="switch"
      aria-checked={include}
      onClick={() => void pick.setInclude(!include)}
      className={cx("inline-flex h-7 shrink-0 touch-hit items-center gap-1.5 rounded-full px-2.5 text-label font-semibold whitespace-nowrap", include ? "bg-accent-tint text-accent" : "text-ink-2 shadow-hairline")}
      data-pick-questions={include ? "on" : "off"}
    >
      <CheckMark on={include} round />
      {t("chat-forwardto.include_questions")}
    </button>
  );
  const dialog = exporting && (
    <Suspense fallback={null}>
      <ExportImage request={exporting} pick={pick} onClose={() => setExporting(null)} />
    </Suspense>
  );
  if (narrow) {
    return (
      <div role="toolbar" aria-label={t("chat-forwardto.selection")} data-pick-bar className="flex shrink-0 flex-col gap-2">
        <div className="flex items-center gap-2">
          <Button tone="secondary" onClick={onEnd} testId="pick-cancel">
            {t("common.cancel")}
          </Button>
          <span className="min-w-0 flex-1 truncate text-center text-xs text-ink-3 tabular-nums" aria-live="polite" data-pick-count>
            {t("chat-forwardto.selected", { count })}
          </span>
          {questions}
        </div>
        <div className="flex rounded-[14px] bg-surface shadow-card">
          {actions.map((a) => (
            <button key={a.key} type="button" disabled={!count || a.busy} onClick={a.run} data-testid={`pick-${a.key}`}
              className="flex min-w-0 flex-1 flex-col items-center gap-0.5 py-2 text-submeta text-accent disabled:text-ink-3">
              <Icon name={a.icon} size={18} />
              <span className="max-w-full truncate">{a.label}</span>
            </button>
          ))}
        </div>
        {dialog}
      </div>
    );
  }
  return (
    <div role="toolbar" aria-label={t("chat-forwardto.selection")} data-pick-bar className="flex shrink-0 items-center gap-2 rounded-[14px] bg-surface p-2 shadow-card">
      <Button tone="quiet" onClick={onEnd} testId="pick-cancel">
        {t("common.cancel")}
      </Button>
      <span className="min-w-0 flex-1 truncate text-center text-xs text-ink-3 tabular-nums" aria-live="polite" data-pick-count>
        {t("chat-forwardto.selected", { count })}
      </span>
      {questions}
      {actions.map((a) => (
        <Button key={a.key} tone={a.primary ? "primary" : "secondary"} icon={a.icon} disabled={!count} busy={a.busy} onClick={a.run} testId={`pick-${a.key}`}>
          {a.label}
        </Button>
      ))}
      {dialog}
    </div>
  );
}
