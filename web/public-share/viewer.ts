import type { PublicPage } from "../src/sharing/types";
import { PublicReadError, readPublic } from "./api";
export interface ViewerState { phase: "loading" | "ready" | "hidden" | "unavailable" | "error"; page: PublicPage | null; busy: boolean }
export class Viewer {
  state: ViewerState = { phase: "loading", page: null, busy: false };
  private generation = 0;
  private controller?: AbortController;
  private flight = false;
  private pending = false;
  private hidden = false;
  private stopped = false;
  private failures = 0;
  private notBefore = 0;
  private poll?: ReturnType<typeof setTimeout>;
  private expiry?: ReturnType<typeof setTimeout>;
  constructor(private readonly token: string, private readonly changed: (state: ViewerState) => void, private readonly read = readPublic) {}
  private emit(phase: ViewerState["phase"], page: PublicPage | null = this.state.page) {
    this.state = { phase, page, busy: this.flight }; this.changed(this.state);
  }
  start() { void this.refresh(); }
  dispose() { this.stopped = true; this.invalidate(); }
  private invalidate() { this.generation++; this.controller?.abort(); clearTimeout(this.poll); clearTimeout(this.expiry); }
  visibility(hidden: boolean) {
    if (this.stopped || hidden === this.hidden) return;
    this.hidden = hidden; this.invalidate();
    // Even bfcache/resume must not paint previously authorized content before a fresh root read.
    this.emit(hidden ? "hidden" : "loading", null);
    this.pending = !hidden; if (!hidden) void this.refresh();
  }
  retry() { if (!this.stopped && !this.hidden) void this.refresh(); }
  private unavailable() { this.stopped = true; this.invalidate(); this.emit("unavailable", null); }
  private armExpiry(page: PublicPage) {
    clearTimeout(this.expiry);
    const remaining = Date.parse(page.expires_at) - Date.now();
    if (remaining <= 0) { this.unavailable(); return false; }
    this.expiry = setTimeout(() => { if (Date.parse(page.expires_at) <= Date.now()) this.unavailable(); else this.armExpiry(page); }, Math.min(remaining, 2_147_483_647)); return true;
  }
  private schedule(ms: number) {
    clearTimeout(this.poll); if (this.stopped || this.hidden) return;
    this.poll = setTimeout(() => void this.refresh(), Math.min(ms, 2_147_483_647));
  }
  async refresh(cursor?: string) {
    if (this.stopped || this.hidden) return;
    if (this.flight) { if (!cursor) this.pending = true; return; }
    if (Date.now() < this.notBefore) { this.schedule(this.notBefore - Date.now()); return; }
    const base = this.state.page;
    if (cursor && (!base || cursor !== base.next_cursor)) return;
    this.flight = true; this.pending = false;
    const generation = this.generation; const controller = this.controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15_000);
    this.emit(this.state.phase);
    try {
      const page = await this.read(this.token, cursor, controller.signal);
      if (this.stopped || this.hidden || generation !== this.generation) return;
      if (!this.armExpiry(page)) return;
      if (cursor && base) {
        if (base.revision !== page.revision || base.content_version !== page.content_version) {
          this.generation++; this.emit("loading", null); this.pending = true; return;
        }
        const ids = new Set(base.messages.map((m) => m.message_id));
        if (page.messages.some((m) => ids.has(m.message_id)) || base.messages.length + page.messages.length > 10000) throw new PublicReadError(0, "invalid_response");
        const combined = { ...page, messages: [...base.messages, ...page.messages] };
        if (new TextEncoder().encode(JSON.stringify(combined)).length > 16 * 1024 * 1024) throw new PublicReadError(0, "response_limit");
        this.emit("ready", combined);
      } else {
        // Equivalent validity reads preserve loaded history and manual disclosures. A policy/content change retires all cursors.
        this.emit("ready", base && base.revision === page.revision && base.content_version === page.content_version ? base : page);
      }
      this.failures = 0;
      this.schedule(Math.max(page.publication_status === "live" ? 15000 : 30000, page.poll_after_seconds * 1000));
    } catch (error) {
      if (this.stopped || this.hidden || generation !== this.generation) return;
      if (error instanceof PublicReadError && error.status === 404) return this.unavailable();
      if (error instanceof PublicReadError && error.status === 409) {
        this.emit("loading", null); this.pending = true;
        // Avoid an invalid-cursor loop: the next request has no cursor.
        this.notBefore = Date.now() + 1000;
      } else {
        this.emit("error", null);
        const backoff = Math.min(300000, 5000 * 2 ** Math.min(this.failures++, 6));
        this.notBefore = Date.now() + Math.max(backoff, error instanceof PublicReadError ? error.retryAfter : 0);
        this.schedule(this.notBefore - Date.now());
      }
    } finally {
      clearTimeout(timer); this.flight = false;
      if (!this.stopped && !this.hidden && generation === this.generation) this.emit(this.state.phase);
      if (this.pending && !this.stopped && !this.hidden) { this.pending = false; void this.refresh(); }
    }
  }
  more() { if (this.state.page?.next_cursor) void this.refresh(this.state.page.next_cursor); }
}
