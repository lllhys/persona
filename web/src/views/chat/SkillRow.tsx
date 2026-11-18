// The @ panel's skill presentation, also used in the / palette's shorter rows.
import type { ComponentProps } from "react";
import type { MentionSkill } from "../../lib/skill-mentions";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

export function SkillRow({ skill, common, compact = false }: {
  skill: MentionSkill;
  common: ComponentProps<"button">;
  /** The / palette's 36 px row (44 px on touch), with name, description and source on one line. */
  compact?: boolean;
}) {
  return (
    <button
      {...common}
      title={`${skill.name} · ${skill.source}\n${skill.description ?? ""}`}
      className={cx(
        "relative z-10 flex w-full items-center gap-2 text-left text-ink",
        compact ? "h-9 rounded-control px-2.5 text-sm leading-[1.43] [@media(pointer:coarse)]:h-11" : "min-h-14 rounded-[8px] px-[10px] py-2 text-sm",
      )}
    >
      <Icon name="spark" size={16} className="shrink-0 text-ink-3" />
      {compact ? (
        <>
          <span className="min-w-0 truncate font-medium">{skill.name}</span>
          {skill.description && <span className="min-w-0 flex-1 truncate text-xs font-normal text-ink-3">{skill.description}</span>}
          <span className="ml-auto max-w-[40%] shrink-0 truncate pl-1 text-xs font-normal text-ink-3">{skill.source}</span>
        </>
      ) : (
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline gap-2">
            <span className="truncate font-medium">{skill.name}</span>
            <span className="min-w-0 truncate text-xs text-ink-3">{skill.source}</span>
          </span>
          <span className="block truncate text-xs text-ink-3">{skill.description}</span>
        </span>
      )}
    </button>
  );
}
