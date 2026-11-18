// Markdown images on a third-party host stay unloaded until the reader taps them (#8162): opening a conversation must
// not send the reader's address to a host the message names. The hub's own images load inline as before. Only the
// consent gate for an off-hub image is lazy: same-origin and relative attachments draw immediately.
import { lazy, Suspense, type ComponentProps } from "react";
import { isOwnImageURL } from "../lib/links";

const ExternalImageGate = lazy(() => import("./ExternalImageGate").then((m) => ({ default: m.ExternalImageGate })));

export function ExternalImage({ node: _node, src = "", alt = "", ...rest }: ComponentProps<"img"> & { node?: unknown }) {
  if (isOwnImageURL(src)) return <img src={src} alt={alt} {...rest} />;
  return <Suspense fallback={null}><ExternalImageGate src={src} alt={alt} {...rest} /></Suspense>;
}
