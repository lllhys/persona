// UI version 9 (#9337, docs/extensions/extensions.md "UI version 9"): a tile's gauge, a ring filled to a ratio with the
// tile's value and unit in its centre and its title below, for a quota or a usage. A chunk of its own, loaded with the
// first tile that has one (ViewBody6.tsx TileView): an extension's page or card, never the first screen or the idle
// preload. TileView keeps the tile's surface, its press, badge, switch and menu; this draws what is inside.
import { hue, hueTint, type UITile } from "../../lib/extension-ui-v6";
import { cx } from "../../ui/atoms";
import { gaugePercent, gaugeUnit } from "./ViewBody6";

/** The ring's geometry in its 100 × 100 box: a stroke of 9 inside the edge. */
const R = 45.5;
const STROKE = 9;

/**
 * A gauge tile's face: the ring (a track in the color's tint, the arc from the top clockwise to the ratio, round ends),
 * the value and unit in its centre (the percentage when the tile has no value), the title and subtitle below. The arc
 * eases to a new ratio when the view is drawn again, and snaps with Reduce Motion (styles.css strips transitions). What
 * it says is read out by TileView's label, so the ring itself is hidden from assistive technology.
 */
export function GaugeFace({ tile, compact }: { tile: UITile; compact?: boolean }) {
  const g = tile.gauge!;
  const pct = gaugePercent(g.ratio);
  const color = hue(g.color, hue(tile.color, "var(--accent)"));
  const track = hueTint(g.color, hueTint(tile.color, "color-mix(in srgb, var(--accent) 14%, transparent)"));
  const centre = tile.value || `${pct}%`;
  const unit = tile.value && gaugeUnit(g);
  return (
    <span data-gauge className="flex min-w-0 flex-col items-center gap-0.5 p-3 text-center">
      <span className={cx("relative mb-1.5 grid shrink-0 place-items-center", compact ? "size-20" : "size-24")}>
        <svg viewBox="0 0 100 100" aria-hidden="true" className="absolute inset-0 size-full -rotate-90">
          <circle cx={50} cy={50} r={R} fill="none" stroke={track} strokeWidth={STROKE} />
          {pct > 0 && (
            <circle
              data-gauge-arc
              cx={50}
              cy={50}
              r={R}
              fill="none"
              stroke={color}
              strokeWidth={STROKE}
              strokeLinecap="round"
              pathLength={100}
              strokeDasharray="100 100"
              strokeDashoffset={100 - pct}
              className="transition-[stroke-dashoffset] duration-500 ease-out motion-reduce:transition-none"
            />
          )}
        </svg>
        <span className="relative flex max-w-[72%] min-w-0 flex-col items-center leading-tight">
          <span className={cx("max-w-full truncate font-semibold text-ink tabular-nums", compact ? "text-lg" : "text-xl")}>{centre}</span>
          {unit && <span className="max-w-full truncate text-submeta text-ink-3">{unit}</span>}
        </span>
      </span>
      <span className="max-w-full truncate text-meta font-medium text-ink">{tile.title}</span>
      {tile.subtitle && <span className="max-w-full truncate text-submeta text-ink-3">{tile.subtitle}</span>}
    </span>
  );
}
