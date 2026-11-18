import { IconActions } from '../design/IconActions';
import { LengthMetrics } from '@kit.ArkUI';
import { DeviceLook, DeviceLookAvatar } from '../models/DeviceLook';
import { Colors } from '../design/Colors';
import { ButtonStyles, SelectStyles } from '../design/Controls';
import { Tokens } from '../design/Tokens';
import { tr } from '../design/Strings';

@ComponentV2
export struct DeviceLookPicker {
  @Require @Param model: DeviceLook;
  @Param active: boolean = true;
  @Event onAvatarSelected: (key: string) => void = (_key: string): void => {};
  @Event onAccentSelected: (id: string) => void = (_id: string): void => {};
  @Event onRefresh: () => void = (): void => {};

  private accentLabel(id: string): string { return tr('device_look_accent_' + id); }
  private accentChoices(): SelectOption[] { return this.model.accentOptions().map((id: string): SelectOption => ({ value: this.accentLabel(id) })); }
  private accentIndex(): number { return Math.max(0, this.model.accentOptions().indexOf(this.model.accentId)); }
  private avatarLabel(avatar: DeviceLookAvatar): string {
    const duplicate = this.model.avatars.some((item: DeviceLookAvatar) => item.name === avatar.name && item.optionKey() !== avatar.optionKey());
    return duplicate && avatar.ownerName ? avatar.name + ' · ' + avatar.ownerName : avatar.name;
  }
  private selectAvatar(key: string): void {
    if (this.active && this.model.ready && !this.model.busy && this.model.avatarKey !== key) this.onAvatarSelected(key);
  }
  @Builder avatarHeader() {
    Text(tr('device_look_avatar_heading')).fontSize(Tokens.title).fontWeight(FontWeight.Medium)
      .padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset }).width('100%')
  }
  @Builder avatarImage(avatar: DeviceLookAvatar) {
    Image(avatar.isBuiltin() ? $r('app.media.foreground') : avatar.imageUri()).sourceSize({ width: 256, height: 256 })
      .width(Tokens.avatarChoice).height(Tokens.avatarChoice).objectFit(ImageFit.Cover).borderRadius(Tokens.avatarChoice / 2).accessibilityLevel('no')
  }
  build() {
    ListItemGroup({ header: (): void => this.avatarHeader() }) {
      ForEach(this.model.avatars, (avatar: DeviceLookAvatar) => {
        ListItem() {
          Row({ space: Tokens.inset }) {
            this.avatarImage(avatar)
            Text(this.avatarLabel(avatar)).fontSize(Tokens.body).layoutWeight(1)
            Radio({ value: avatar.optionKey(), group: 'persona-device-avatars' }).checked(this.model.avatarKey === avatar.optionKey())
              .radioStyle({ checkedBackgroundColor: Colors.action, uncheckedBorderColor: Colors.secondary, indicatorColor: Colors.onAction })
              .enabled(this.active && this.model.ready && !this.model.busy).accessibilityText(this.avatarLabel(avatar))
              .onChange((checked: boolean) => { if (checked) this.selectAvatar(avatar.optionKey()); })
          }.width('100%').constraintSize({ minHeight: Tokens.touch }).padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset })
        }.onClick(() => this.selectAvatar(avatar.optionKey())).id('device-look-avatar-' + avatar.optionKey())
      }, (avatar: DeviceLookAvatar) => avatar.optionKey())
      ListItem() {
        Flex({ wrap: FlexWrap.Wrap, justifyContent: FlexAlign.SpaceBetween, alignItems: ItemAlign.Center, space: { main: LengthMetrics.vp(Tokens.inset), cross: LengthMetrics.vp(Tokens.gap) } }) {
          Text(tr('device_look_accent_heading')).fontSize(Tokens.body).flexShrink(0).constraintSize({ maxWidth: '100%' })
          Select(this.accentChoices()).attributeModifier(SelectStyles.inline).selected(this.accentIndex()).value(this.accentLabel(this.model.accentId)).enabled(this.active && this.model.ready)
            .onSelect((index: number) => { const id = this.model.accentOptions()[index]; if (this.active && id) this.onAccentSelected(id); })
            .constraintSize({ minHeight: Tokens.touch, maxWidth: '100%' }).id('device-look-accent')
        }.width('100%').constraintSize({ minHeight: Tokens.touch }).padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset })
      }
      if (this.model.busy || !this.model.ready) ListItem() { LoadingProgress().width(Tokens.touch).height(Tokens.touch) }
      if (this.model.error) ListItem() {
        Row({ space: Tokens.gap }) {
          Text(tr(this.model.error)).fontSize(Tokens.meta).fontColor($r('app.color.persona_danger')).layoutWeight(1)
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('retry')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('retry', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('retry')).bindTips(tr('retry')).enabled(this.active).onClick(() => this.onRefresh())
        }.padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset }).width('100%')
      }
    }
  }
}
