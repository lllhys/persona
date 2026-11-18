// Settings › Assistant › Skills: the Persona-provided skills on the assistant's computer, plus a bounded, explicitly
// confirmed ZIP import. The dialog borrows the shapes the rest of the app already uses: LookUpload's drop zone,
// ProviderImport's preview card and footer, ErrorNotice for what went wrong.
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { t } from "../../i18n";
import { credentialOwner, errorText, subscribeCredentialOwner } from "../../lib/api";
import { bytes } from "../../lib/format";
import type { Skill } from "../../lib/hub-update";
import { useShownManager } from "../../lib/manager-choice";
import { importSkill, readSkillZIP, skillImportError, skillImportList, type SkillImportResult } from "../../lib/skill-import";
import { Button, cx, Dialog, Loading } from "../../ui/atoms";
import { ErrorNotice } from "../../ui/ErrorNotice";
import { Icon } from "../../ui/icons";
import { Group, Pill, Row, RowFailed, SettingsHint } from "./SettingsParts";

type Phase = "select" | "checking" | "preview" | "installing" | "success" | "undoing" | "undone";

type ImportView = {
  phase: Phase;
  archive?: string;
  /** The chosen file's own name, shown while it is checked. */
  file?: string;
  result?: SkillImportResult;
  error?: string;
};

export function SkillsGroup({ manager }: { manager?: string } = {}) {
  const shown = useShownManager();
  const owner = useSyncExternalStore(subscribeCredentialOwner, credentialOwner);
  return <SkillsForManager key={JSON.stringify([manager ?? shown, owner])} manager={manager ?? shown} owner={owner} />;
}

function SkillsForManager({ manager, owner }: { manager: string; owner: string }) {
  const [list, setList] = useState<Skill[] | null>(null);
  const [machine, setMachine] = useState<string>();
  const [error, setError] = useState("");
  const [view, setView] = useState<ImportView | null>(null);
  const [lastInstall, setLastInstall] = useState<SkillImportResult>();
  const active = useRef(true);
  const version = useRef(0);
  const request = useRef<AbortController | null>(null);
  const reading = useRef<AbortController | null>(null);
  const admitted = useRef(false);
  const uploadButton = useRef<HTMLButtonElement>(null);
  const cancelButton = useRef<HTMLButtonElement>(null);
  const current = () => active.current && credentialOwner() === owner;

  async function refresh() {
    reading.current?.abort();
    const controller = new AbortController();
    reading.current = controller;
    try {
      const r = await skillImportList(manager, controller.signal, current);
      if (!current() || controller.signal.aborted) return;
      setList(r.skills ?? []); setMachine(r.machine || r.machine_id); setError("");
    } catch (e) {
      if (!current() || controller.signal.aborted) return;
      setError(errorText(e)); setList([]);
    }
  }
  useEffect(() => {
    active.current = true;
    void refresh();
    return () => { active.current = false; version.current++; request.current?.abort(); reading.current?.abort(); };
  }, [manager, owner]);
  useEffect(() => {
    // Cancel, not Install, takes the focus: Enter must not install what the user has only just seen.
    if (view?.phase === "preview") cancelButton.current?.focus();
  }, [view?.phase]);

  function close() {
    if (admitted.current) return;
    const result = view?.phase === "preview" ? view.result : undefined;
    version.current++;
    request.current?.abort();
    setView(null);
    if (result && current()) {
      void importSkill(manager, { op: "cancel", reference: result.reference, environment: result.environment }, undefined, current).catch(() => {});
    }
    uploadButton.current?.focus();
  }
  async function check(file?: File) {
    if (!file || admitted.current) return;
    const n = ++version.current;
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setView({ phase: "checking", file: file.name });
    try {
      const archive = await readSkillZIP(file, controller.signal);
      const result = await importSkill(manager, { op: "preview", archive }, controller.signal, current);
      if (current() && n === version.current) setView({ phase: "preview", archive, result });
    } catch (e) {
      if (current() && n === version.current && !controller.signal.aborted) setView({ phase: "select", error: skillImportError(e) });
    }
  }
  async function write(op: "install" | "undo", result: SkillImportResult, archive?: string) {
    if (admitted.current) return;
    admitted.current = true;
    const n = ++version.current;
    const controller = new AbortController();
    request.current = controller;
    setView({ phase: op === "install" ? "installing" : "undoing", result });
    try {
      const out = await importSkill(manager, { op, archive, reference: result.reference, environment: result.environment }, controller.signal, current);
      if (!current() || n !== version.current) return;
      if (op === "install") setLastInstall(out);
      else setLastInstall(undefined);
      setView({ phase: op === "install" ? "success" : "undone", result: out });
    } catch (e) {
      if (current() && n === version.current && !controller.signal.aborted) {
        setView({ phase: "select", error: skillImportError(e) });
      }
    } finally {
      admitted.current = false;
      // A lost write response is not evidence that nothing happened. Reconcile through the existing list.
      if (current()) void refresh();
    }
  }

  const phase = view?.phase;
  const result = view?.result;
  const committing = phase === "installing" || phase === "undoing";
  const done = phase === "success" || phase === "undone";

  return (
    <>
      <Group
        anchor="skills"
        title={
          <span className="flex items-center gap-2">
            {t("settings-skills.title")}
            {list && list.length > 0 && <span className="font-normal tabular-nums">{t("settings-skills.count", { count: list.length })}</span>}
          </span>
        }
        right={
          <Button ref={uploadButton} tone="quiet" icon="upload" className="-mb-1 touch-hit" onClick={() => setView({ phase: "select" })}>
            {t("settings-skills.upload")}
          </Button>
        }
        footer={machine ? t("settings-skills.footer_where", { machine }) : t("settings-skills.footer_shared")}
      >
        {list === null ? (
          <div className="px-4 py-4"><Loading /></div>
        ) : error ? (
          <RowFailed text={t("settings-skills.load_failed", { error })} retry={t("settings-skills.retry")} onRetry={() => void refresh()} />
        ) : list.length === 0 ? (
          <SettingsHint>{t("settings-skills.empty")}</SettingsHint>
        ) : (
          list.map((s) => (
            <Row key={s.name} label={s.name} hint={s.description} wrapHint>
              <Pill tone={s.source === "builtin" ? undefined : "accent"}>{t(s.source === "builtin" ? "settings-skills.source_builtin" : s.source === "plugin" ? "settings-skills.source_plugin" : "settings-skills.source_mine")}</Pill>
            </Row>
          ))
        )}
        {lastInstall && !view && (
          <Row icon="check" label={t("settings-skills.installed", { name: lastInstall.name })} hint={t("settings-skills.undo_hint")} wrapHint stackOnNarrow>
            <Button className="touch-hit" icon="undo" onClick={() => void write("undo", lastInstall)}>{t("settings-skills.undo")}</Button>
          </Row>
        )}
      </Group>

      <Dialog
        open={view !== null}
        onClose={close}
        title={t("settings-skills.dialog_title")}
        width={480}
        footer={
          <div className="flex flex-1 items-center gap-2">
            {phase === "success" || phase === "undoing" ? (
              <Button className="touch-hit" tone="quiet" icon="undo" busy={phase === "undoing"} onClick={() => result && void write("undo", result)}>{t("settings-skills.undo")}</Button>
            ) : !done && (
              <Button ref={cancelButton} className="touch-hit" tone="quiet" disabled={committing} onClick={close}>{t("common.cancel")}</Button>
            )}
            <span className="flex-1" />
            {(phase === "preview" || phase === "installing") && result && (
              <Button className="touch-hit" tone="primary" busy={phase === "installing"} onClick={() => void write("install", result, view?.archive)}>{t("settings-skills.install")}</Button>
            )}
            {(done || phase === "undoing") && (
              <Button className="touch-hit" tone="primary" disabled={committing} onClick={close}>{t("settings-skills.close")}</Button>
            )}
            {committing && <span role="status" className="sr-only">{t(phase === "installing" ? "settings-skills.installing" : "settings-skills.undoing")}</span>}
          </div>
        }
      >
        {view && (
          <div className="flex flex-col gap-3" aria-busy={phase === "checking" || committing || undefined}>
            {(phase === "select" || phase === "checking") && <PickZip view={view} onPick={(f) => void check(f)} />}
            {(phase === "preview" || phase === "installing") && result && <Preview result={result} />}
            {(phase === "success" || phase === "undoing") && result && (
              <Outcome icon="check" tone="green" title={t("settings-skills.installed", { name: result.name })} text={t("settings-skills.next_chat")} note={t("settings-skills.undo_hint")} />
            )}
            {phase === "undone" && (
              <>
                <Outcome icon="undo" title={t("settings-skills.undone_title")} text={t("settings-skills.undone")} />
                {result?.recovery && <p className="rounded-control bg-inset px-2.5 py-2 font-mono text-submeta leading-relaxed break-all text-ink-2 shadow-hairline">{result.recovery}</p>}
              </>
            )}
          </div>
        )}
      </Dialog>
    </>
  );
}

/** Choosing the ZIP: LookUpload's drop zone, the error the last try ended on above it, and the check's progress in its place. */
function PickZip({ view, onPick }: { view: ImportView; onPick: (file?: File) => void }) {
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const checking = view.phase === "checking";
  const drop = {
    onDragOver: (e: React.DragEvent) => {
      e.preventDefault();
      if (!checking && [...e.dataTransfer.types].includes("Files")) setOver(true);
    },
    onDragLeave: () => setOver(false),
    onDrop: (e: React.DragEvent) => {
      e.preventDefault();
      setOver(false);
      if (checking || !e.dataTransfer.files.length) return;
      onPick(e.dataTransfer.files[0]);
    },
  };
  return (
    <>
      {view.error && <ErrorNotice role="alert" message={view.error} />}
      <div
        {...drop}
        data-testid="skill-zip-drop"
        className={cx(
          "flex min-h-52 flex-col items-center justify-center gap-3 rounded-card border border-dashed px-6 py-6 text-center transition-colors duration-150",
          over ? "border-accent bg-accent-tint" : "border-line-strong bg-inset",
        )}
      >
        {checking ? (
          <>
            <Loading label={t("settings-skills.checking")} />
            {view.file && <p className="max-w-full truncate text-submeta text-ink-3">{view.file}</p>}
          </>
        ) : (
          <>
            <span className="flex size-14 items-center justify-center rounded-full bg-field text-ink-3">
              <Icon name="archive" size={24} />
            </span>
            <Button className="touch-hit" tone="secondary" icon="upload" onClick={() => input.current?.click()}>{t("settings-skills.file_label")}</Button>
            <p className="max-w-72 text-submeta leading-relaxed text-ink-3">{t("settings-skills.drop_hint")}</p>
          </>
        )}
      </div>
      <input
        ref={input}
        type="file"
        accept=".zip,application/zip"
        className="hidden"
        aria-label={t("settings-skills.file_label")}
        onChange={(e) => { onPick(e.currentTarget.files?.[0]); e.currentTarget.value = ""; }}
      />
    </>
  );
}

/** What the package would install, before anything is: the skill as its row will read, then where it goes. Its name
 *  and description come from the ZIP, so they are only ever text. */
function Preview({ result }: { result: SkillImportResult }) {
  return (
    <>
      <p role="status" className="flex items-center gap-1.5 text-label font-medium text-green">
        <Icon name="check" size={13} stroke={2.4} className="shrink-0" />
        {t("settings-skills.valid")}
      </p>
      <div className="overflow-hidden rounded-card bg-surface shadow-btn">
        <div className="flex items-start gap-3 px-3 py-3">
          <span className="flex size-7 shrink-0 items-center justify-center rounded-control bg-inset text-ink-2 shadow-hairline">
            <Icon name="spark" size={14} />
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-meta font-medium break-words text-ink">{result.name}</p>
            <p className="mt-0.5 text-submeta leading-relaxed break-words whitespace-pre-wrap text-ink-3">{result.description}</p>
          </div>
        </div>
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5 border-t border-line px-3 py-2.5 text-submeta leading-relaxed">
          <dt className="text-ink-3">{t("settings-skills.computer_label")}</dt>
          <dd className="break-words text-ink-2">{result.machine}</dd>
          <dt className="text-ink-3">{t("settings-skills.destination")}</dt>
          <dd className="font-mono break-all text-ink-2">{result.destination}</dd>
          <dt className="text-ink-3">{t("settings-skills.contents_label")}</dt>
          <dd className="text-ink-2 tabular-nums">{t("settings-skills.package_size", { count: result.files, bytes: bytes(result.bytes) })}</dd>
        </dl>
      </div>
      <div className="flex items-start gap-2 px-1 text-submeta leading-relaxed text-ink-3">
        <Icon name="shield" size={13} className="mt-0.5 shrink-0" />
        <div className="flex flex-col gap-1">
          <p>{t("settings-skills.trust")}</p>
          <p>{t("settings-skills.shared")}</p>
        </div>
      </div>
    </>
  );
}

/** How an install or an undo ended: a mark, what happened, what to do next. */
function Outcome({ icon, tone, title, text, note }: { icon: "check" | "undo"; tone?: "green"; title: string; text: string; note?: string }) {
  return (
    <div role="status" className="flex flex-col items-center gap-2 px-2 pt-3 pb-1 text-center">
      <span className={cx("flex size-12 items-center justify-center rounded-full", tone === "green" ? "bg-green-tint text-green" : "bg-field text-ink-3")}>
        <Icon name={icon} size={22} stroke={tone === "green" ? 2.4 : 1.9} />
      </span>
      <p className="mt-1 text-meta font-semibold break-words text-ink">{title}</p>
      <p className="max-w-80 text-submeta leading-relaxed text-ink-2">{text}</p>
      {note && <p className="max-w-80 text-submeta leading-relaxed text-ink-3">{note}</p>}
    </div>
  );
}
