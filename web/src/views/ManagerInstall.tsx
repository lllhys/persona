// Making an assistant and installing it (docs/assistant/managers.md "Install codes"): the dialog that asks for its name, and the
// one-time install code with its countdown (and the install commands when the hub gives them). Settings › Assistants
// and the sidebar's switcher both open them, so they live outside the settings chunk.
import { useEffect, useState } from "react";
import { t } from "../i18n";
import { clock, secondsLeft } from "../lib/connect-device";
import { createManager, managerError, NAME_MAX, renewCode, validName, type InstallCode, type Manager } from "../lib/managers";
import { Button, copyText, cx, Dialog, IconButton, toast, useNow } from "../ui/atoms";
import { inputClass } from "./assistant/parts";

/** A code on screen: the manager it installs, and whether it replaces one already installed somewhere. */
export interface ShownCode {
  id: string;
  name: string;
  code: InstallCode;
  /** Never installed: once its code runs out the hub may have dropped it, and a new code makes it again (renewCode). */
  fresh?: boolean;
}

export function NewManager({ open, onClose, onMade }: { open: boolean; onClose: () => void; onMade: (m: Manager, code: InstallCode) => void }) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!open) return;
    setName("");
    setError("");
  }, [open]);
  const ok = validName(name);
  const make = async () => {
    if (!ok || busy) return;
    setBusy(true);
    setError("");
    try {
      const r = await createManager(name.trim());
      onMade(r.manager, r);
    } catch (e) {
      setError(managerError(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      width={400}
      title={t("settings-managers.new")}
      footer={
        <>
          <Button tone="quiet" disabled={busy} onClick={onClose}>{t("common.cancel")}</Button>
          <Button tone="primary" busy={busy} disabled={!ok} onClick={() => void make()}>{t("settings-managers.create")}</Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void make();
        }}
      >
        <input
          autoFocus
          value={name}
          maxLength={NAME_MAX * 2}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("settings-managers.name_placeholder")}
          aria-label={t("settings-managers.name")}
          className={inputClass}
        />
      </form>
      <p className="mt-2 text-[11.5px] leading-relaxed text-ink-3">{t("settings-managers.new_hint")}</p>
      {error && <p role="alert" className="mt-2 text-[12.5px] text-red">{error}</p>}
    </Dialog>
  );
}

/** The install code (and commands), its countdown, and once it ran out New installation code. */
export function CodeDialog({ shown, onClose, onCode }: { shown: ShownCode | null; onClose: () => void; onCode: (c: InstallCode, id: string) => void }) {
  const now = useNow(1000);
  const [busy, setBusy] = useState(false);
  const [last, setLast] = useState<ShownCode | null>(null);
  useEffect(() => void (shown && setLast(shown)), [shown]);
  const s = shown ?? last;
  if (!s) return null;
  const left = secondsLeft(s.code.expires_at, now);
  const again = async () => {
    setBusy(true);
    try {
      const r = await renewCode(s.id, s.name, !!s.fresh);
      onCode(r.code, r.id);
    } catch (e) {
      toast(managerError(e), "error");
    } finally {
      setBusy(false);
    }
  };
  const commands = [
    { label: t("settings-managers.install_service"), text: s.code.commands?.service },
    { label: t("settings-managers.install_docker"), text: s.code.commands?.docker },
  ].filter((c): c is { label: string; text: string } => !!c.text);
  return (
    <Dialog open={!!shown} onClose={onClose} width={480} title={t("settings-managers.code_for", { name: s.name })} footer={<Button tone="primary" onClick={onClose}>{t("settings-managers.done")}</Button>}>
      {left === 0 ? (
        <div className="flex flex-col items-center gap-3 py-6 text-center">
          <p className="text-[13px] text-ink-2">{t("settings-managers.expired")}</p>
          <Button tone="primary" busy={busy} onClick={() => void again()}>{t("settings-managers.new_code")}</Button>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-1 flex items-center gap-2 text-[12px] text-ink-3">
              <span className="flex-1">{t("settings-managers.code_title")}</span>
              <span className="tabular-nums">{t("settings-managers.expires_in", { time: clock(left) })}</span>
            </div>
            <CopyLine text={s.code.code} big />
          </div>
          {commands.map((c) => (
            <div key={c.label}>
              <div className="mb-1 text-[12px] text-ink-3">{c.label}</div>
              <CopyLine text={c.text} />
            </div>
          ))}
          <p className="text-[12px] leading-relaxed text-ink-3">{t(commands.length ? "settings-managers.commands_note" : "settings-managers.code_note")}</p>
        </div>
      )}
    </Dialog>
  );
}

function CopyLine({ text, big }: { text: string; big?: boolean }) {
  return (
    <div className="flex items-center gap-2 rounded-control bg-inset px-2.5 py-1.5 shadow-hairline">
      {/* a command wraps so all of it can be read before it is pasted; the code is one short line */}
      <code className={cx("min-w-0 flex-1 font-mono text-ink select-all", big ? "truncate text-[14px]" : "break-all text-[12px] leading-relaxed")} title={text}>{text}</code>
      <IconButton
        icon="copy"
        label={t("settings-managers.copy")}
        size={24}
        iconSize={12}
        onClick={() => void copyText(text)}
      />
    </div>
  );
}
