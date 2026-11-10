// The knowledge-base projects to-dos can belong to (#2581, docs/assistant/todos.md "Project"): read once from
// GET /api/v1/kb/projects when the To-dos page first needs them, then shared. A hub without a knowledge base (off, not
// open yet) gives none: groups then show the slug, and the picker offers only what the to-do already has. The knowledge
// base is the current assistant's (its routes name the pick): another assistant made current reads its own, and until
// then the list is empty rather than the last one's.
import { useEffect, useSyncExternalStore } from "react";
import { api } from "./api";
import { chosenManager, useChosenManager } from "./manager-choice";

export interface TodoProject {
  slug: string;
  name: string;
  /** A long-term project: an area of work rather than one feature or launch. */
  long: boolean;
}

interface KBEntry {
  id: string;
  name: string;
  /** "<short | long> · <status>" */
  extra?: string;
}

const NONE: TodoProject[] = [];
// whose they are: the picked assistant, "" the default
let projects: { of: string; list: TodoProject[] } = { of: "", list: NONE };
let loading: { of: string; done: Promise<void> } | undefined;
const listeners = new Set<() => void>();
const pick = () => chosenManager() ?? "";

function set(of: string, list: TodoProject[]) {
  projects = { of, list };
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

const current = () => (projects.of === pick() ? projects.list : NONE);

/** Reads the list again (after a project was made, the first time, or for another assistant made current). */
export function loadTodoProjects(): Promise<void> {
  const of = pick();
  if (loading?.of === of) return loading.done;
  const done = api<{ items?: KBEntry[] }>("/kb/projects")
    .then((r) => set(of, todoProjectsOf(r.items ?? [])))
    .catch(() => undefined)
    .finally(() => {
      if (loading?.done === done) loading = undefined;
    });
  loading = { of, done };
  return done;
}

/** The knowledge base's projects as to-dos use them: the hub does not list merged ones; ignored ones (not a project) are left out. */
export function todoProjectsOf(items: KBEntry[]): TodoProject[] {
  return items
    .filter((it) => !/·\s*ignored$/.test(it.extra ?? ""))
    .map((it) => ({ slug: it.id, name: it.name || it.id, long: (it.extra ?? "").startsWith("long") }));
}

/** The projects, read again whenever a page that uses them opens. */
export function useTodoProjects(): TodoProject[] {
  const picked = useChosenManager();
  const list = useSyncExternalStore(subscribe, current, current);
  useEffect(() => {
    void loadTodoProjects(); // each page that shows them reads them fresh: a project made meanwhile shows up
  }, [picked]);
  return list;
}

/** A project's name by its slug; the slug itself for one not (or not yet) in the list. */
export function projectNames(list: TodoProject[]): (slug: string) => string {
  const names = new Map(list.map((p) => [p.slug, p.name]));
  return (slug) => names.get(slug) ?? slug;
}

/** What the picker offers: long-term projects first, then the rest, each by name; the to-do's own project stays even
 *  when the list does not have it. */
export function projectChoices(list: TodoProject[], current?: string): TodoProject[] {
  const out = [...list].sort((a, b) => Number(b.long) - Number(a.long) || a.name.localeCompare(b.name));
  if (current && !out.some((p) => p.slug === current)) out.unshift({ slug: current, name: current, long: false });
  return out;
}
