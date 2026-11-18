import { useLayoutEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { useCloseOnBack } from "../lib/back-close";
import { IconButton } from "./atoms";
import { NARROW, useMedia } from "./media-query";
import { useEscapeClose, useModalFocus } from "./overlay";

type Point = { x: number; y: number };
type View = Point & { zoom: number };
type Gesture = { center: Point; distance: number };

function gesture(points: Map<number, Point>): Gesture | undefined {
  const [a, b] = points.values();
  if (!a) return;
  if (!b) return { center: a, distance: 0 };
  return {
    center: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 },
    distance: Math.hypot(a.x - b.x, a.y - b.y),
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

const STEP = 1.25;
const KEY_PAN: Readonly<Record<string, Point>> = {
  ArrowLeft: { x: 48, y: 0 },
  ArrowRight: { x: -48, y: 0 },
  ArrowUp: { x: 0, y: 48 },
  ArrowDown: { x: 0, y: -48 },
};

export function MermaidViewer({ svg, onClose }: { svg: string; onClose: () => void }) {
  const dialog = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const drawing = useRef<HTMLDivElement>(null);
  const controls = useRef<{ fit: () => void; zoom: (factor: number) => void } | null>(null);
  useModalFocus(dialog);
  useCloseOnBack(true, onClose, useMedia(NARROW));
  useEscapeClose(true, onClose);

  useLayoutEffect(() => {
    const area = stage.current!;
    const layer = drawing.current!;
    const graphic = layer.querySelector("svg");
    if (!graphic) return;
    const box = graphic.viewBox?.baseVal;
    const width = box?.width || graphic.getBoundingClientRect().width || 1;
    const height = box?.height || graphic.getBoundingClientRect().height || 1;
    const points = new Map<number, Point>();
    let viewport = { x: 0, y: 0 };
    let fitZoom = 1;
    let view: View = { x: 0, y: 0, zoom: 1 };
    // Mermaid may put an intrinsic max-width directly on the SVG. Size the SVG via its wrapper instead.
    layer.style.width = `${width}px`;
    layer.style.height = `${height}px`;

    function setView({ x, y, zoom }: View) {
      view = { x, y, zoom };
      layer.style.transform = `translate(-50%, -50%) translate(${view.x}px, ${view.y}px) scale(${zoom})`;
    }

    function panBy({ x, y }: Point) {
      const maxX = Math.max(0, (width * view.zoom - viewport.x) / 2);
      const maxY = Math.max(0, (height * view.zoom - viewport.y) / 2);
      // Zoom can leave the drawing off-center. Let pan move it back without snapping or going farther out.
      setView({
        x: clamp(view.x + x, Math.min(view.x, -maxX), Math.max(view.x, maxX)),
        y: clamp(view.y + y, Math.min(view.y, -maxY), Math.max(view.y, maxY)),
        zoom: view.zoom,
      });
    }

    function fit() {
      setView({ x: 0, y: 0, zoom: fitZoom });
    }

    function resize() {
      const x = area.clientWidth;
      const y = area.clientHeight;
      if (!x || !y || (x === viewport.x && y === viewport.y)) return;
      const firstLayout = viewport.x === 0;
      viewport = { x, y };
      const inset = 16;
      fitZoom = Math.min(Math.max(1, viewport.x - 2 * inset) / width, Math.max(1, viewport.y - 2 * inset) / height);
      if (firstLayout) {
        fit();
        return;
      }
      // Keep the established view and enough of the drawing in reach after a large size change.
      const reach = (size: number, available: number) => (size + available) / 2 - Math.min(32, size, available);
      const maxX = reach(width * view.zoom, x);
      const maxY = reach(height * view.zoom, y);
      setView({ x: clamp(view.x, -maxX, maxX), y: clamp(view.y, -maxY, maxY), zoom: view.zoom });
    }

    function zoomFrom(from: Point, to: Point, factor: number) {
      const zoom = clamp(view.zoom * factor, fitZoom / 4, fitZoom * 16);
      if (zoom === view.zoom) {
        if (from.x !== to.x || from.y !== to.y) panBy({ x: to.x - from.x, y: to.y - from.y });
        return;
      }
      const ratio = zoom / view.zoom;
      // Keep the drawing point under the gesture even on an axis that still fits the viewport.
      setView({ x: to.x - (from.x - view.x) * ratio, y: to.y - (from.y - view.y) * ratio, zoom });
    }

    function local(e: MouseEvent): Point {
      const bounds = area.getBoundingClientRect();
      return { x: e.clientX - bounds.left - bounds.width / 2, y: e.clientY - bounds.top - bounds.height / 2 };
    }

    function wheel(e: WheelEvent) {
      if (e.ctrlKey || e.metaKey) return;
      e.preventDefault();
      const point = local(e);
      const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? viewport.y : 1;
      zoomFrom(point, point, Math.exp(clamp(-e.deltaY * unit * 0.004, -1, 1)));
    }

    function down(e: PointerEvent) {
      if (e.button !== 0 || points.size >= 2) return;
      e.preventDefault();
      area.setPointerCapture(e.pointerId);
      points.set(e.pointerId, local(e));
    }

    function move(e: PointerEvent) {
      if (!points.has(e.pointerId)) return;
      const before = gesture(points)!;
      points.set(e.pointerId, local(e));
      const after = gesture(points)!;
      const factor = before.distance > 0 && after.distance > 0 ? after.distance / before.distance : 1;
      zoomFrom(before.center, after.center, factor);
    }

    function end(e: PointerEvent) {
      points.delete(e.pointerId);
    }

    function key(e: KeyboardEvent) {
      if (e.metaKey || e.ctrlKey || e.altKey || !Object.hasOwn(KEY_PAN, e.key)) return;
      const delta = KEY_PAN[e.key];
      e.preventDefault();
      panBy(delta);
    }

    const center = { x: 0, y: 0 };
    controls.current = { fit, zoom: (factor) => zoomFrom(center, center, factor) };
    resize();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(resize);
    observer?.observe(area);
    area.addEventListener("wheel", wheel, { passive: false });
    area.addEventListener("pointerdown", down);
    area.addEventListener("pointermove", move);
    area.addEventListener("pointerup", end);
    area.addEventListener("pointercancel", end);
    area.addEventListener("lostpointercapture", end);
    const modal = dialog.current!;
    modal.addEventListener("keydown", key);
    return () => {
      observer?.disconnect();
      controls.current = null;
      area.removeEventListener("wheel", wheel);
      area.removeEventListener("pointerdown", down);
      area.removeEventListener("pointermove", move);
      area.removeEventListener("pointerup", end);
      area.removeEventListener("pointercancel", end);
      area.removeEventListener("lostpointercapture", end);
      modal.removeEventListener("keydown", key);
    };
  }, [svg]);

  useLayoutEffect(() => {
    const modal = dialog.current!;
    // A fixed overlay is outside the chat scroll container. Prevent scroll over its chrome and leave browser zoom there.
    const stopScroll = (event: WheelEvent) => {
      if (!event.ctrlKey && !event.metaKey) event.preventDefault();
    };
    modal.addEventListener("wheel", stopScroll, { passive: false });
    return () => modal.removeEventListener("wheel", stopScroll);
  }, []);

  return createPortal(
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-label={t("ui-mermaid.diagram")}
      tabIndex={-1}
      className="fixed inset-0 z-50 flex bg-black/55 p-2 outline-none sm:p-6"
      onClick={(e) => {
        e.stopPropagation();
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden rounded-[12px] border border-line bg-surface shadow-lg">
        <div className="flex shrink-0 justify-end gap-2 border-b border-line p-2">
          <IconButton icon="minus" label={t("ui-mermaid.zoom_out")} size={44} onClick={() => controls.current?.zoom(1 / STEP)} />
          <IconButton icon="plus" label={t("ui-mermaid.zoom_in")} size={44} onClick={() => controls.current?.zoom(STEP)} />
          <IconButton icon="external" label={t("ui-mermaid.fit")} size={44} onClick={() => controls.current?.fit()} />
          <IconButton icon="x" label={t("ui-atoms.close")} size={44} onClick={onClose} />
        </div>
        <div ref={stage} data-mermaid-stage="" className="relative min-h-0 flex-1 touch-none cursor-grab overflow-hidden overscroll-none select-none active:cursor-grabbing">
          <div
            ref={drawing}
            className="pointer-events-none absolute top-1/2 left-1/2 [&>svg]:block [&>svg]:h-full [&>svg]:w-full [&>svg]:max-h-none [&>svg]:max-w-none!"
            dangerouslySetInnerHTML={{ __html: svg }}
          />
        </div>
      </div>
    </div>,
    document.body,
  );
}
