import { DeviceLook } from '../models/DeviceLook';
import { deviceLookAccentText } from '../core/DeviceLook';

/** Scope tokens follow the native application configuration, including explicit appearance overrides. */
export function nativeLookTheme(look: DeviceLook, style: string, dark: boolean = false): CustomTheme {
  const palette = look.palette(style);
  const current = dark ? palette.dark : palette.light;
  return {
    colors: { brand: current.brand, fontEmphasize: current.emphasize, iconEmphasize: current.emphasize,
      compBackgroundEmphasize: current.brand, fontOnPrimary: deviceLookAccentText(current.brand), iconOnPrimary: deviceLookAccentText(current.brand) },
    darkColors: { brand: palette.dark.brand, fontEmphasize: palette.dark.emphasize, iconEmphasize: palette.dark.emphasize,
      compBackgroundEmphasize: palette.dark.brand, fontOnPrimary: deviceLookAccentText(palette.dark.brand), iconOnPrimary: deviceLookAccentText(palette.dark.brand) }
  };
}

export function nativeMessageTheme(look: DeviceLook, style: string, own: boolean, dark: boolean = false): CustomTheme {
  if (!own) return {};
  const palette = look.palette(style);
  const current = dark ? palette.dark : palette.light;
  return { colors: { compBackgroundPrimary: current.userBubble, fontPrimary: current.userBubbleText,
    compBackgroundSecondary: current.userBubble, compBackgroundTertiary: current.userBubble,
    fontSecondary: current.userBubbleText, fontEmphasize: current.userBubbleText },
    darkColors: { compBackgroundPrimary: palette.dark.userBubble, fontPrimary: palette.dark.userBubbleText,
      compBackgroundSecondary: palette.dark.userBubble, compBackgroundTertiary: palette.dark.userBubble,
      fontSecondary: palette.dark.userBubbleText, fontEmphasize: palette.dark.userBubbleText } };
}
