import { Theme } from '@kit.ArkUI';
import { Colors } from './Colors';

/** Explicit attributes use resolved theme tokens rather than global system resource IDs. */
@ComponentV2
export struct ThemeScope {
  @BuilderParam content: () => void;
  onWillApplyTheme(theme: Theme): void {
    Colors.action = theme.colors.compBackgroundEmphasize;
    Colors.onAction = theme.colors.fontOnPrimary;
    Colors.link = theme.colors.fontEmphasize;
  }
  build() { this.content() }
}
