export interface ComposerEditorRect { left: number; top: number; width: number; height: number; }
export interface ComposerPopupViewport { width: number; top: number; bottom: number; }
export interface ComposerPopupPolicy { ordinaryWidth: number; maximumWidth: number; margin: number; }
export interface ComposerPopupLayout { width: number; maximumHeight: number; side: string; }

/** Content bounds leave native bubble padding/arrow room; ArkUI owns final placement and avoidance. */
export function composerPopupLayout(editor: ComposerEditorRect, viewport: ComposerPopupViewport,
  rowHeight: number, naturalHeight: number, fontScale: number, policy: ComposerPopupPolicy): ComposerPopupLayout {
  const frame = policy.margin * 2;
  const fullHeight = Math.max(0, viewport.bottom - viewport.top - frame);
  const above = Math.max(0, editor.top - viewport.top - frame);
  const below = Math.max(0, viewport.bottom - editor.top - editor.height - frame);
  const left = Math.max(0, editor.left - frame);
  const right = Math.max(0, viewport.width - editor.left - editor.width - frame);
  const wide = Math.max(0, Math.min(fontScale >= 1.5 ? policy.maximumWidth : policy.ordinaryWidth, viewport.width - frame));
  let side = 'above', width = wide, height = above;
  if (above < rowHeight && left >= policy.ordinaryWidth && fullHeight >= rowHeight) {
    side = 'left'; width = policy.ordinaryWidth; height = fullHeight;
  } else if (above < rowHeight && right >= policy.ordinaryWidth && fullHeight >= rowHeight) {
    side = 'right'; width = policy.ordinaryWidth; height = fullHeight;
  } else if (above < rowHeight && below > above) { side = 'below'; height = below; }
  return { side, width, maximumHeight: Math.min(Math.max(0, naturalHeight), height) };
}
