// The pictures an agent looked at in a turn, under its answer (#1540; iOS / Mac #1543 draw the same): one picture is a
// card like a file card (its thumbnail, name, where it came from); several are a strip that scrolls sideways and fades
// at its edge. A tap opens the gallery of the turn's pictures. Tiles keep their keys and the strip its height, so a
// picture arriving while the agent works appends without moving anything above it.
import { useMemo, useRef, useState } from "react";
import { fileDownloadURL, fileThumbURL } from "../../lib/api";
import { openPanel } from "../../lib/panel";
import type { TurnImage } from "../../lib/turn-images";
import { copyText } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { edgeMask, useEdgeFade } from "./edge-fade";
import type { ViewerAction, ViewerImage } from "./ImageViewer";
import { ImageViewer } from "./LazyImageViewer";
import { imageSrc } from "./Steps";
import { t } from "../../i18n";

const TILE_H = 60;
const ONE_H = 88; // a lone picture: a little larger

/** The picture's own bytes (for the gallery) and a small one for the strip. */
function sources(img: TurnImage, mid: string, sid: string, cwd?: string): { full: string; thumb: string } {
  if (img.item) {
    const src = imageSrc(img.item, mid, sid);
    return { full: src, thumb: src };
  }
  return { full: fileDownloadURL(mid, img.path!, cwd, { inline: true }), thumb: fileThumbURL(mid, img.path!, cwd, 256) };
}

const SOURCE: Record<TurnImage["source"], string> = {
  read: t("chat-turnimages.source_read"),
  screenshot: t("chat-turnimages.source_screenshot"),
  browser: t("chat-turnimages.source_browser"),
  mcp: t("chat-turnimages.source_mcp"),
  tool: t("chat-turnimages.source_tool"),
};

function meta(img: TurnImage): string {
  // a screenshot's label already says what it is ("Screenshot · MacBook"): only the size then
  const what = img.label.startsWith(SOURCE[img.source]) ? "" : SOURCE[img.source];
  return [what, img.width && img.height ? `${img.width} × ${img.height}` : ""].filter(Boolean).join(" · ");
}

/** The turn's pictures, between its work row and its answer (#1540). */
export function TurnImages({ images, mid, sid, cwd }: { images: TurnImage[]; mid: string; sid: string; cwd?: string }) {
  const [open, setOpen] = useState<number | null>(null);
  if (!images.length) return null;
  return (
    <div className="flex flex-col gap-1.5" data-turn-images="">
      <Strip images={images} mid={mid} sid={sid} cwd={cwd} onOpen={setOpen} />
      {open !== null && <Gallery images={images} start={open} mid={mid} sid={sid} cwd={cwd} onClose={() => setOpen(null)} />}
    </div>
  );
}

/** The pictures as a row of thumbnails that scrolls sideways, fading at the edge that has more; one picture is just a
 * larger thumbnail. No words: names and sizes are on hover and in the gallery. */
function Strip({ images, mid, sid, cwd, onOpen }: { images: TurnImage[]; mid: string; sid: string; cwd?: string; onOpen: (i: number) => void }) {
  const row = useRef<HTMLDivElement>(null);
  const { edges, measure } = useEdgeFade(row, [images.length]);
  const mask = edgeMask(edges);
  return (
    <>
      <div
        ref={row}
        onScroll={measure}
        className="flex gap-2 overflow-x-auto overscroll-x-contain pb-0.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
        style={{ height: (images.length === 1 ? ONE_H : TILE_H) + 2, maskImage: mask, WebkitMaskImage: mask }}
      >
        {images.map((img, i) => (
          <Tile key={img.key} img={img} mid={mid} sid={sid} cwd={cwd} h={images.length === 1 ? ONE_H : TILE_H} onOpen={() => onOpen(i)} />
        ))}
      </div>
    </>
  );
}

function Tile({ img, mid, sid, cwd, h, onOpen }: { img: TurnImage; mid: string; sid: string; cwd?: string; h: number; onOpen: () => void }) {
  const { thumb } = sources(img, mid, sid, cwd);
  const [failed, setFailed] = useState(false);
  // the picture's shape, kept between square and a wide screenshot, so a row of mixed pictures stays even
  const ratio = img.width && img.height ? img.width / img.height : 1;
  const w = Math.round(h * Math.max(h === ONE_H ? 0.75 : 1, Math.min(1.78, ratio)));
  return (
    <button
      type="button"
      onClick={onOpen}
      title={[img.label, meta(img)].filter(Boolean).join(" · ")}
      aria-label={img.label}
      className="group relative shrink-0 overflow-hidden rounded-[10px] bg-field shadow-hairline outline-none focus-visible:ring-2 focus-visible:ring-accent"
      style={{ width: w, height: h, animation: "fade-in 220ms ease-out both" }}
    >
      {failed ? (
        // no thumbnail (the file is gone, or the machine cannot draw it): a quiet glyph; the gallery still tries the file
        <span className="flex size-full items-center justify-center text-ink-3">
          <Icon name="image" size={18} />
        </span>
      ) : (
        <img src={thumb} alt="" loading="lazy" decoding="async" onError={() => setFailed(true)} className="size-full object-cover transition-transform duration-200 group-hover:scale-[1.05]" />
      )}
      <span className="pointer-events-none absolute inset-x-0 bottom-0 truncate bg-gradient-to-t from-black/70 to-transparent px-1.5 pt-3 pb-1 text-left text-[10.5px] font-medium text-white opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-visible:opacity-100">
        {img.label}
      </span>
    </button>
  );
}

/** The turn's pictures in the chat's image viewer (pinch, double tap, swipe; ← / →, Esc), with the file's own
 *  buttons for a picture read from disk: copy its path, open it in the file panel. */
function Gallery({ images, start, mid, sid, cwd, onClose }: { images: TurnImage[]; start: number; mid: string; sid: string; cwd?: string; onClose: () => void }) {
  const shown = useMemo(
    () => images.map<ViewerImage>((img) => ({ id: img.key, src: sources(img, mid, sid, cwd).full, label: [img.label, meta(img)].filter(Boolean).join(" · "), name: img.path?.split("/").pop() })),
    [images, mid, sid, cwd],
  );
  const actions = (i: number): ViewerAction[] => {
    const path = images[i]?.path;
    if (!path) return [];
    return [
      { key: "path", icon: "link", label: t("chat-turnimages.copy_path"), run: () => void copyText(path) },
      {
        key: "file",
        icon: "folder",
        label: t("chat-turnimages.open_in_files"),
        run: () => {
          onClose();
          openPanel({ kind: "file", mid, path, cwd });
        },
      },
    ];
  };
  return <ImageViewer images={shown} start={start} onClose={onClose} actions={actions} />;
}
