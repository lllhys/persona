// Several forwards of one chat as one card (#3838; iOS ForwardMessage.runs / groupHeading / line): which make one, and
// its words. Only the group's bars and cards (views/chat/ForwardGroup.tsx, a chunk of their own) need them. No DOM here.
import { t } from "../i18n";
import { FORWARD_KIND, forwardOrigin, forwardSummary, shownAuthor } from "./forward";

type Attrs = Record<string, string | undefined> | undefined;

/** A block of a message as the bubble or the box holds it: a forward has a `url` naming its session. */
type Block = { kind: string; url?: string; attrs?: Attrs };

/** Forwards next to each other from the same session read as one card (several.json "groups"; iOS ForwardMessage.runs):
 *  the indices into `blocks` of each run of forwards, in order. Another session's forward or any other block ends a run;
 *  a forward whose `url` names no session is a run of its own. */
export function forwardRuns(blocks: readonly Block[]): number[][] {
  const runs: number[][] = [];
  let open = "";
  blocks.forEach((b, i) => {
    if (b.kind !== FORWARD_KIND) {
      open = "";
      return;
    }
    const o = forwardOrigin(b.url ?? "", b.attrs?.msg);
    const at = o ? `${o.machine}/${o.session}` : "";
    if (at && at === open) runs[runs.length - 1].push(i);
    else runs.push([i]);
    open = at;
  });
  return runs;
}

/** A group's first line: "3 forwarded messages · Network tests". */
export function forwardGroupHeading(group: readonly { attrs?: Attrs }[]): string {
  const chat = group[0]?.attrs?.chat;
  const count = group.length;
  return chat ? t("chat-forwardto.group_heading_chat", { count, chat }) : t("chat-forward.group_heading", { count });
}

/** One message of a group, on a line: "Claude: Three times, from RetryPolicy.swift." */
export function forwardLine(f: { text: string; attrs?: Attrs }): string {
  return t("chat-forwardto.line", { who: shownAuthor(f.attrs), text: forwardSummary(f.text).replace(/\n/g, " ") });
}
