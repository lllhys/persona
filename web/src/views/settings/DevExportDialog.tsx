import { useMemo, useState } from "react";
import { t } from "../../i18n";
import { errorText } from "../../lib/api";
import { addFiles } from "../../lib/composer-files";
import { devRecorder, startDevMode, webBuild } from "../../lib/dev-mode";
import { buildPackage, DEV_CATEGORIES, thisDevice, type DevCategory } from "../../lib/dev-package";
import { sendDraft } from "../../lib/drafts";
import { bytes } from "../../lib/format";
import { forwardSessions, PERSONA_TARGET, rememberForward, sessionTarget } from "../../lib/forward-memory";
import { forwardSections, keyOf } from "../../lib/forward-targets";
import { whenComposer } from "../../lib/links";
import { machineWithHub } from "../../lib/mesh";
import { navigate } from "../../lib/router";
import { getState } from "../../lib/store";
import type { Session } from "../../lib/types";
import { Button, cx, Dialog, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { SearchField } from "../../ui/SearchField";
import { conversationTitle } from "../assistant/conversations";
import { FixedRow, heldSessions, Section, TargetRow } from "../chat/ForwardDialog";

type Target = { kind: "persona" } | { kind: "session"; s: Session };

const CATEGORY: Record<DevCategory, { title: string; hint: string }> = {
  logs: { title: "settings-devmode.logs", hint: "settings-devmode.logs_hint" },
  errors: { title: "settings-devmode.errors", hint: "settings-devmode.errors_hint" },
  energy: { title: "settings-devmode.energy", hint: "settings-devmode.energy_hint" },
};

/** Puts the package over the target's box as a picked file (uploading to the computer that chat runs on), the note in
 *  the box (before any words it holds), and opens the chat focused. Nothing is sent until the owner sends it. */
function deliver(target: Target, file: File, note: string) {
  rememberForward(target.kind === "persona" ? PERSONA_TARGET : sessionTarget(target.s.machine_id, target.s.session_id));
  const a = getState().assistant;
  const [key, mid] = target.kind === "persona"
    ? [a?.main ? `${a.machine_id}/${a.main}` : "persona-new", a?.machine_id ?? ""]
    : [keyOf(target.s), target.s.machine_id];
  addFiles(key, mid, [file]);
  sendDraft(key, { type: "unsent", text: note });
  whenComposer(key, (c) => setTimeout(() => c.focus(), 0));
  if (target.kind === "persona") navigate({ name: "persona" });
  else if (target.s.assistant) navigate({ name: "persona", sid: target.s.session_id });
  else navigate({ name: "session", mid: target.s.machine_id, sid: target.s.session_id, view: "chat" });
}

/** Export Diagnostics (#9617, docs/operations/developing-persona.md): what the package holds and which of it to leave
 *  out, then Send to Agent (Forward to's targets; the chat opens with the zip over its box and a note) or Download. */
export function DevExportDialog({ since, size, onClose }: { since: string | null; size: number | null; onClose: () => void }) {
  const [picked, setPicked] = useState<Set<DevCategory>>(() => new Set(DEV_CATEGORIES));
  const [busy, setBusy] = useState<"send" | "download" | "">("");
  const [file, setFile] = useState<File | null>(null);
  const [query, setQuery] = useState("");
  const when = since ? new Date(since).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) : "–";
  const device = useMemo(() => thisDevice(webBuild(), getState().version), []);
  const system = [device.browser, device.os && `${device.os}${device.os_version ? ` ${device.os_version}` : ""}`].filter(Boolean).join(" · ");

  const make = async (way: "send" | "download"): Promise<File | null> => {
    setBusy(way);
    try {
      const r = devRecorder() ?? startDevMode();
      await r.flush();
      return await buildPackage(r.store, picked, since, device);
    } catch (e) {
      toast(t("settings-devmode.failed", { error: errorText(e) }), "error");
      return null;
    } finally {
      setBusy("");
    }
  };
  const download = async () => {
    const f = await make("download");
    if (!f) return;
    const url = URL.createObjectURL(f);
    const a = document.createElement("a");
    a.href = url;
    a.download = f.name;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 60_000);
  };
  const send = async () => {
    const f = await make("send");
    if (f) setFile(f);
  };

  if (file) return <SendTo file={file} since={when} query={query} setQuery={setQuery} onClose={onClose} onBack={() => setFile(null)} />;

  return (
    <Dialog
      open
      onClose={onClose}
      title={t("settings-devmode.export_title")}
      width={460}
      footer={
        <div className="flex flex-wrap justify-end gap-2">
          <Button icon="download" onClick={() => void download()} busy={busy === "download"} disabled={Boolean(busy)} testId="dev-export-download">
            {t("settings-devmode.download")}
          </Button>
          <Button tone="primary" icon="send" onClick={() => void send()} busy={busy === "send"} disabled={Boolean(busy)} testId="dev-export-send">
            {t("settings-devmode.send")}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-3" data-dev-export>
        <p className="text-label text-ink-3" data-dev-export-summary>
          {t("settings-devmode.window", { since: when })} · {t("settings-devmode.details", { size: size === null ? "–" : bytes(size), system })}
        </p>
        <div role="group" aria-label={t("settings-devmode.includes")} className="flex flex-col">
          {DEV_CATEGORIES.map((c) => {
            const on = picked.has(c);
            return (
              <button
                key={c}
                type="button"
                role="checkbox"
                aria-checked={on}
                data-dev-category={c}
                className="flex items-start gap-2.5 rounded-control px-1 py-2 text-left enabled:hover:bg-hover"
                onClick={() => setPicked((p) => {
                  const next = new Set(p);
                  if (on) next.delete(c);
                  else next.add(c);
                  return next;
                })}
              >
                <span aria-hidden className={cx("mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-[4px]", on ? "bg-accent text-white" : "shadow-hairline")}>
                  {on && <Icon name="check" size={11} />}
                </span>
                <span className="min-w-0">
                  <span className="block text-meta text-ink">{t(CATEGORY[c].title)}</span>
                  <span className="block text-submeta text-ink-3">{t(CATEGORY[c].hint)}</span>
                </span>
              </button>
            );
          })}
          <div className="flex items-start gap-2.5 px-1 py-2">
            <span aria-hidden className="mt-0.5 flex size-4 shrink-0 items-center justify-center text-ink-3">
              <Icon name="lock" size={12} />
            </span>
            <span className="min-w-0">
              <span className="block text-meta text-ink">{t("settings-devmode.device")}</span>
              <span className="block text-submeta text-ink-3">{t("settings-devmode.device_hint")}</span>
            </span>
          </div>
        </div>
        <p className="rounded-control bg-field px-3 py-2 text-submeta text-ink-3">{t("settings-devmode.never")}</p>
      </div>
    </Dialog>
  );
}

/** Send To: Persona's main chat, the recent targets and each computer's Agents, with search, as Forward to lists them. */
function SendTo({ file, since, query, setQuery, onClose, onBack }: { file: File; since: string; query: string; setQuery: (q: string) => void; onClose: () => void; onBack: () => void }) {
  const held = useMemo(() => ({ sessions: heldSessions(), machines: getState().machines, assistant: getState().assistant, recents: forwardSessions() }), []);
  const a = held.assistant;
  const main = a?.main ? `${a.machine_id}/${a.main}` : undefined;
  const sections = useMemo(
    () =>
      forwardSections({
        all: held.sessions,
        from: "",
        recents: held.recents,
        main,
        machines: held.machines.map((m) => ({ id: m.id, name: machineWithHub(m.id, held.machines) })),
        query,
        online: (mid) => held.machines.find((m) => m.id === mid)?.online === true,
        conversationTitle: (s) => conversationTitle(s),
      }),
    [held, main, query],
  );
  const pick = (target: Target) => {
    onClose();
    deliver(target, file, t("settings-devmode.note", { since }));
  };
  return (
    <Dialog open onClose={onClose} title={t("settings-devmode.send_to")} width={480}>
      <div className="flex flex-col gap-2.5" data-dev-send>
        <div className="flex items-center gap-2.5 rounded-control bg-field px-2.5 py-2">
          <Icon name="archive" size={14} />
          <span className="min-w-0 flex-1 truncate text-label text-ink">{file.name}</span>
          <span className="text-submeta text-ink-3">{bytes(file.size)}</span>
          <Button tone="quiet" size="sm" onClick={onBack}>
            {t("settings-devmode.back")}
          </Button>
        </div>
        <SearchField value={query} onChange={setQuery} placeholder={t("chat-forwardto.search")} tone="inset" />
        <div className="-mx-1.5 flex flex-col">
          {!query.trim() && <FixedRow icon="spark" title="Persona" detail={t("chat-forwardto.main_chat")} busy={false} disabled={false} onClick={() => pick({ kind: "persona" })} id="persona" />}
          {sections.map((sec) => (
            <Section key={sec.id} section={sec}>
              {sec.sessions.map((s) => (
                <TargetRow key={keyOf(s)} s={s} machine={sec.kind === "machine" || s.assistant ? undefined : machineWithHub(s.machine_id, held.machines)} busy={false} disabled={false} onClick={() => pick({ kind: "session", s })} />
              ))}
            </Section>
          ))}
          {query.trim() && !sections.length && <p className="px-2.5 py-6 text-center text-label text-ink-3">{t("chat-forwardto.no_match")}</p>}
        </div>
      </div>
    </Dialog>
  );
}
