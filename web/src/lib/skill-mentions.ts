import { LEAD_CONTEXT_RE, CONTEXT_RE } from "./compatibility";
import { contextXML } from "./context-xml";
import type { ContextRef } from "./links";
import { tagAttrs, type Context } from "./turns";

export interface MentionSkill {
  id: string;
  name: string;
  description?: string;
  source: string;
}

export interface SkillCatalog {
  catalog_version: number;
  skills: MentionSkill[];
  complete: boolean;
}

/** Execution environment for a picker's catalog. Ordinary Agents use session-owned facts. */
export interface SkillSource {
  manager: string;
  session?: string;
  launcher?: string;
  environment: string;
  agent?: boolean;
}

export function matchingSkills(skills: MentionSkill[], query: string): MentionSkill[] {
  const q = query.toLocaleLowerCase();
  return skills.filter((s) => `${s.name}\n${s.description ?? ""}`.toLocaleLowerCase().includes(q));
}

export function skillContext(skill: MentionSkill): ContextRef {
  return { id: `skill:${skill.id}`, kind: "skill", title: skill.name, url: "", text: [skill.source, skill.description].filter(Boolean).join("\n"), attrs: { ref: skill.id } };
}

/** The same opaque reference travels in drafts, outbox/retry, queued text and history. Never infer one from @text. */
export function skillDraft(refs: ContextRef[], text: string): string {
  const skills = refs.filter((r) => r.kind === "skill");
  return skills.length ? `${skills.map(contextXML).join("\n")}\n\n${text}` : text;
}

export function skillEditText(contexts: Context[], text: string): string {
  return skillDraft(contexts.filter((c) => c.kind === "skill").map((c) => ({
    id: `skill:${c.attrs?.ref}`, kind: "skill", title: c.title ?? "", url: c.url ?? "", text: c.text, attrs: c.attrs,
  })), text);
}

/** Pull only leading explicit selections back into removable chips. Keep existing non-skill context bytes. */
export function splitSkillDraft(raw: string): { text: string; skills: ContextRef[] } {
  const skills: ContextRef[] = [];
  const kept: string[] = [];
  let rest = raw;
  for (;;) {
    const m = LEAD_CONTEXT_RE.exec(rest);
    if (!m) break;
    const tag = new RegExp(CONTEXT_RE.source).exec(m[0]);
    if (!tag) break;
    const { kind, title, url, ...attrs } = tagAttrs(tag[1]);
    if (kind === "skill" && attrs.ref) {
      if (!skills.some((s) => s.attrs?.ref === attrs.ref)) skills.push({ id: `skill:${attrs.ref}`, kind, title: title ?? "", url: url ?? "", text: tag[2].trim(), attrs });
    } else if (kind !== "skill-instructions") {
      kept.push(m[0].trim());
    }
    rest = rest.slice(m[0].length);
  }
  if (!skills.length && !raw.includes('kind="skill-instructions"')) return { text: raw, skills };
  return { text: [...kept, rest.trimStart()].filter(Boolean).join("\n\n"), skills };
}
