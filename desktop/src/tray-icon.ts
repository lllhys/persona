/** The tray's file in dist/assets (scripts/assets/make_icon.py makes them from the app icon's helper): macOS's menu bar
 *  template (Electron takes a name ending in Template as a template image, which the menu bar colours for light, dark
 *  and highlighted, and finds its @2x beside it), Windows' colour helper in an .ico of the notification area's sizes,
 *  and a colour PNG elsewhere (development runs). */
export function trayIconFile(platform: NodeJS.Platform): string {
  if (platform === "darwin") return "trayTemplate.png";
  if (platform === "win32") return "tray.ico";
  return "tray.png";
}
