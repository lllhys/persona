// Making an avatar from an image (#2404, docs/apple/persona.md "Uploading an image"): the dialog the avatar picker's
// Upload card opens. Pick an image (or drop one), crop it square with the avatar's circle over it — drag to move; the
// slider, the wheel or a pinch to zoom; arrow keys and + / − from the keyboard — name it, and the hub makes a package
// of it and chooses it. The geometry, the export and the request are lib/look-upload.ts; the picker (Persona.tsx)
// opens this and takes the answer.
import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { isAbort } from "../../lib/api";
import {
  acceptsType, bigEnough, canSend, centeredCrop, clampCrop, exportCrop, exportRect, maxZoom, MIN_SIDE, NAME_MAX, panCrop,
  readImage, refusalText, SUBTITLE_MAX, textLength, uploadBody, uploadLook, UPLOAD_TYPES, zoomCrop,
  type Crop, type PickedImage, type UploadAnswer,
} from "../../lib/look-upload";
import { Button, cx, Dialog } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { t } from "../../i18n";
import { inputClass } from "./parts";

/** What the dialog uses to read, export and send; tests give their own. */
export interface LookUploadDeps {
  read?: (file: Blob) => Promise<PickedImage>;
  render?: typeof exportCrop;
  send?: typeof uploadLook;
}

type Phase = "idle" | "reading" | "preparing" | "sending" | "making";

/** Zooming in or out one step from the keyboard; a drag step from the arrow keys (CSS pixels, Shift: four times). */
const KEY_ZOOM = 1.1;
const KEY_STEP = 8;

/** A tick for the spinner to show before the canvas work blocks the page for a moment. */
const paint = () => new Promise<void>((done) => setTimeout(done, 30));

/** `onSend` as the request leaves (the picker notes where the choices stood then), `onUploaded` with the hub's answer. */
export function LookUploadDialog({ open, onClose, onSend, onUploaded, deps = {} }: { open: boolean; onClose: () => void; onSend?: () => void; onUploaded: (answer: UploadAnswer) => void; deps?: LookUploadDeps }) {
  const read = deps.read ?? readImage;
  const render = deps.render ?? exportCrop;
  const send = deps.send ?? uploadLook;
  const [picked, setPicked] = useState<PickedImage | null>(null);
  const [crop, setCrop] = useState<Crop>({ x: 0, y: 0, zoom: 1 });
  const [name, setName] = useState("");
  const [subtitle, setSubtitle] = useState("");
  const [phase, setPhase] = useState<Phase>("idle");
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState("");
  const [over, setOver] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const kept = useRef<PickedImage | null>(null);
  const picks = useRef(0);
  const busy = useRef(false);
  const abort = useRef<AbortController | null>(null);
  const ids = useId();
  const sending = phase === "preparing" || phase === "sending" || phase === "making";

  // Closed, it starts over (what it showed stays on screen while it leaves): the decoded image is let go here, when
  // another replaces it and when the dialog goes.
  const letGo = () => {
    abort.current?.abort();
    kept.current?.release();
    kept.current = null;
    picks.current++; // an image still being decoded is let go as it comes
  };
  useEffect(() => {
    if (open) return;
    letGo();
    setPicked(null);
    setName("");
    setSubtitle("");
    setPhase("idle");
    setError("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  // Gone without closing (the page left under it, Back on a wide screen): the same, so a request still going out
  // stops as Cancel stops it.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => letGo, []);

  const pick = async (file: File | undefined) => {
    if (!file || busy.current) return;
    setError("");
    if (!acceptsType(file.type)) {
      setError(t("assistant-lookupload.not_image"));
      return;
    }
    const n = ++picks.current;
    setPhase("reading");
    let img: PickedImage;
    try {
      img = await read(file);
    } catch {
      if (n === picks.current) {
        setPhase("idle");
        setError(t("assistant-lookupload.not_image"));
      }
      return;
    }
    if (n !== picks.current || busy.current) return img.release();
    setPhase("idle");
    if (!bigEnough(img.size)) {
      img.release();
      setError(t("assistant-lookupload.too_small", { px: MIN_SIDE }));
      return;
    }
    kept.current?.release();
    kept.current = img;
    setPicked(img);
    setCrop(centeredCrop(img.size));
  };

  const ready = !!picked && canSend(name, subtitle) && phase === "idle";

  const submit = async () => {
    if (busy.current || !picked || !ready) return;
    busy.current = true;
    const ctl = new AbortController();
    abort.current = ctl;
    setError("");
    setProgress(0);
    setPhase("preparing");
    try {
      await paint();
      if (ctl.signal.aborted) return;
      let image: string;
      try {
        image = render(picked.source, picked.size, crop).image;
      } catch (e) {
        setError(t("assistant-lookupload.export_failed", { error: e instanceof Error ? e.message : String(e) }));
        setPhase("idle");
        return;
      }
      setPhase("sending");
      onSend?.();
      const answer = await send(uploadBody(name, subtitle, image), {
        signal: ctl.signal,
        onProgress: (sent, total) => {
          const f = total > 0 ? Math.min(1, sent / total) : 0;
          setProgress(f);
          // every byte is there: the hub is making the package now
          if (f >= 1) setPhase("making");
        },
      });
      if (ctl.signal.aborted) return;
      onUploaded(answer);
      onClose();
    } catch (e) {
      if (ctl.signal.aborted || isAbort(e)) return;
      setError(refusalText(e));
      setPhase("idle");
    } finally {
      busy.current = false;
      if (abort.current === ctl) abort.current = null;
    }
  };

  // Closing stops a request still going out; one the hub already has may still make the avatar (its pushes show it).
  const close = () => {
    abort.current?.abort();
    onClose();
  };

  const nameLength = textLength(name.trim());
  const subtitleLength = textLength(subtitle.trim());
  const enter = (e: React.KeyboardEvent) => {
    if (e.key !== "Enter" || e.nativeEvent.isComposing) return;
    e.preventDefault();
    void submit();
  };
  const top = maxZoom(picked?.size ?? { w: 1, h: 1 });
  const percent = Math.round(progress * 100);

  const footer = (
    <>
      {picked && !sending && (
        <Button tone="quiet" icon="image" className="-ml-3" busy={phase === "reading"} onClick={() => fileInput.current?.click()}>{t("assistant-lookupload.change")}</Button>
      )}
      <span className="mr-auto flex min-w-0 flex-1 items-center gap-2 text-[12px] text-ink-3" aria-live="polite">
        {phase === "sending" && (
          <>
            <span role="progressbar" aria-label={t("assistant-lookupload.progress")} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent} className="h-1 w-full max-w-20 min-w-6 overflow-hidden rounded-full bg-hover-2">
              <span className="block h-full rounded-full bg-accent transition-[width] duration-150" style={{ width: `${percent}%` }} />
            </span>
            <span className="truncate whitespace-nowrap tabular-nums">{t("assistant-lookupload.uploading", { percent })}</span>
          </>
        )}
        {(phase === "preparing" || phase === "making") && <span className="truncate whitespace-nowrap">{t(phase === "preparing" ? "assistant-lookupload.preparing" : "assistant-lookupload.making")}</span>}
      </span>
      <Button tone="quiet" onClick={close}>{t("common.cancel")}</Button>
      <Button tone="primary" icon="upload" busy={sending} disabled={!ready} onClick={() => void submit()}>{t("assistant-lookupload.upload")}</Button>
    </>
  );

  const drop = {
    onDragOver: (e: React.DragEvent) => {
      if (sending || ![...e.dataTransfer.types].includes("Files")) return;
      e.preventDefault();
      setOver(true);
    },
    onDragLeave: () => setOver(false),
    onDrop: (e: React.DragEvent) => {
      setOver(false);
      if (sending || !e.dataTransfer.files.length) return;
      e.preventDefault();
      void pick(e.dataTransfer.files[0]);
    },
  };

  return (
    <Dialog open={open} onClose={close} title={t("assistant-lookupload.title")} width={400} footer={footer}>
      <div className="flex flex-col gap-3">
        <div className="mx-auto w-full max-w-(--crop-box)" {...drop}>
          {picked ? (
            <CropBox image={picked} crop={crop} setCrop={setCrop} disabled={sending} over={over} hintId={`${ids}-hint`} />
          ) : (
            <div
              data-testid="look-upload-drop"
              className={cx(
                "flex aspect-square w-full flex-col items-center justify-center gap-3 rounded-card border border-dashed p-6 text-center transition-colors duration-150",
                over ? "border-accent bg-accent-tint" : "border-line-strong bg-inset",
              )}
            >
              <span className="flex size-14 items-center justify-center rounded-full bg-field text-ink-3">
                <Icon name="image" size={24} />
              </span>
              <Button tone="secondary" icon="upload" busy={phase === "reading"} onClick={() => fileInput.current?.click()}>{t("assistant-lookupload.choose")}</Button>
              <p className="max-w-56 text-[11.5px] leading-relaxed text-ink-3">{t("assistant-lookupload.choose_hint")}</p>
            </div>
          )}
        </div>
        {picked && (
          <div className="mx-auto flex w-full max-w-(--crop-box) flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <Icon name="image" size={12} className="shrink-0 text-ink-3" />
              <input
                type="range"
                aria-label={t("assistant-lookupload.zoom")}
                min={1}
                max={top}
                step="any"
                value={crop.zoom}
                disabled={sending || top <= 1}
                onChange={(e) => {
                  const z = Number(e.target.value);
                  setCrop((c) => zoomCrop(picked.size, c, z, 1));
                }}
                className="min-w-0 flex-1 accent-[var(--accent)] disabled:opacity-50"
              />
              <Icon name="image" size={16} className="shrink-0 text-ink-3" />
            </div>
            <p id={`${ids}-hint`} className="text-center text-[11.5px] leading-snug text-ink-3">{t("assistant-lookupload.crop_hint")}</p>
          </div>
        )}
        <Field id={`${ids}-name`} label={t("assistant-lookupload.name")} count={nameLength} max={NAME_MAX}>
          <input
            id={`${ids}-name`}
            className={inputClass}
            value={name}
            disabled={sending}
            autoComplete="off"
            aria-invalid={nameLength > NAME_MAX || undefined}
            aria-describedby={`${ids}-name-count`}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={enter}
          />
        </Field>
        <Field id={`${ids}-subtitle`} label={t("assistant-lookupload.subtitle")} count={subtitleLength} max={SUBTITLE_MAX}>
          <input
            id={`${ids}-subtitle`}
            className={inputClass}
            value={subtitle}
            disabled={sending}
            autoComplete="off"
            aria-invalid={subtitleLength > SUBTITLE_MAX || undefined}
            aria-describedby={`${ids}-subtitle-count`}
            onChange={(e) => setSubtitle(e.target.value)}
            onKeyDown={enter}
          />
        </Field>
        {error && <p role="alert" className="text-[12px] leading-relaxed break-words text-red">{error}</p>}
        <input
          ref={fileInput}
          type="file"
          accept={UPLOAD_TYPES.join(",")}
          hidden
          data-testid="look-upload-file"
          onChange={(e) => {
            void pick(e.target.files?.[0]);
            e.target.value = ""; // the same file again is a change too
          }}
        />
      </div>
    </Dialog>
  );
}

/** A labelled field with its length against the hub's limit (red past it). */
function Field({ id, label, count, max, children }: { id: string; label: string; count: number; max: number; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="mb-1 flex items-baseline justify-between gap-2">
        <label htmlFor={id} className="truncate text-[12px] font-medium text-ink-2">{label}</label>
        <span id={`${id}-count`} className={cx("shrink-0 text-[11px] tabular-nums", count > max ? "text-red" : "text-ink-3")}>
          {count}/{max}
        </span>
      </div>
      {children}
    </div>
  );
}

/** The box's width in CSS pixels, kept as it changes. */
function useWidth(ref: React.RefObject<HTMLElement | null>): number {
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => setWidth(el.getBoundingClientRect().width);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [ref]);
  return width;
}

/**
 * The square the avatar is cut from, with its circle drawn over it: the crop is drawn into a canvas at the box's own
 * resolution (sharp at any zoom, the first frame of a GIF, exactly what is sent), and moved by dragging, zoomed by the
 * wheel or a pinch, and from the keyboard by the arrow keys and + / −.
 */
function CropBox({ image, crop, setCrop, disabled, over, hintId }: { image: PickedImage; crop: Crop; setCrop: React.Dispatch<React.SetStateAction<Crop>>; disabled: boolean; over: boolean; hintId: string }) {
  const box = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const view = useWidth(box);
  const size = image.size;
  const points = useRef(new Map<number, { x: number; y: number }>());

  useLayoutEffect(() => {
    const c = canvas.current;
    if (!c || view <= 0) return;
    const scale = Math.max(1, Math.min(3, typeof devicePixelRatio === "number" ? devicePixelRatio : 1));
    const px = Math.round(view * scale);
    if (c.width !== px) {
      c.width = px;
      c.height = px;
    }
    const g = c.getContext("2d");
    if (!g) return;
    const r = exportRect(size, clampCrop(size, crop));
    g.imageSmoothingEnabled = true;
    g.imageSmoothingQuality = "high";
    g.clearRect(0, 0, px, px);
    g.drawImage(image.source, r.sx, r.sy, r.side, r.side, 0, 0, px, px);
  }, [image, size, crop, view]);

  // The wheel zooms about the pointer (a trackpad's pinch comes as a wheel with Ctrl). React's own wheel listener is
  // passive, so it could not keep the dialog from scrolling.
  useEffect(() => {
    const el = box.current;
    if (!el || disabled) return;
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      const r = el.getBoundingClientRect();
      if (r.width <= 0) return;
      const delta = e.deltaY * (e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? r.height : 1);
      const f = Math.exp(-delta * (e.ctrlKey ? 0.01 : 0.002));
      setCrop((c) => zoomCrop(size, c, c.zoom * f, r.width, e.clientX - r.left, e.clientY - r.top));
    };
    el.addEventListener("wheel", wheel, { passive: false });
    return () => el.removeEventListener("wheel", wheel);
  }, [size, setCrop, disabled]);

  const down = (e: React.PointerEvent<HTMLDivElement>) => {
    if (disabled || (e.pointerType === "mouse" && e.button !== 0)) return;
    e.currentTarget.setPointerCapture?.(e.pointerId);
    points.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
  };
  const move = (e: React.PointerEvent<HTMLDivElement>) => {
    const all = points.current;
    const was = all.get(e.pointerId);
    if (!was || !box.current) return;
    const now = { x: e.clientX, y: e.clientY };
    const r = box.current.getBoundingClientRect();
    all.set(e.pointerId, now);
    if (r.width <= 0) return;
    const other = [...all.entries()].find(([id]) => id !== e.pointerId)?.[1];
    if (!other) {
      setCrop((c) => panCrop(size, c, now.x - was.x, now.y - was.y, r.width));
      return;
    }
    // a pinch: zoom by how much the fingers spread, about where they were, then follow where they went
    const apart = (a: { x: number; y: number }) => Math.hypot(a.x - other.x, a.y - other.y);
    const before = apart(was);
    if (before <= 0) return;
    const m0 = { x: (was.x + other.x) / 2 - r.left, y: (was.y + other.y) / 2 - r.top };
    const m1 = { x: (now.x + other.x) / 2 - r.left, y: (now.y + other.y) / 2 - r.top };
    setCrop((c) => panCrop(size, zoomCrop(size, c, (c.zoom * apart(now)) / before, r.width, m0.x, m0.y), m1.x - m0.x, m1.y - m0.y, r.width));
  };
  const up = (e: React.PointerEvent<HTMLDivElement>) => {
    points.current.delete(e.pointerId);
  };
  const key = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (disabled || view <= 0) return;
    const step = e.shiftKey ? KEY_STEP * 4 : KEY_STEP;
    const moves: Record<string, [number, number]> = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    if (moves[e.key]) {
      e.preventDefault();
      const [dx, dy] = moves[e.key];
      setCrop((c) => panCrop(size, c, dx, dy, view));
    } else if (e.key === "+" || e.key === "=" || e.key === "-" || e.key === "_") {
      e.preventDefault();
      const f = e.key === "-" || e.key === "_" ? 1 / KEY_ZOOM : KEY_ZOOM;
      setCrop((c) => zoomCrop(size, c, c.zoom * f, view));
    }
  };

  return (
    <div
      ref={box}
      role="group"
      tabIndex={disabled ? -1 : 0}
      aria-label={t("assistant-lookupload.crop_label")}
      aria-describedby={hintId}
      data-testid="look-upload-crop"
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onPointerCancel={up}
      onLostPointerCapture={up}
      onKeyDown={key}
      className={cx(
        "relative aspect-square w-full touch-none overflow-hidden rounded-card bg-field outline-none select-none focus-visible:shadow-[0_0_0_2px_var(--accent)]",
        disabled ? "opacity-80" : "cursor-grab active:cursor-grabbing",
        over && "shadow-[0_0_0_2px_var(--accent)]",
      )}
    >
      <canvas ref={canvas} aria-hidden className="pointer-events-none absolute inset-0 size-full" />
      <span aria-hidden className="crop-ring pointer-events-none absolute inset-0" />
    </div>
  );
}
