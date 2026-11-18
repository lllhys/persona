// One open section of a grouped Agents list (views/agents/GroupedSessions.tsx): a SessionList that passes over the
// `sessions_list` hints naming none of its rows (#9176). With several sections open, each read for every agent's update
// anywhere: seven open to-dos made about seven reads a second. Kept out of session-list.ts, which the first screen loads.
import { SessionList, type ListChangeHint } from "./session-list";

export class SectionList<T> extends SessionList<T> {
  /** Where the hints were followed to without a read, and the read revision that was from: a read that moves
   *  `checkpoint` leaves it behind. */
  private followed?: { from: string; to: string };
  /** A hint's read held to the window's end while a read was out. */
  private held?: ReturnType<typeof setTimeout>;

  /** `hint` (the hub's message) is passed over when it starts where this list stands (its last read, or the hint before
   *  it passed over), names none of the rows it holds, and the list holds every row its filters select. The list then
   *  follows to the hint's revision, but its next read still starts at its last answer's: a row that joins it is named
   *  by no hint it could check, so the caller reads it when what shows a join changes (its heading's numbers). A read
   *  out or waiting, a gap, a hint without `since`/`changed` (an older hub, too many changes), or pages not read yet:
   *  read, as any list. */
  override invalidate(hint?: ListChangeHint): void {
    if (hint && this.passes(hint)) return;
    // One that lands while a read is out waits for the window's end, not the read's: a busy section read about twice
    // a window (SessionList.invalidate starts its trailing read as soon as the read is in).
    const wait = this.invalidatedAt + 2_000 - Date.now();
    if (this.work.promise && wait > 0) {
      this.held ??= setTimeout(() => {
        this.held = undefined;
        super.invalidate();
      }, wait);
      return;
    }
    super.invalidate();
  }

  override reset(): void {
    clearTimeout(this.held);
    this.held = undefined;
    super.reset();
  }

  private passes({ revision, since, changed }: ListChangeHint): boolean {
    const at = this.checkpoint;
    if (!at || this.cursor || this.invalidationQueued || this.work.promise || !this.snapshot.loaded || this.snapshot.error) return false;
    const head = this.followed?.from === at ? this.followed.to : at;
    if (typeof revision !== "string" || !revision || since !== head || !Array.isArray(changed)) return false;
    const held = this.candidates();
    try {
      if (changed.some((id) => held.has(this.removeIdentity(JSON.stringify(id))))) return false;
    } catch {
      return false; // not a row id this list can read: read
    }
    this.followed = { from: at, to: revision };
    return true;
  }
}
