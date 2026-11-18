// @vitest-environment jsdom
// The calendar's axis on a day the clocks change (#9851): its lines are the clock's hours, so a to-do and the moment sit on
// the line of their clock time, and a to-do picked up and let go where it is drawn stays at its time. jsdom has no layout:
// the column and the block say where they are, the block where its style puts it.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import type { DuePatch } from "../lib/todo-calendar";
import type { Todo } from "../lib/todos";
import { TodoCalendar } from "./TodoCalendar";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const HOUR = 48; // px an hour on the axis
const todo = (id: string, extra: Partial<Todo>): Todo => ({ id, title: id, agents: [], ...extra }) as Todo;

const savedTZ = process.env.TZ;
let host: HTMLDivElement;
let root: Root;
let onMove: Mock<(id: string, patch: DuePatch) => Promise<unknown>>;
let under: Element | null = null;

beforeAll(() => {
  process.env.TZ = "America/New_York";
});
afterAll(() => {
  if (savedTZ === undefined) delete process.env.TZ;
  else process.env.TZ = savedTZ;
});
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
  window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia;
  document.elementFromPoint = () => under;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  onMove = vi.fn(async () => {});
});
afterEach(() => {
  act(() => void vi.runOnlyPendingTimers());
  act(() => root.unmount());
  host.remove();
  under = null;
  vi.useRealTimers();
});

function draw(todos: Todo[], anchor: number) {
  act(() =>
    root.render(
      <TodoCalendar todos={todos} lookup={() => undefined} mode="day" anchor={anchor} showDone={false} onMode={() => {}} onAnchor={() => {}} onShowDone={() => {}} onOpen={() => {}} onAdd={() => {}} onShowList={() => {}} onMove={onMove} />,
    ),
  );
}

const column = () => host.querySelector<HTMLElement>('[data-testid="todo-calendar-column"]')!;
const entry = (id: string) => host.querySelector<HTMLElement>(`[data-testid="todo-calendar-entry"][data-todo-id="${id}"]`)!;
const blockTop = (id: string) => parseFloat((entry(id).parentElement as HTMLElement).style.top);
const nowTop = () => parseFloat(column().querySelector<HTMLElement>(".border-red")!.style.top);
function place(el: Element, top: number) {
  el.getBoundingClientRect = () => ({ top, bottom: top + 2000, left: 0, right: 400, width: 400, height: 2000, x: 0, y: top, toJSON() {} }) as DOMRect;
}
function pointer(target: EventTarget, type: string, clientY: number) {
  act(() => void target.dispatchEvent(new PointerEvent(type, { clientX: 100, clientY, pointerId: 1, button: 0, pointerType: "mouse", bubbles: true, cancelable: true })));
}

describe("the calendar on a day the clocks change (#9851)", () => {
  for (const [name, month, date] of [["clocks forward", 2, 8], ["clocks back", 10, 1]] as const) {
    it(`${name}: a 10:00 to-do and the moment sit on their hour's line`, () => {
      vi.setSystemTime(new Date(2026, month, date, 9, 30));
      draw([todo("a", { due_at: new Date(2026, month, date, 10).toISOString() })], new Date(2026, month, date).getTime());
      expect(blockTop("a")).toBe(10 * HOUR + 1);
      expect(nowTop()).toBe(9.5 * HOUR);
    });

    it(`${name}: picked up and let go where it is drawn, it keeps its time`, async () => {
      vi.setSystemTime(new Date(2026, month, date, 9, 30));
      draw([todo("a", { due_at: new Date(2026, month, date, 10).toISOString() })], new Date(2026, month, date).getTime());
      place(column(), 0);
      const top = blockTop("a");
      place(entry("a"), top);
      under = entry("a");
      pointer(entry("a"), "pointerdown", top + 10);
      under = column();
      pointer(window, "pointermove", top + 40); // a drag, then back to where it was picked up
      pointer(window, "pointermove", top + 10);
      pointer(window, "pointerup", top + 10);
      await act(async () => {});
      expect(onMove).not.toHaveBeenCalled();
    });
  }
});
