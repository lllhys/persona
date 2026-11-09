// The assistant's example cards (schema/extension/examples/assistant/, the synthetic data every app's tests draw), read
// for the "Assistant cards" page's preview. Nothing here talks to a hub: a press only says what the card would have sent.
import { t } from "../i18n";
import { readView, submitValues, type UIView } from "../lib/extension-ui";

const FILES = import.meta.glob("../../../schema/extension/examples/assistant/*.json", { eager: true, import: "default" }) as Record<string, unknown>;

/** The examples, in the order the page shows them; progress has its card before and after an update. */
export const EXAMPLE_ORDER = ["decision", "compare", "progress", "form", "chart", "html"] as const;
export type ExampleId = (typeof EXAMPLE_ORDER)[number];

export interface Example {
  id: string;
  raw: Record<string, unknown>;
  title: string;
  fallback: string;
  /** null: drawn as its fallback only (an html card: the preview never runs a model's page). */
  view: UIView | null;
  html: boolean;
}

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

/** Every component of a raw view (its sections' items and its named pages'), depth first. */
function components(raw: Record<string, unknown>): Record<string, unknown>[] {
  const out: Record<string, unknown>[] = [];
  const walk = (sections: unknown) => {
    if (!Array.isArray(sections)) return;
    for (const s of sections) if (isObject(s) && Array.isArray(s.items)) for (const it of s.items) if (isObject(it)) out.push(it);
  };
  walk(raw.sections);
  if (isObject(raw.pages)) for (const p of Object.values(raw.pages)) if (isObject(p)) walk(p.sections);
  return out;
}

export function readExample(id: string, raw: unknown): Example | null {
  if (!isObject(raw)) return null;
  const html = components(raw).some((c) => c.type === "html");
  let view: UIView | null = null;
  if (!html) {
    try {
      view = readView(raw);
    } catch {
      view = null;
    }
  }
  return { id, raw, title: typeof raw.title === "string" ? raw.title : id, fallback: typeof raw.fallback === "string" ? raw.fallback : "", view, html };
}

export function example(name: string): Example | null {
  const key = Object.keys(FILES).find((k) => k.endsWith(`/${name}.json`));
  return key ? readExample(name, FILES[key]) : null;
}

/** Find the saved action rather than treating its opaque id as reply text. */
function actionOf(raw: Record<string, unknown>, actionId: string): Record<string, unknown> | null {
  for (const c of components(raw)) {
    for (const a of [c.action, isObject(c.button) ? c.button.action : undefined]) {
      if (isObject(a) && a.id === actionId) return a;
    }
  }
  return null;
}

export function previewAction(raw: Record<string, unknown>, actionId: string): { sends: boolean; text: string } | null {
  const a = actionOf(raw, actionId);
  if (!a) return null;
  if (typeof a.reply === "string") return { sends: true, text: a.reply };
  if (isObject(a.push) && typeof a.push.page === "string") {
    const page = isObject(raw.pages) ? raw.pages[a.push.page] : null;
    const title = isObject(page) && typeof page.title === "string" ? page.title : a.push.page;
    return { sends: false, text: t("admin-assistant-examples.would_push", { title }) };
  }
  if (a.pop === true) return { sends: false, text: t("admin-assistant-examples.would_pop") };
  if (typeof a.open === "string") return { sends: false, text: t("admin-assistant-examples.would_open", { target: a.open }) };
  return null;
}

/** A submitted form as people read it: "label: value" per field (full-width colon), a choice by its title, a toggle as "Yes" / "No". */
export function submitSummary(view: UIView, values: Record<string, string>): string[] {
  const sent = submitValues(view, values);
  const out: string[] = [];
  for (const s of view.sections) {
    for (const it of s.items) {
      if ((it.type !== "field" && it.type !== "segmented") || !it.name || !(it.name in sent)) continue;
      const v = sent[it.name];
      const shown =
        it.kind === "toggle" ? (v === "true" ? t("admin-assistant-examples.yes") : t("admin-assistant-examples.no")) : (it.choices?.find((c) => c.value === v)?.title ?? v);
      out.push(t("admin-assistant-examples.field_line", { label: it.label || it.name, value: shown || t("admin-assistant-examples.empty") }));
    }
  }
  return out;
}
