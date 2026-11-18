// Semantic native colors; appearance resources own the light and dark variants.
@ObservedV2
class NativeColors {
  readonly canvas: ResourceColor = $r('app.color.persona_canvas');
  readonly grouped: ResourceColor = $r('app.color.persona_grouped');
  readonly ink: ResourceColor = $r('app.color.persona_ink');
  readonly secondary: ResourceColor = $r('app.color.persona_secondary');
  @Trace action: ResourceColor = $r('sys.color.comp_background_emphasize');
  @Trace onAction: ResourceColor = $r('sys.color.font_on_primary');
  @Trace link: ResourceColor = $r('sys.color.font_emphasize');
  readonly control: ResourceColor = $r('app.color.persona_control');
  readonly selection: ResourceColor = $r('app.color.persona_selection');
  readonly question: ResourceColor = $r('app.color.persona_question');
  readonly needs: ResourceColor = $r('app.color.persona_needs');
  readonly working: ResourceColor = $r('app.color.persona_working');
  readonly unread: ResourceColor = $r('app.color.persona_unread');
  readonly positive: ResourceColor = $r('app.color.persona_positive');
  readonly divider: ResourceColor = $r('app.color.persona_divider');
  readonly danger: ResourceColor = $r('app.color.persona_danger');
  readonly ai: ResourceColor = $r('app.color.persona_ai');
  readonly teal: ResourceColor = $r('app.color.persona_teal');
  readonly subtle: ResourceColor = $r('app.color.persona_subtle');
}

export const Colors: NativeColors = new NativeColors();
