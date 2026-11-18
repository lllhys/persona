// The tick of a row that is picked or not: Inbox's Select (#2059), picking messages to forward (#3838) and the settings'
// checkbox and radio rows. Kept on its own so the chat can draw it without the settings' fields.
import { cx } from "./atoms";
import { Icon } from "./icons";

/** The tick of a role="checkbox" row; round for a role="radio" one. */
export function CheckMark({ on, round, className }: { on: boolean; round?: boolean; className?: string }) {
  return (
    <span aria-hidden className={cx("flex size-4 shrink-0 items-center justify-center transition-colors duration-150", round ? "rounded-full" : "rounded-chip", on ? "bg-accent text-white" : "shadow-[inset_0_0_0_1.5px_var(--line-strong)]", className)}>
      {on && <Icon name="check" size={10} stroke={3} />}
    </span>
  );
}
