// An assistant's avatar (docs/assistant/managers.md "Clients", docs/design/DESIGN_SYSTEM.md 3.8): a disc in its color with the
// first letter of its name, the same rule on every client (lib/managers.ts avatarColor). Decorative: the name is
// always beside it, or the control that holds it says it. Optional marks: `status` at its foot (online, offline,
// installing) and `waiting` at its head (another assistant has something for the user).
import { avatarColor, avatarInitial, type Tone } from "../lib/managers";
import { cx } from "./atoms";

// installing is orange here: on a blue avatar the list's accent dot would not show
const STATUS: Record<Tone, string> = { green: "bg-green", grey: "bg-line-strong", accent: "bg-orange" };

export function AssistantAvatar({ id, name, isDefault, size = 24, status, waiting, ring = "ring-canvas", className }: {
  id: string;
  name: string;
  isDefault: boolean;
  /** The disc's diameter in px; the letter and the marks follow it. */
  size?: number;
  status?: Tone;
  waiting?: boolean;
  /** The color the marks are cut out of: the surface the avatar sits on. */
  ring?: string;
  className?: string;
}) {
  const mark = Math.max(7, Math.round(size * 0.28));
  return (
    <span aria-hidden data-assistant-avatar={id} className={cx("relative inline-flex shrink-0", className)} style={{ width: size, height: size }}>
      <span
        className="flex size-full items-center justify-center rounded-full font-semibold text-surface select-none"
        style={{ background: avatarColor(id, isDefault), fontSize: Math.round(size * 0.44), lineHeight: 1 }}
      >
        {avatarInitial(name)}
      </span>
      {status && <span data-avatar-status={status} className={cx("absolute -right-px -bottom-px rounded-full ring-2", ring, STATUS[status])} style={{ width: mark, height: mark }} />}
      {waiting && <span data-avatar-waiting className={cx("absolute -top-px -right-px rounded-full bg-attention ring-2", ring)} style={{ width: mark, height: mark }} />}
    </span>
  );
}
