// The image viewer (ImageViewer.tsx) from a chunk of its own, for the chat's thumbnails (TurnImages, AttachedPhotos,
// Steps): the first screen draws the thumbnails, never the viewer, so its code and words (chat-imageviewer) load on the
// first open, behind OverlayBoundary and an empty Suspense, as the other overlays a click opens (LazyOverlay.tsx). Only
// first-screen code imports this module; the extension view loads the viewer itself (extensions/ViewBody6.tsx),
// and the inbox threads, lazy pages themselves, import the viewer directly, so this does not become a first-screen chunk of its own.
import { lazyOverlay } from "../LazyOverlay";

export const ImageViewer = lazyOverlay(() => import("./ImageViewer").then((m) => m.ImageViewer));
export const ChatImageViewer = lazyOverlay(() => import("./ImageViewer").then((m) => m.ChatImageViewer));
