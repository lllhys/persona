// Pictures in replies, drawn to fit the text (#9475; iOS #9474 keeps the same rules and numbers in PersonaKit's
// ReplyImages / ReplyImageLayout; docs/design/DESIGN_SYSTEM.md "Pictures in replies"). A web picture on a line of its
// own is a block picture, however it sits in its paragraph (replies often put one right under a line of text); block
// pictures with only line breaks or blank lines between them are one run (ui/reply-images-plugin.ts groups them).
// One picture is a fixed height (its box is its final height before it arrives; only its width changes), as wide as
// its shape from 3:4 up to the column, never larger than itself. Two are squares side by side; three or more squares
// three to a row (four as 2 × 2), at most two rows, the sixth counting those not drawn. Pictures load through the
// hub's remote image route (docs/protocol/protocol.md "Remote images (#9473)") at the size drawn. The rules are in
// reply-image-layout.ts (the runs' own chunk); this is what the first screen needs.
import { BASE } from "./base";

/** The hub feature that says it has the route. */
export const REMOTE_IMAGES_FEATURE = "remote_images";
/** The size the image viewer asks for, and a picture inside a line of text. */
export const VIEWER_SIZE = 2048;
export const INLINE_SIZE = 256;

/** Whether `src` is a picture on the web (absolute http or https), the only ones drawn this way. */
export function isWebImage(src: string | undefined | null): boolean {
  return !!src && /^https?:\/\/[^/\s]/i.test(src.trim());
}

/** The hub's remote image route for `src` at `size` (the token in the query, as every picture the page loads from the
 *  hub: an <img> sends no header). `encodeURIComponent` escapes `&`, `=`, `#`, `+` and spaces. */
export function remoteImageURL(src: string, size: number, token: string): string {
  return `${BASE}/api/v1/remote-image?url=${encodeURIComponent(src)}&size=${size}&token=${encodeURIComponent(token)}`;
}
