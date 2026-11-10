import { describe, expect, it } from "vitest";
import fixture from "../../../spec/fixtures/entries/cases.json";
import { assistantEntries } from "./assistant-entries";
import type { FeedItem, TaskPlan } from "./feed";
import type { Todo } from "./todos";
import type { Interaction, Session } from "./types";

// Android reads the same fixture (AssistantEntriesFixtureTest): the two entries split the same input the same way.
describe("the Assistant's Needs you and In progress entries (spec/fixtures/entries)", () => {
  for (const c of fixture.cases) {
    it(c.name, () => {
      const input = c.input as unknown as { conversation: string; main: string; now: string; todos: Todo[]; sessions: Session[]; interactions: Interaction[]; plans: Record<string, TaskPlan>; feed: FeedItem[] };
      const got = assistantEntries({ ...input, now: Date.parse(input.now) });
      expect(got.needsYou.map((item) => item.key)).toEqual(c.expected.needs_you);
      expect(got.running.map((item) => item.key)).toEqual(c.expected.running);
      expect(got.shelved.map((item) => item.key)).toEqual(c.expected.shelved);
      const activity = (c.expected as { activity?: Record<string, unknown> }).activity;
      for (const [key, expected] of Object.entries(activity ?? {})) {
        const work = got.running.find((row) => row.key === key)?.activity;
        expect(work ? { tasks: work.tasks, subagents: work.subagents, workflows: work.workflows,
          ...(work.workflow?.phase ? { phase: work.workflow.phase, progress: `${work.workflow.agents_done}/${work.workflow.agents_started}+` } : {}) } : null).toEqual(expected);
      }
      // one entry each: nothing is in both, and no agent of a listed task shows on its own
      const keys = [...got.needsYou, ...got.running, ...got.shelved].map((item) => item.key);
      expect(new Set(keys).size).toBe(keys.length);
      const taskAgents = new Set(input.todos.filter((todo) => !todo.done_at && keys.includes(`task:${todo.id}`))
        .flatMap((todo) => (todo.agents ?? []).filter((a) => !a.released_at).map((a) => `agent:${a.machine_id}/${a.session_id}`)));
      expect(keys.filter((key) => taskAgents.has(key))).toEqual([]);
    });
  }
});
