// A run of a reply's pictures (#9475): reply-images-plugin.ts lifts it out of its paragraph, lib/reply-images.ts has
// the rules (iOS's ReplyPictureRun draws the same). One picture is a fixed height and its shape's width, never larger
// than itself; two or more are squares, the sixth counting those not drawn. The box is its final size before the
// picture arrives (only a lone one's width changes), a picture that did not load is a small tile that retries, and a
// click opens the image viewer on every picture of the reply. Pictures load through the hub's remote image route at
// the size drawn (`remote_images`); what other people wrote waits for a click, and so does every picture of a hub
// without the route (as #8162 had them, loaded straight from their host with no Referer once asked).
// Its own chunk: loaded with a reply's first picture. Sizes are inline styles, not new utility classes (those would
// land in every page's stylesheet).
import { createContext, useContext, useMemo, useState, type CSSProperties } from "react";
import type { Element } from "hast";
import { t } from "../i18n";
import { getToken } from "../lib/api";
import {
  grid,
  hostOf,
  MOST_SHOWN,
  PICTURE_FAILED,
  PICTURE_GAP,
  PICTURE_H,
  PICTURE_H_NARROW,
  PICTURES_W,
  picturesIn,
  PLACEHOLDER,
  remoteSize,
  single,
  type ReplyPicture,
  type Size,
} from "../lib/reply-image-layout";
import { isWebImage, remoteImageURL, REMOTE_IMAGES_FEATURE, VIEWER_SIZE } from "../lib/reply-images";
import { hubHas, useStore } from "../lib/store";
import type { ViewerImage } from "../views/chat/ImageViewer";
import { ImageViewer } from "../views/chat/LazyImageViewer";
import { Icon } from "./icons";
import { NARROW, useMedia } from "./media-query";
import { ReplyPicturesContext } from "./reply-pictures-context";

export { rehypeReplyImages } from "./reply-images-plugin";

/** Set by Export image (#9469, views/chat/ExportImage.tsx): a run draws all its pictures, through the hub only, for a
 *  picture that cannot be tapped (no "+N", no viewer, no waiting for a tap). Here, not with the first screen's context. */
export const ReplyPicturesExport = createContext(false);

const RADIUS = 10; // the turn's viewed images' tiles (TurnImages.tsx)
const tileClass = "group relative shrink-0 overflow-hidden bg-field shadow-hairline outline-none focus-visible:ring-2 focus-visible:ring-accent";

/** The pictures a run's element holds. */
function picturesOf(node: Element | undefined): ReplyPicture[] {
  return (node?.children ?? []).flatMap((c) => {
    if (c.type !== "element" || c.tagName !== "img") return [];
    const src = typeof c.properties.src === "string" ? c.properties.src : "";
    return isWebImage(src) ? [{ src, alt: typeof c.properties.alt === "string" ? c.properties.alt : "" }] : [];
  });
}

export function ReplyImages({ node }: { node?: Element }) {
  const pictures = picturesOf(node);
  const { text, ask } = useContext(ReplyPicturesContext);
  const all = useMemo(() => picturesIn(text), [text]);
  const through = useStore((s) => hubHas(s.features, REMOTE_IMAGES_FEATURE));
  const narrow = useMedia(NARROW);
  const [asked, setAsked] = useState(false);
  const [open, setOpen] = useState<number | null>(null);
  const exporting = useContext(ReplyPicturesExport);
  if (!pictures.length) return null;
  if (exporting) return <ExportPictures pictures={pictures} through={through} />;
  const height = narrow ? PICTURE_H_NARROW : PICTURE_H;
  const ratio = typeof devicePixelRatio === "number" ? devicePixelRatio : 1;
  const src = (p: ReplyPicture, size: number) => (through ? remoteImageURL(p.src, size, getToken()) : p.src);
  // the viewer pages through the whole reply's pictures, from the one clicked
  const gallery = pictures.every((p) => all.some((a) => a.src === p.src)) ? all : pictures;
  const at = (p: ReplyPicture) => Math.max(0, gallery.findIndex((g) => g.src === p.src));
  const viewer: ViewerImage[] = gallery.map((g, i) => ({ id: `${i}:${g.src}`, src: src(g, VIEWER_SIZE), label: [g.alt, hostOf(g.src)].filter(Boolean).join(" · ") }));
  const waiting = (ask || !through) && !asked;
  return (
    <div data-reply-images="" style={{ margin: "8px 0" }}>
      {waiting ? (
        <Asked pictures={pictures} height={height} onAsk={() => setAsked(true)} />
      ) : pictures.length === 1 ? (
        <Single picture={pictures[0]} height={height} src={src(pictures[0], remoteSize(height * PLACEHOLDER, ratio))} onOpen={() => setOpen(at(pictures[0]))} />
      ) : (
        <Squares pictures={pictures} src={(p) => src(p, remoteSize(gridFor(pictures.length).tile * PLACEHOLDER, ratio))} onOpen={(p) => setOpen(at(p))} />
      )}
      {open !== null && <ImageViewer images={viewer} start={open} onClose={() => setOpen(null)} />}
    </div>
  );
}

const gridFor = (count: number) => grid(count, PICTURES_W);

/** The squares' container: `columns` to a row, as wide as the column allows up to PICTURES_W (four: two of the
 *  three-to-a-row squares). */
function squaresStyle(count: number): CSSProperties {
  const g = gridFor(count);
  const width = count === 4 ? `calc((min(100%, ${PICTURES_W}px) - ${2 * PICTURE_GAP}px) * 2 / 3 + ${PICTURE_GAP}px)` : `min(100%, ${PICTURES_W}px)`;
  return { display: "grid", gridTemplateColumns: `repeat(${g.columns}, minmax(0, 1fr))`, gap: PICTURE_GAP, width };
}

function Single({ picture, height, src, onOpen }: { picture: ReplyPicture; height: number; src: string; onOpen: () => void }) {
  const [pixels, setPixels] = useState<Size | null>(null);
  const [failed, setFailed] = useState(false);
  // the column is CSS's (min(100%, …)): wider than it, a picture is cut around its centre at the same height
  const box = failed ? { w: PICTURE_FAILED, h: PICTURE_FAILED } : single(pixels, Number.POSITIVE_INFINITY, height);
  return (
    <Tile
      picture={picture}
      src={src}
      onOpen={onOpen}
      style={{ width: `min(100%, ${box.w}px)`, height: box.h, transition: "width 200ms ease-out" }}
      onPixels={setPixels}
      onFailed={setFailed}
    />
  );
}

function Squares({ pictures, src, onOpen }: { pictures: ReplyPicture[]; src: (p: ReplyPicture) => string; onOpen: (p: ReplyPicture) => void }) {
  const g = gridFor(pictures.length);
  const shown = pictures.slice(0, Math.min(g.shown, MOST_SHOWN));
  return (
    <div style={squaresStyle(pictures.length)}>
      {shown.map((p, i) => (
        <Tile key={`${i}:${p.src}`} picture={p} src={src(p)} onOpen={() => onOpen(p)} style={{ aspectRatio: "1 / 1" }} more={i === shown.length - 1 ? g.more : 0} />
      ))}
    </div>
  );
}

/** One picture filling its box: the field's color while it comes, then the picture (cut to the box); one that did not
 *  load shows the picture mark and retries on a click. */
function Tile({
  picture,
  src,
  onOpen,
  style,
  more = 0,
  onPixels,
  onFailed,
}: {
  picture: ReplyPicture;
  src: string;
  onOpen: () => void;
  style: CSSProperties;
  more?: number;
  onPixels?: (s: Size) => void;
  onFailed?: (failed: boolean) => void;
}) {
  const [state, setState] = useState<"loading" | "loaded" | "failed">("loading");
  const [attempt, setAttempt] = useState(0);
  const host = hostOf(picture.src);
  const failed = state === "failed";
  return (
    <button
      type="button"
      onClick={() => {
        if (!failed) return onOpen();
        setState("loading");
        onFailed?.(false);
        setAttempt((n) => n + 1);
      }}
      title={failed ? `${t("ui-replyimages.retry")} · ${host}` : [picture.alt, host].filter(Boolean).join(" · ")}
      aria-label={failed ? t("ui-replyimages.retry") : picture.alt || t("ui-replyimages.picture")}
      data-reply-picture={failed ? "failed" : state}
      className={tileClass}
      style={{ borderRadius: RADIUS, padding: 0, ...style }}
    >
      {failed ? (
        <span className="flex size-full items-center justify-center text-ink-3">
          <Icon name="image" size={18} />
        </span>
      ) : (
        <img
          key={attempt}
          src={src}
          alt=""
          decoding="async"
          loading="lazy"
          referrerPolicy="no-referrer"
          onLoad={(e) => {
            setState("loaded");
            onPixels?.({ w: e.currentTarget.naturalWidth, h: e.currentTarget.naturalHeight });
          }}
          onError={() => {
            setState("failed");
            onFailed?.(true);
          }}
          style={{ display: "block", width: "100%", height: "100%", maxWidth: "none", objectFit: "cover", borderRadius: 0, opacity: state === "loaded" ? 1 : 0, transition: "opacity 200ms ease-out" }}
        />
      )}
      {more > 0 && !failed && (
        <span className="absolute inset-0 flex items-center justify-center text-xl font-semibold text-white" style={{ background: "rgba(0, 0, 0, 0.45)" }} aria-label={t("ui-replyimages.more", { n: more })}>
          +{more}
        </span>
      )}
    </button>
  );
}

/** A run not loaded yet: its boxes in the field's color, the site and Load over them. */
function Asked({ pictures, height, onAsk }: { pictures: ReplyPicture[]; height: number; onAsk: () => void }) {
  const one = pictures.length === 1;
  const g = gridFor(pictures.length);
  const box = single(null, Number.POSITIVE_INFINITY, height);
  return (
    <button
      type="button"
      onClick={onAsk}
      data-reply-picture="asked"
      className="relative block text-left outline-none focus-visible:ring-2 focus-visible:ring-accent"
      style={{ borderRadius: RADIUS, padding: 0, ...(one ? { width: `min(100%, ${box.w}px)`, height: box.h } : squaresStyle(pictures.length)) }}
    >
      {one ? (
        <span className="block size-full bg-field shadow-hairline" style={{ borderRadius: RADIUS }} />
      ) : (
        pictures.slice(0, g.shown).map((p, i) => <span key={`${i}:${p.src}`} className="block bg-field shadow-hairline" style={{ borderRadius: RADIUS, aspectRatio: "1 / 1" }} />)
      )}
      <span className="absolute inset-0 flex flex-col items-center justify-center gap-0.5 p-2 text-center">
        <span className="inline-flex items-center gap-1.5 text-meta text-ink-2">
          <Icon name="image" size={14} />
          <span className="whitespace-nowrap">{one ? t("ui-replyimages.load_image") : t("ui-replyimages.load_images", { n: pictures.length })}</span>
        </span>
        <span className="max-w-full truncate text-submeta text-ink-3">{hostOf(pictures[0].src)}</span>
      </span>
    </button>
  );
}


/** A run in an exported picture (#9469): one picture at most PICTURE_H_NARROW high and never larger than itself; several
 *  as squares, every one of them; each through the hub's route, or, when the hub has none, a tile naming the site:
 *  nothing is read from another site. */
function ExportPictures({ pictures, through }: { pictures: ReplyPicture[]; through: boolean }) {
  const size = remoteSize(PICTURE_H_NARROW * PLACEHOLDER, 3);
  const src = (p: ReplyPicture) => remoteImageURL(p.src, size, getToken());
  const tile = (p: ReplyPicture, style: CSSProperties) =>
    through ? (
      <img key={p.src} src={src(p)} alt={p.alt} style={{ ...style, objectFit: "cover", borderRadius: RADIUS, display: "block" }} data-export-picture="" />
    ) : (
      <div key={p.src} style={{ ...style, borderRadius: RADIUS }} className="flex flex-col items-center justify-center gap-1 bg-field text-ink-3" data-export-picture="away">
        <Icon name="image" size={18} />
        <span className="max-w-full truncate px-2 text-submeta">{hostOf(p.src)}</span>
      </div>
    );
  if (pictures.length === 1) {
    const p = pictures[0];
    return <div style={{ margin: "8px 0" }}>{through ? tile(p, { maxHeight: PICTURE_H_NARROW, maxWidth: "100%" }) : tile(p, { height: PICTURE_H_NARROW * 0.6, width: PICTURE_H_NARROW })}</div>;
  }
  const g = grid(pictures.length, PICTURES_W);
  return (
    <div style={{ margin: "8px 0", display: "grid", gridTemplateColumns: `repeat(${g.columns}, minmax(0, 1fr))`, gap: PICTURE_GAP, width: `min(100%, ${PICTURES_W}px)` }}>
      {pictures.map((p) => tile(p, { width: "100%", aspectRatio: "1 / 1" }))}
    </div>
  );
}
