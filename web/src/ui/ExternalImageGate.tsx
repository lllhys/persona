// The external-image consent gate, loaded only when Markdown actually contains an off-hub image. Same-origin images
// never load this chunk or its words (ExternalImage.tsx).
import { useContext, useState, type ComponentProps, type CSSProperties } from "react";
import { t } from "../i18n";
import { getToken } from "../lib/api";
import { INLINE_SIZE, isWebImage, remoteImageURL, REMOTE_IMAGES_FEATURE, VIEWER_SIZE } from "../lib/reply-images";
import { hubHas, useStore } from "../lib/store";
import { Icon } from "./icons";
import { ReplyPicturesContext } from "./reply-pictures-context";

type GateState = "idle" | "loading" | "failed";

function hostOf(src: string): string {
  try {
    // Same page-origin base as isOwnImageURL, so a protocol-relative //host/x labels the host instead of the raw string.
    return new URL(src, typeof location !== "undefined" ? location.href : undefined).host;
  } catch {
    return src;
  }
}

const gateClass =
  "my-0.5 inline-flex items-center gap-1.5 rounded-chip bg-inset px-2.5 py-1 text-label text-ink-2 shadow-btn hover:bg-hover hover:text-ink";

/** A picture inside a line of text: the line's height, its own width. */
const INLINE: CSSProperties = { display: "inline", height: "1.2em", width: "auto", maxWidth: "none", verticalAlign: "-0.25em", borderRadius: 3 };

// With the hub's remote image route (`remote_images`, #9475) the host sees the hub, never the reader: a web picture
// inside a line of text the user's own Agents or assistant wrote loads through it at once, as tall as the line (block
// pictures are runs, ReplyImages.tsx); in what other people wrote (`askImages`) the gate stays, and loads through it.
export function ExternalImageGate({ node: _node, src = "", alt = "", ...rest }: ComponentProps<"img"> & { node?: unknown }) {
  const through = useStore((s) => hubHas(s.features, REMOTE_IMAGES_FEATURE)) && isWebImage(src);
  const { ask } = useContext(ReplyPicturesContext);
  const [gate, setGate] = useState<GateState>("idle");
  if (through && !ask) return <img src={remoteImageURL(src, INLINE_SIZE, getToken())} alt={alt} decoding="async" loading="lazy" style={INLINE} />;
  if (gate !== "loading") {
    const host = hostOf(src);
    return (
      <button
        type="button"
        className={gateClass}
        title={alt || src}
        data-external-image={gate === "failed" ? "retry" : "gate"}
        onClick={() => setGate("loading")}
      >
        <Icon name="image" size={14} />
        <span>{t(gate === "failed" ? "ui-externalimage.image_retry" : "ui-externalimage.image_load", { host })}</span>
      </button>
    );
  }
  // The src reaches an <img> only after the tap, and the request carries no Referer header.
  const load = through ? remoteImageURL(src, VIEWER_SIZE, getToken()) : src;
  return <img {...rest} src={load} alt={alt} referrerPolicy="no-referrer" onError={() => setGate("failed")} data-external-image="loading" />;
}
