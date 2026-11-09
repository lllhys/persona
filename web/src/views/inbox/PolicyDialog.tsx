// Inbox policies (docs/assistant/inbox.md "Policies", #5764): "Rule for this chat / sender / repository…" from an item's menu opens
// PolicyDialog (the tier its items get, what the AI twin does with them); "Rules" in the Inbox's ⋯ menu lists them
// (RulesDialog) with Edit and Remove. The hub decides in code; this only sets and lists.
import { useEffect, useState, useSyncExternalStore } from "react";
import {
  listInboxPolicies, muteLine, muteShortName, policySummary, policyTierName, policyTwinName, removeInboxPolicy, saveInboxPolicy,
  type InboxItem, type InboxMuteOption, type InboxPolicy, type PolicyTier, type PolicyTwin,
} from "../../lib/inbox";
import { errorText } from "../../lib/api";
import { otherManager } from "../../lib/manager-choice";
import { managerOrder, multiManager, useRoster, type Manager } from "../../lib/managers";
import { t } from "../../i18n";
import { Button, Dialog, Loading, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Segmented } from "../assistant/parts";
import { ManagerHeading } from "../ManagerParts";

/** What the dialog is about: a new rule from an item's option, or an existing one. Each manager keeps its own rules:
 *  the rule is the item's (or the rule's) manager's. */
export type PolicyTarget = { item?: InboxItem; option: InboxMuteOption; source: string; policy?: InboxPolicy };

/** Whose rules a target's are: its rule's, else its item's manager (none: the default's). */
const ownerOf = (target: PolicyTarget) => target.policy?.manager ?? target.item?.manager;

let current: PolicyTarget | null = null;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

/** Open the rule dialog (from a row's menu or an item's page); the Inbox page's PolicyDialogHost draws it. */
export function openPolicyDialog(target: PolicyTarget) {
  current = target;
  emit();
}

function closePolicyDialog() {
  current = null;
  emit();
}

function usePolicyTarget(): PolicyTarget | null {
  return useSyncExternalStore((l) => (listeners.add(l), () => void listeners.delete(l)), () => current);
}

/** Mounted once on the Inbox page: the rule dialog when one is open. */
export function PolicyDialogHost() {
  const target = usePolicyTarget();
  return target ? <PolicyDialog key={`${ownerOf(target) ?? ""}:${target.source}:${target.option.tag}:${target.option.value}`} target={target} onClose={closePolicyDialog} /> : null;
}

const TIERS: PolicyTier[] = ["", "action", "fyi", "noise"];
const TWINS: PolicyTwin[] = ["", "first", "never"];

/** The rule of one chat / sender / repository: its tier and what the AI twin does; prefilled from the rule it has. */
export function PolicyDialog({ target, onClose }: { target: PolicyTarget; onClose: () => void }) {
  const [policy, setPolicy] = useState<InboxPolicy | undefined>(target.policy);
  const [loaded, setLoaded] = useState(Boolean(target.policy));
  const [tier, setTier] = useState<PolicyTier>(target.policy?.tier ?? "");
  const [twin, setTwin] = useState<PolicyTwin>(target.policy?.twin ?? "");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (target.policy) return;
    // the item's manager's rules: a rule of the same chat under another manager is not this one
    listInboxPolicies(otherManager(ownerOf(target))).then(
      (list) => {
        const same = list.find((p) => p.source === target.source && p.tag === target.option.tag && p.value === target.option.value);
        setPolicy(same);
        setTier(same?.tier ?? "");
        setTwin(same?.twin ?? "");
        setLoaded(true);
      },
      () => setLoaded(true), // no list (an older hub): start from nothing
    );
  }, [target]);
  const save = async (nextTier: PolicyTier, nextTwin: PolicyTwin) => {
    setBusy(true);
    // removing a rule opened from the Rules list names the manager the list read it from, even when that is the default now
    const stamped = !target.item && !nextTier && !nextTwin;
    const ok = await saveInboxPolicy({ item: target.item, option: target.option, source: target.source, id: policy?.id, manager: ownerOf(target), stamped }, nextTier, nextTwin);
    setBusy(false);
    if (ok !== null) onClose();
  };
  return (
    <Dialog open onClose={onClose} title={t("inbox-policydialog.title", { name: muteShortName(target.option) })} width={440}
      footer={
        <>
          {policy && <Button tone="quiet" disabled={busy} onClick={() => void save("", "")}>{t("inbox-policydialog.remove")}</Button>}
          <span className="flex-1" />
          <Button tone="secondary" onClick={onClose}>{t("inbox-policydialog.cancel")}</Button>
          <Button tone="primary" busy={busy} disabled={!loaded || (!policy && !tier && !twin)} onClick={() => void save(tier, twin)}>{t("inbox-policydialog.save")}</Button>
        </>
      }>
      {!loaded ? (
        <div className="py-2"><Loading /></div>
      ) : (
        <div className="flex flex-col gap-4">
          <p className="text-[12.5px] leading-relaxed text-ink-3">{t("inbox-policydialog.intro", { what: muteLine({ ...target.option, id: "", source: target.source, source_name: target.item?.account_name || policy?.source_name }) })}</p>
          <div className="flex flex-col gap-1.5">
            <span className="text-[12px] font-medium text-ink-2">{t("inbox-policydialog.tier")}</span>
            <Segmented label={t("inbox-policydialog.tier")} value={tier} onChange={setTier} options={TIERS.map((v) => ({ v, label: policyTierName(v) }))} compact />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-[12px] font-medium text-ink-2">{t("inbox-policydialog.twin")}</span>
            <Segmented label={t("inbox-policydialog.twin")} value={twin} onChange={setTwin} options={TWINS.map((v) => ({ v, label: policyTwinName(v) }))} compact />
            <p className="text-[11.5px] leading-relaxed text-ink-3">{t("inbox-policydialog.twin_note")}</p>
          </div>
        </div>
      )}
    </Dialog>
  );
}

/** One manager's rules in the "Rules" dialog (several managers: each keeps its own, read with its name). */
type RuleGroup = { manager: Manager; rules: InboxPolicy[] | null; error: string };

async function rulesByManager(managers: Manager[]): Promise<RuleGroup[]> {
  const read = await Promise.allSettled(managers.map((m) => listInboxPolicies(m.id)));
  return managers.map((m, i) => {
    const r = read[i];
    return r.status === "fulfilled" ? { manager: m, rules: r.value, error: "" } : { manager: m, rules: null, error: errorText(r.reason) };
  });
}

/** "Rules" (the Inbox's ⋯ menu): every source's rules, newest first, each with Edit and Remove. With several managers,
 *  each one's under its name. */
export function RulesDialog({ onClose }: { onClose: () => void }) {
  const roster = useRoster();
  const managers = multiManager(roster) ? managerOrder(roster) : null;
  const several = managers?.map((m) => m.id).join(",") ?? "";
  const [list, setList] = useState<InboxPolicy[] | null>(null);
  const [groups, setGroups] = useState<RuleGroup[] | null>(null);
  const [failed, setFailed] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const editing = usePolicyTarget();
  const load = () => {
    setFailed("");
    if (managers) void rulesByManager(managers).then(setGroups);
    else listInboxPolicies().then(setList, (e) => setFailed(errorText(e)));
  };
  useEffect(load, [editing, several]); // again after an edit closes
  const remove = async (p: InboxPolicy) => {
    const key = `${p.manager ?? ""}:${p.id}`;
    setBusy(key);
    try {
      await removeInboxPolicy(p);
      setList((l) => (l ?? []).filter((x) => x.id !== p.id));
      setGroups((gs) => gs?.map((g) => (g.manager.id === p.manager ? { ...g, rules: g.rules?.filter((x) => x.id !== p.id) ?? null } : g)) ?? gs);
      toast(t("inbox-policydialog.removed", { name: p.name }), "ok");
    } catch (e) {
      toast(t("inbox-policydialog.remove_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(null);
    }
  };
  if (editing) return null; // the rule's own dialog is open over the list's place
  const row = (p: InboxPolicy) => {
    const key = `${p.manager ?? ""}:${p.id}`;
    return (
      <li key={key} data-policy={p.id} className="flex items-center gap-3 rounded-control px-1 py-2 hover:bg-hover">
        <Icon name="target" size={14} className="shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] text-ink" title={p.name}>{p.name}</span>
          <span className="mt-0.5 block truncate text-[11.5px] text-ink-3">{[muteLine(p), policySummary(p)].filter(Boolean).join(" · ")}</span>
        </span>
        <Button tone="quiet" disabled={busy !== null} onClick={() => openPolicyDialog({ option: p, source: p.source, policy: p })}>{t("inbox-policydialog.edit")}</Button>
        <Button tone="quiet" busy={busy === key} disabled={busy !== null && busy !== key} onClick={() => void remove(p)}>{t("inbox-policydialog.remove")}</Button>
      </li>
    );
  };
  const shownGroups = groups?.filter((g) => g.error || g.rules?.length) ?? null;
  const empty = managers ? shownGroups?.length === 0 : list?.length === 0;
  return (
    <Dialog open onClose={onClose} title={t("inbox-policydialog.rules")} width={440} footer={<Button tone="primary" onClick={onClose}>{t("inbox-policydialog.close")}</Button>}>
      {failed ? (
        <div className="flex flex-col items-start gap-2">
          <p className="text-[12.5px] break-words text-red">{t("inbox-policydialog.load_failed", { error: failed })}</p>
          <Button tone="secondary" icon="retry" onClick={load}>{t("inbox-policydialog.retry")}</Button>
        </div>
      ) : (managers ? !groups : !list) ? (
        <div className="py-2"><Loading /></div>
      ) : empty ? (
        <div className="flex flex-col items-center gap-2 py-4 text-center">
          <span className="flex size-10 items-center justify-center rounded-full bg-field text-ink-3">
            <Icon name="target" size={17} />
          </span>
          <p className="text-[13.5px] font-medium text-ink">{t("inbox-policydialog.empty_title")}</p>
          <p className="max-w-[300px] text-[12.5px] leading-relaxed text-ink-3">{t("inbox-policydialog.empty_hint")}</p>
        </div>
      ) : shownGroups ? (
        shownGroups.map((g) => (
          <section key={g.manager.id} data-rules-manager={g.manager.id} className="mb-2">
            <ManagerHeading manager={g.manager} className="px-1 pt-1 pb-0.5" />
            {g.error ? (
              <p className="px-1 py-1.5 text-[12px] break-words text-ink-3">{g.manager.online ? t("inbox-policydialog.load_failed", { error: g.error }) : t("inbox-policydialog.rules_offline")}</p>
            ) : (
              <ul className="-mx-1 flex flex-col">{g.rules!.map(row)}</ul>
            )}
          </section>
        ))
      ) : (
        <ul className="-mx-1 flex flex-col">{list!.map(row)}</ul>
      )}
    </Dialog>
  );
}
