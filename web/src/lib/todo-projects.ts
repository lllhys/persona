// The knowledge-base projects to-dos can belong to (#2581, docs/assistant/todos.md "Project"): read once from
// GET /api/v1/kb/projects when the To-dos page first needs them, then shared. A hub without a knowledge base (off, not
// open yet) gives none: groups then show the slug, and the picker offers only what the to-do already has.
import { useEffect, useSyncExternalStore } from "react";
import { api } from "./api";

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

let projects: TodoProject[] = [];
let loading: Promise<void> | undefined;
const listeners = new Set<() => void>();

function set(next: TodoProject[]) {
  projects = next;
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

const current = () => projects;

/** Reads the list again (after a project was made, or the first time). */
export function loadTodoProjects(): Promise<void> {
  loading ??= api<{ items?: KBEntry[] }>("/kb/projects")
    .then((r) => set(todoProjectsOf(r.items ?? [])))
    .catch(() => undefined)
    .finally(() => {
      loading = undefined;
    });
  return loading;
}

/** The knowledge base's projects as to-dos use them: the hub does not list merged ones; ignored ones (not a project) are left out. */
export function todoProjectsOf(items: KBEntry[]): TodoProject[] {
  return items
    .filter((it) => !/·\s*ignored$/.test(it.extra ?? ""))
    .map((it) => ({ slug: it.id, name: it.name || it.id, long: (it.extra ?? "").startsWith("long") }));
}

/** The projects, read again whenever a page that uses them opens. */
export function useTodoProjects(): TodoProject[] {
  const list = useSyncExternalStore(subscribe, current, current);
  useEffect(() => {
    void loadTodoProjects(); // each page that shows them reads them fresh: a project made meanwhile shows up
  }, []);
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

/** For tests: forget the list. */
export function resetTodoProjects() {
  set([]);
}
