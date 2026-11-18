// Skill-aware second stage of the / palette, loaded only after the palette opens.
import { useEffect, useMemo } from "react";
import { rankCommands, SLASH_LIMIT } from "../../lib/composer-commands";
import type { ComposerCommand as Command } from "../../lib/extension-prompts";
import type { MentionSkill } from "../../lib/skill-mentions";
import { SkillRow } from "./SkillRow";
import { useSkillCatalog } from "./useSkillCatalog";
import { SlashSkillView, type SlashPaletteInput } from "./SlashSkillView";

type SkillCommand = Command & { skill: MentionSkill };
const attached = (row: Command | undefined) => row && (row as SkillCommand).skill;

export function SlashSkillPalette(input: SlashPaletteInput) {
  const { source, machineId } = input;
  const query = input.query ?? "";
  const catalog = useSkillCatalog(source?.skills, machineId, source?.cwd);
  const skillRows = useMemo(() => {
    const remaining = SLASH_LIMIT - input.rows.length;
    if (remaining <= 0) return [];
    const candidates: SkillCommand[] = (catalog.catalog?.skills ?? []).map((skill) => ({
      key: `skill:${skill.id}`, word: skill.name, desc: skill.description ?? "", text: skill.name, source: skill.source, skill,
    }));
    return rankCommands(candidates, query, remaining);
  }, [catalog.catalog, query, input.rows.length]);
  const entries = useMemo<Command[]>(() => [...input.rows, ...skillRows], [input.rows, skillRows]);
  const current = Math.min(input.active, entries.length - 1);
  const pick: SlashPaletteInput["onPick"] = (row) => {
    if ("kind" in row) return input.onPick(row);
    const skill = attached(row);
    input.onPick(skill ? { kind: "skill", id: `skill:${skill.id}`, skill } : row);
  };
  // Capture inside this composer, before its special /model and /permissions handling. Commands fall through to it;
  // arrows and a highlighted skill are handled here across the combined list.
  useEffect(() => {
    const root = input.within?.current;
    if (!root) return;
    const key = (e: KeyboardEvent) => {
      if (e.isComposing || e.keyCode === 229) return;
      const modEnter = e.ctrlKey || e.metaKey;
      if ((e.key === "ArrowDown" || e.key === "ArrowUp") && entries.length) {
        e.preventDefault();
        e.stopPropagation();
        input.onHover((current + (e.key === "ArrowDown" ? 1 : entries.length - 1)) % entries.length);
      } else if (((e.key === "Enter" && !modEnter) || e.key === "Tab") && attached(entries[current])) {
        e.preventDefault();
        e.stopPropagation();
        pick(entries[current]);
      }
    };
    root.addEventListener("keydown", key, true);
    return () => root.removeEventListener("keydown", key, true);
  });
  return (
    <SlashSkillView
      {...input}
      entries={entries}
      onPick={pick}
      status={catalog.status}
      renderRow={(row, common) => {
        const skill = attached(row);
        return skill && <SkillRow key={skill.id} skill={skill} common={common} compact />;
      }}
    />
  );
}
