// Keep execution-catalog I/O and skill presentation off the base panel's idle path.
import { useCallback, type ComponentProps } from "react";
import { matchingSkills } from "../../lib/skill-mentions";
import { ComposerPanel, type MentionRow } from "./ComposerPanel";
import { SkillRow } from "./SkillRow";
import { SkillCatalogStatus } from "./SkillCatalogStatus";
import { useSkillCatalog } from "./useSkillCatalog";
import { SlashSkillEntry } from "./SlashSkillView";
import type { SlashPaletteInput } from "./SlashSkillView";

type MentionProps = ComponentProps<typeof ComposerPanel>;

/** The same intent-loaded catalog chunk serves the @ panel and the skill-aware / palette. */
export function AssistantComposerPanel(props: MentionProps | (SlashPaletteInput & { slash: true })) {
  if ("slash" in props) return <SlashSkillEntry {...props} />;
  return <AssistantMentionPanel {...props} />;
}

function AssistantMentionPanel(props: MentionProps) {
  const source = props.source?.skills;
  const current = useSkillCatalog(source, props.machineId, props.source?.cwd);
  const rows = useCallback((query: string): MentionRow[] => matchingSkills(current.catalog?.skills ?? [], query).map((skill) => ({ kind: "skill", id: `skill:${skill.id}`, skill })), [current.catalog]);
  return (
    <ComposerPanel {...props} skills={{
      rows,
      loading: current.status === "loading",
      status: <SkillCatalogStatus status={current.status} />,
      row: (skill, common) => <SkillRow key={skill.id} skill={skill} common={common} />,
    }} />
  );
}
