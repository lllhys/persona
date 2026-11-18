import { describe, expect, it } from "vitest";
import { contextXML } from "./context-xml";
import { skillContext, skillDraft, splitSkillDraft, matchingSkills, skillEditText } from "./skill-mentions";
import { parseUserText } from "./turns";

const review = { id: "opaque-a", name: "review", description: "Inspect security boundaries", source: "project · ." };

describe("explicit skill references", () => {
  it("matches descriptions and keeps duplicate source identities", () => {
    const other = { ...review, id: "opaque-b", source: "user" };
    expect(matchingSkills([review, other], "SECURITY")).toEqual([review, other]);
    expect(skillContext(review).attrs?.ref).not.toBe(skillContext(other).attrs?.ref);
  });

  it("round trips drafts, edit/queue input and send history without turning prose into selections", () => {
    const selection = skillContext(review);
    const text = "Email me@example.com, literal @review and @src/file.ts";
    const saved = skillDraft([selection], text);
    const restored = splitSkillDraft(saved);
    expect(restored.text).toBe(text);
    expect(restored.skills).toEqual([selection]);
    expect(skillDraft(restored.skills, restored.text)).toBe(saved);
    expect(splitSkillDraft(text)).toEqual({ text, skills: [] });
    expect(splitSkillDraft(`Quoted:\n${contextXML(selection)}`).skills).toEqual([]);
    expect(splitSkillDraft(`> ${contextXML(selection)}`).skills).toEqual([]);
    expect(parseUserText(saved).contexts[0].attrs?.ref).toBe(review.id);
  });

  it("preserves existing context tags and removes only explicit skill tags from the editor", () => {
    const file = contextXML({ kind: "context", title: "file", url: "", text: "@src/main.ts" });
    const saved = file + "\n" + skillDraft([skillContext(review)], "Check this");
    expect(splitSkillDraft(saved)).toEqual({ text: file + "\n\nCheck this", skills: [skillContext(review)] });
    expect(skillDraft([], "")).toBe("");
  });

  it("preserves a sent selection when editing history, without turning ordinary quoted contexts into calls", () => {
    const selection = skillContext(review);
    const contexts = parseUserText(skillDraft([selection], "Check this")).contexts;
    expect(splitSkillDraft(skillEditText(contexts, "Check this")).skills).toEqual([selection]);
    expect(skillEditText([{ kind: "context", title: "@review", text: "@review" }], "Ordinary")).toBe("Ordinary");
  });
});
