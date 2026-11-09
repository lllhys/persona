import { describe, expect, it } from "vitest";
import { projectChoices, projectNames, todoProjectsOf } from "./todo-projects";

describe("knowledge-base projects for to-dos (#2581)", () => {
  const list = todoProjectsOf([
    { id: "launch", name: "Launch week", extra: "short · active" },
    { id: "community", name: "Community", extra: "long · active" },
    { id: "noise", name: "Lunch group", extra: "short · ignored" },
    { id: "diagnostics", name: "Diagnostics", extra: "long · paused" },
  ]);

  it("leaves out ignored projects and tells long-term ones", () => {
    expect(list.map((p) => [p.slug, p.long])).toEqual([
      ["launch", false],
      ["community", true],
      ["diagnostics", true],
    ]);
  });

  it("offers long-term projects first, by name, and keeps a to-do's own project the list lacks", () => {
    expect(projectChoices(list).map((p) => p.slug)).toEqual(["community", "diagnostics", "launch"]);
    expect(projectChoices(list, "gone").map((p) => p.slug)).toEqual(["gone", "community", "diagnostics", "launch"]);
    expect(projectChoices(list, "launch")).toHaveLength(3);
  });

  it("names a project by its slug, or shows the slug", () => {
    const name = projectNames(list);
    expect(name("community")).toBe("Community");
    expect(name("gone")).toBe("gone");
  });
});
