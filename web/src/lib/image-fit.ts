// Fitting a picture in a box, the one piece of geometry the image viewer (lib/image-zoom.ts) and the image editor
// (lib/image-edit.ts) share. A module of its own: the viewer and the editor are each a chunk loaded on intent (the
// editor also at idle), and the editor importing these from image-zoom would bring the viewer's zoom and pan with it.
import type { Rect, Size } from "./image-zoom";

/** `size` scaled to fit inside `box`, centered in it (the image editor's layout too, lib/image-edit.ts). */
export function fitIn(size: Size, box: Rect): Rect {
  if (size.width <= 0 || size.height <= 0 || box.width <= 0 || box.height <= 0) return { x: 0, y: 0, width: 0, height: 0 };
  const s = Math.min(box.width / size.width, box.height / size.height);
  const width = size.width * s,
    height = size.height * s;
  return { x: box.x + (box.width - width) / 2, y: box.y + (box.height - height) / 2, width, height };
}

/** The image scaled to fit inside a box at the origin, centered (the image viewer's picture). */
export function fitRect(image: Size, box: Size): Rect {
  return fitIn(image, { x: 0, y: 0, width: box.width, height: box.height });
}
