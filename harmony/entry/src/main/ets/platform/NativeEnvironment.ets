import { ConfigurationConstant } from '@kit.AbilityKit';
import type { Configuration } from '@kit.AbilityKit';
import { display, window } from '@kit.ArkUI';

/** Native application configuration, shared by workspace and detached views. */
@ObservedV2
export class NativeEnvironment {
  @Trace dark: boolean = false;
  @Trace fontScale: number = 1;
  @Trace safeBottomPx: number = 0;
  @Trace safeTopPx: number = 0;
  @Trace keyboardHeightPx: number = 0;
  @Trace windowWidthPx: number = 0;
  private windowRef: window.Window | undefined;
  private keyboardChanged: ((height: number) => void) | undefined;
  private sizeChanged: (size: window.Size) => void = (_size: window.Size): void => this.refreshWindow();
  private rectChanged: (event: window.RectChangeOptions) => void = (_event: window.RectChangeOptions): void => this.refreshWindow();
  private avoidChanged: (event: window.AvoidAreaOptions) => void = (_event: window.AvoidAreaOptions): void => this.refreshWindow();

  bindWindow(value: window.Window): void {
    this.unbindWindow(); this.windowRef = value;
    value.on('windowSizeChange', this.sizeChanged);
    value.on('windowRectChange', this.rectChanged);
    value.on('avoidAreaChange', this.avoidChanged);
    const changed = (height: number): void => { if (this.windowRef === value && this.keyboardChanged === changed) this.keyboardHeightPx = Number.isFinite(height) ? Math.max(0, height) : 0; };
    this.keyboardChanged = changed; value.on('keyboardHeightChange', changed);
    this.refreshWindow();
  }
  unbindWindow(): void {
    const value = this.windowRef, keyboard = this.keyboardChanged; this.windowRef = undefined; this.keyboardChanged = undefined;
    this.keyboardHeightPx = 0;
    if (!value) return;
    try { value.off('windowSizeChange', this.sizeChanged); value.off('windowRectChange', this.rectChanged); value.off('avoidAreaChange', this.avoidChanged); if (keyboard) value.off('keyboardHeightChange', keyboard); } catch (_) {}
  }
  private refreshWindow(): void {
    if (!this.windowRef) return;
    try {
      const rect = this.windowRef.getWindowProperties().windowRect;
      const bottom = this.windowRef.getWindowAvoidArea(window.AvoidAreaType.TYPE_NAVIGATION_INDICATOR).bottomRect.height;
      this.safeBottomPx = rect.top + rect.height - bottom;
      this.keyboardHeightPx = this.windowRef.getWindowAvoidArea(window.AvoidAreaType.TYPE_KEYBOARD).bottomRect?.height || 0;
      this.windowWidthPx = Number.isFinite(rect.width) ? rect.width : 0;
      this.safeTopPx = this.windowRef.getWindowAvoidArea(window.AvoidAreaType.TYPE_SYSTEM).topRect?.height || 0;
    } catch (_) {}
  }

  @Trace nativeHoverAvailable: boolean = false;
  private refreshFoldCapability(): void {
    // Some emulator images report foldable but omit the crease region. The native
    // FoldSplitContainer dereferences that region when entering hover mode.
    try {
      const region = display.isFoldable() ? display.getCurrentFoldCreaseRegion() : undefined;
      this.nativeHoverAvailable = !!region && region.creaseRects.length > 0 && region.creaseRects.every((rect: display.Rect): boolean =>
        Number.isFinite(rect.left) && Number.isFinite(rect.top) && Number.isFinite(rect.width) && Number.isFinite(rect.height) && rect.width > 0 && rect.height > 0);
    } catch (_) { this.nativeHoverAvailable = false; }
  }
  update(configuration: Configuration): void {
    if (configuration.colorMode === ConfigurationConstant.ColorMode.COLOR_MODE_DARK) this.dark = true;
    else if (configuration.colorMode === ConfigurationConstant.ColorMode.COLOR_MODE_LIGHT) this.dark = false;
    this.refreshFoldCapability();
    const scale = configuration.fontSizeScale;
    if (typeof scale === 'number' && Number.isFinite(scale) && scale > 0) this.fontScale = scale;
  }
}
export const nativeEnvironment: NativeEnvironment = new NativeEnvironment();
