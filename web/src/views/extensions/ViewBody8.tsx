// UI version 8 (#9158, docs/extensions/extensions.md "UI version 8"): a dashboard page's columns layout, and a section's
// icon and link beside its title. A chunk of its own, loaded with the first view that has them (ViewBody.tsx
// ViewSections): an extension's page, never the first screen or the idle preload. It draws the view through
// ViewSections and Section again, giving them its head for the sections' titles.
import type { UISectionLink, UIView } from "../../lib/extension-ui";
import { hue } from "../../lib/extension-ui-v6";
import { cx, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { ROW_ICON, Section, ViewSections, type SectionHeadAt, type ViewHandlers } from "./ViewBody";
import { glyphOf } from "./ViewBody6";

type Props = ViewHandlers & { view: UIView; embedded?: boolean; compact?: boolean };

/**
 * A view with UI version 8 in it. A columns view on a page is a grid of cards: one column on a narrow page, two once
 * each card keeps 24rem, three once three do; left to right, then down, as a narrow page stacks them; each row of cards
 * as tall as its tallest (the grid stretches them); a section with span full takes a whole row. Any other view, or a
 * columns view in a card or inside another page, is drawn as before (a stack), with the sections' icons and links.
 */
export function Sections8({ view, embedded, compact, ...handlers }: Props) {
  const h: ViewHandlers = { ...handlers, head: Head };
  if (view.layout !== "columns" || compact || embedded) return <ViewSections view={view} embedded={embedded} compact={compact} {...h} />;
  return (
    <div className="@container">
      <div data-columns className="grid grid-cols-1 gap-3 @min-[49rem]:grid-cols-2 @min-[74rem]:grid-cols-3">
        {view.sections.map((s, i) => (
          <div key={s.key ?? i} data-span={s.span === "full" ? "full" : undefined} className={cx("min-w-0 overflow-hidden rounded-card bg-surface pb-1.5 shadow-card", s.span === "full" && "col-span-full")}>
            <Section section={s} card {...h} />
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * A section's title with its icon (in its color) before it and its link at the end. In a card it is the card's head,
 * as an overview card's; elsewhere it is drawn where and as the plain title is, and a section with neither icon nor
 * link keeps the plain one (undefined).
 */
function Head({ section: s, card, compact, embedded, bare, runs }: SectionHeadAt, h: ViewHandlers) {
  const glyph = glyphOf(s.icon, ROW_ICON);
  if (!card && !glyph && !s.link) return undefined;
  if (!s.title && !glyph && !s.link) return null;
  return (
    <div className={cx("flex min-w-0 items-center", card ? "gap-2 px-4 pt-3 pb-1" : cx("gap-1.5", compact ? "px-4 pt-1" : embedded ? "mb-1.5 px-1" : bare ? "px-3 pt-2 pb-0.5" : "mb-1.5 px-1", runs > 1 && "-mb-1.5"))}>
      {glyph && <Icon name={glyph} size={card ? 14 : 12} className="shrink-0" style={{ color: hue(s.icon_color, card ? "var(--ink-2)" : "var(--ink-3)") }} />}
      <h3 className={cx("min-w-0 flex-1 font-medium break-words", card ? "text-meta text-ink" : cx("text-ink-3", compact || embedded || bare ? "text-submeta" : "text-label"))}>{s.title}</h3>
      {s.link && <SectionLink link={s.link} h={h} />}
    </div>
  );
}

/** The link beside a title: its words and ›, pressed as a row is (its action, or the page it pushes). */
function SectionLink({ link, h }: { link: UISectionLink; h: ViewHandlers }) {
  const pending = h.busy === link.action.id;
  return (
    <button
      type="button"
      disabled={!!h.busy || (h.canPress ? !h.canPress(link.action) : false)}
      onClick={() => h.onPress(link.action)}
      className="touch-hit -my-1 -mr-1.5 flex max-w-[60%] shrink-0 items-center gap-0.5 rounded-control px-1.5 py-1 text-label whitespace-nowrap text-accent-ink transition-colors enabled:hover:bg-hover disabled:text-ink-3"
    >
      <span className="truncate">{link.title}</span>
      {pending ? <Spinner /> : <Icon name="chevronRight" size={11} stroke={2.2} className="shrink-0" />}
    </button>
  );
}
