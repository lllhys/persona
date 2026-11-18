export interface TimelineMessage {
  readonly id: string;
  readonly role: string;
  readonly at: number;
}
export interface TimelineCard {
  readonly id: string;
  readonly at: number;
}
export interface TimelineEntry {
  readonly id: string;
  readonly messageId: string;
  readonly cardId: string;
}

/** A card follows its exchange, before the next input, so it never splits a streamed answer. */
export function mergeConversationTimeline(messages: readonly TimelineMessage[], cards: readonly TimelineCard[]): TimelineEntry[] {
  const byId = new Map<string, TimelineCard>();
  for (const card of cards) if (card.id) byId.set(card.id, card);
  const ordered = [...byId.values()].sort((a, b) => a.at - b.at || a.id.localeCompare(b.id));
  const result: TimelineEntry[] = [];
  let cursor = 0;
  const appendCard = (card: TimelineCard): void => { result.push({ id: 'feed:' + card.id, messageId: '', cardId: card.id }); };
  for (const message of messages) {
    if (message.role === 'user' && message.at > 0) {
      while (cursor < ordered.length && ordered[cursor].at < message.at) appendCard(ordered[cursor++]);
    }
    result.push({ id: 'message:' + message.id, messageId: message.id, cardId: '' });
  }
  while (cursor < ordered.length) appendCard(ordered[cursor++]);
  return result;
}

export interface TimelineRowIdentity { readonly id: string; }

/** Keep one observed array and describe insertion positions to native Repeat/List. */
export function reconcileTimelineRows<T extends TimelineRowIdentity>(current: T[], next: readonly T[]): void {
  const wanted = new Set(next.map((row: T) => row.id));
  for (let index = current.length - 1; index >= 0; index--) if (!wanted.has(current[index].id)) current.splice(index, 1);
  const retained = new Set(current.map((row: T) => row.id));
  let index = 0;
  while (index < next.length) {
    if (current[index]?.id === next[index].id) {
      if (current[index] !== next[index]) current[index] = next[index];
      index++;
    } else if (retained.has(next[index].id)) {
      const previous = current.findIndex((row: T) => row.id === next[index].id);
      current.splice(previous, 1); current.splice(index, 0, next[index]); index++;
    } else {
      let end = index + 1;
      while (end < next.length && !retained.has(next[end].id)) end++;
      current.splice(index, 0, ...next.slice(index, end)); index = end;
    }
  }
}
