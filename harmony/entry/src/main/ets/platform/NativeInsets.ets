import { UIContext } from '@kit.ArkUI';

/** Only the outstanding native Sheet overlap is added, in the same screen coordinate space. */
export function nativeSheetInset(context: UIContext, id: string, safeBottomPx: number): number {
  if (safeBottomPx <= 0) return 0;
  const node = context.getFrameNodeById(id);
  if (!node) return 0;
  const position = node.getPositionToScreenWithTransform();
  const size = node.getMeasuredSize();
  return context.px2vp(Math.max(0, Math.round(context.vp2px(position.y) + size.height - safeBottomPx)));
}
