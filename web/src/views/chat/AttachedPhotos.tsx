// Photos that came with one of your messages as `<attached_images>` paths (iOS AttachedImages, Android AttachedPhotos):
// the phone, this composer or Codex's local_image put them on the session's machine, the message carries where. A row
// of square thumbnails on your side above the bubble (file/thumb; the file itself when the machine draws none); a
// click opens them in the image viewer, whole.
import { useState } from "react";
import { fileDownloadURL, fileThumbURL } from "../../lib/api";
import { getSession } from "../../lib/store";
import { Icon } from "../../ui/icons";
import { ImageViewer } from "./LazyImageViewer";
import { t } from "../../i18n";

/** The side of a thumbnail: the same as an image item's in the bubble (Thumb size 96). */
const SIDE = 96;

export function AttachedPhotos({ paths, mid, sid }: { paths: string[]; mid: string; sid: string }) {
  const [open, setOpen] = useState<number | null>(null);
  // per photo: 0 its thumbnail, 1 the file itself (the machine could not draw a thumbnail), 2 neither
  const [failed, setFailed] = useState<Record<string, number>>({});
  if (!paths.length) return null;
  const cwd = getSession(mid, sid)?.cwd;
  return (
    <div className="flex flex-wrap justify-end gap-1.5" data-attached-photos>
      {paths.map((path, i) => (
        <button
          key={path}
          type="button"
          onClick={() => setOpen(i)}
          title={path}
          aria-label={t("chat-attachedphotos.photo", { n: i + 1 })}
          className="block shrink-0 overflow-hidden rounded-[10px] bg-field shadow-hairline outline-none transition-transform duration-150 focus-visible:ring-2 focus-visible:ring-accent active:scale-[0.98] motion-reduce:transition-none"
          style={{ width: SIDE, height: SIDE }}
        >
          {(failed[path] ?? 0) >= 2 ? (
            // the file is gone (or not a picture): a quiet glyph
            <span className="flex size-full items-center justify-center text-ink-3">
              <Icon name="image" size={18} />
            </span>
          ) : (
            <img
              src={failed[path] ? fileDownloadURL(mid, path, cwd, { inline: true }) : fileThumbURL(mid, path, cwd, 256)}
              alt=""
              loading="lazy"
              decoding="async"
              onError={() => setFailed((f) => ({ ...f, [path]: (f[path] ?? 0) + 1 }))}
              className="size-full object-cover"
            />
          )}
        </button>
      ))}
      {open !== null && (
        <ImageViewer images={paths.map((path) => ({ id: path, src: fileDownloadURL(mid, path, cwd, { inline: true }) }))} start={open} onClose={() => setOpen(null)} />
      )}
    </div>
  );
}
