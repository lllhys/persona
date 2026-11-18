import { IconActions } from '../design/IconActions';
import { Colors } from '../design/Colors';
import { ButtonStyles } from '../design/Controls';
import { common } from '@kit.AbilityKit';
import { ComposeListItemV2, ContentItemV2 } from '@kit.ArkUI';
import { ClientModel } from '../models/ClientModel';
import { Decision } from '../models/Wire';
import { DeviceFeatures, DevicePermissionRow, DeviceVoiceLine, DeviceVoiceRun, deviceFeatures } from '../models/DeviceFeatures';
import { InteractionDecisionForm } from './InteractionDecisionForm';
import { Tokens } from '../design/Tokens';
import { tr } from '../design/Strings';

@ComponentV2
export struct DevicePage {
  @Require @Param model: ClientModel;
  @Param page: string = 'phone';
  @Param active: boolean = true;
  @Event onLeave: (owner: ClientModel) => void = (_owner: ClientModel): void => {};
  @Event onLink: (address: string) => void = () => {};
  @Local device: DeviceFeatures = deviceFeatures;

  private bindOwner(): void {
    if (!this.active) return;
    const host = this.getUIContext().getHostContext();
    if (host) {
      this.device.bind(this.model, host as common.UIAbilityContext, (title: string, message: string,
        approve: () => void, deny: () => void) => {
        this.getUIContext().showAlertDialog({
          title: title,
          message: message,
          primaryButton: { value: tr('cancel'), action: deny },
          secondaryButton: { value: tr('confirm'), action: approve }
        });
      });
    } else {
      this.device.status = 'permission_unavailable';
    }
    this.model.watchDecisions(this.page === 'voice');
  }

  aboutToAppear(): void { this.bindOwner(); }

  @Monitor('active') visibilityChanged(): void {
    if (this.active) this.bindOwner();
    else { this.model.watchDecisions(false); if (this.device.boundTo(this.model)) { this.device.hide(); this.onLeave(this.model); } }
  }

  aboutToDisappear(): void {
    this.model.watchDecisions(false);
    if (this.device.boundTo(this.model)) { this.device.hide(); this.onLeave(this.model); }
  }

  @Monitor('page') pageChanged(): void {
    this.model.watchDecisions(this.page === 'voice');
    if (this.page !== 'voice' && this.device.boundTo(this.model)) this.device.hide();
  }
  @Monitor('model.profile') profileChanged(): void {
    this.device.context(); this.model.watchDecisions(this.page === 'voice');
  }
  @Monitor('model.selected') targetChanged(): void {
    this.device.context(); this.model.watchDecisions(this.page === 'voice');
  }

  @Builder accessRow(row: DevicePermissionRow) {
    ListItem() {
      Row({ space: Tokens.gap }) {
        ComposeListItemV2({ contentItemV2: new ContentItemV2({ primaryText: tr(row.label) }) }).layoutWeight(1)
        Text(tr('permission_' + row.status)).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
        if (row.canRequest()) {
          Button({ type: ButtonType.Circle }) { if (IconActions.busy(row.status === 'denied' ? 'open_settings' : 'allow')) LoadingProgress().width(Tokens.symbol).height(Tokens.symbol).color(Colors.link); else SymbolGlyph(IconActions.symbol(row.status === 'denied' ? 'open_settings' : 'allow')).fontSize(Tokens.symbolFont).fontColor([IconActions.color(row.status === 'denied' ? 'open_settings' : 'allow', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr(row.status === 'denied' ? 'open_settings' : 'allow')).bindTips(tr(row.status === 'denied' ? 'open_settings' : 'allow')).onClick(() => this.device.requestAccess(row.id)).id('device.permission.' + row.id)
        }
      }.width('100%')
    }.width('100%')
  }

  @Builder voiceRunRow(run: DeviceVoiceRun) {
    ListItem() {
      Column({ space: Tokens.fine }) {
        Row({ space: Tokens.gap }) {
          Text(run.task || tr('voice')).fontSize(Tokens.body).layoutWeight(1)
          Text(tr('voice_run_' + run.state)).fontSize(Tokens.meta)
            .fontColor($r('sys.color.font_secondary'))
        }.width('100%')
        if (run.code) Text(run.code).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary')).width('100%')
        if (run.message) Text(run.message).fontSize(Tokens.meta).width('100%')
        if (run.draft) {
          Text(tr('voice_run_unsent_draft')).fontSize(Tokens.meta)
            .fontColor($r('sys.color.font_secondary')).width('100%')
          Text(run.draft).fontSize(Tokens.meta).width('100%')
        }
      }.alignItems(HorizontalAlign.Start)
        .padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset }).width('100%')
    }.id('voice.run.' + run.id)
  }

  @Builder pendingDecision(request: Decision) {
    ListItem() {
      InteractionDecisionForm({ model: this.model, request: request, onLink: (address: string) => this.onLink(address) })
    }.id('voice.decision.' + request.id)
  }

  @Builder phoneContent() {
    Column() {
      List() {
        ListItem() {
          ComposeListItemV2({ contentItemV2: new ContentItemV2({ primaryText: tr('status'),
            secondaryText: tr(this.device.status === 'connected' ? 'phone_tools_connected' : 'phone_tools_offline') +
              ' · ' + String(this.device.toolCount) + ' ' + tr('phone_tools_registered') }) })
        }
        ForEach(this.device.permissionRows(), (row: DevicePermissionRow) => {
          this.accessRow(row)
        }, (row: DevicePermissionRow) => row.id)
      }.width('100%').height('100%').padding({ bottom: Tokens.listTail })
    }.width('100%').height('100%')
  }

  @Builder voiceContent() {
    List() {
      ListItem() {
        Column({ space: Tokens.gap }) {
          Text(tr(this.device.voiceStatus)).fontSize(Tokens.body).width('100%')
          if (this.device.voiceActive) {
            Text((this.model.selected.title || this.model.selected.id)).fontSize(Tokens.meta)
              .fontColor($r('sys.color.font_secondary')).width('100%')
            Row({ space: Tokens.gap }) {
              Button({ type: ButtonType.Circle }) { if (IconActions.busy(this.device.voiceMuted ? 'voice_unmute' : 'voice_mute')) LoadingProgress().width(Tokens.symbol).height(Tokens.symbol).color(Colors.link); else SymbolGlyph(IconActions.symbol(this.device.voiceMuted ? 'voice_unmute' : 'voice_mute')).fontSize(Tokens.symbolFont).fontColor([IconActions.color(this.device.voiceMuted ? 'voice_unmute' : 'voice_mute', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr(this.device.voiceMuted ? 'voice_unmute' : 'voice_mute')).bindTips(tr(this.device.voiceMuted ? 'voice_unmute' : 'voice_mute')).onClick(() => this.device.setVoiceMuted(!this.device.voiceMuted)).id('voice.mute')
              Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('voice_interrupt')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('voice_interrupt', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('voice_interrupt')).bindTips(tr('voice_interrupt')).onClick(() => this.device.interrupt()).id('voice.interrupt')
              Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('voice_end')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('voice_end', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('voice_end')).bindTips(tr('voice_end'))
                .onClick(() => this.device.endVoice().catch(() => undefined)).id('voice.end')
            }.width('100%')
          } else {
            Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('voice_start')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('voice_start', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('voice_start')).bindTips(tr('voice_start')).enabled(this.device.canStartVoice())
              .onClick(() => this.device.startVoice().catch(() => undefined)).id('voice.start')
          }
        }.alignItems(HorizontalAlign.Start).padding(Tokens.page).width('100%')
      }
      if (this.device.voiceToolLabel) ListItem() { Text(this.device.voiceToolLabel).fontSize(Tokens.meta).width('100%').padding(Tokens.page) }
      if (this.device.voiceNotice) {
        ListItem() {
          Text(this.device.voiceNotice).fontSize(Tokens.meta).width('100%')
            .padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset })
        }.id('voice.notice')
      }
      if (this.model.decision) this.pendingDecision(this.model.decision)
      ForEach(this.device.voiceRuns, (run: DeviceVoiceRun) => {
        this.voiceRunRow(run)
      }, (run: DeviceVoiceRun) => run.id)
      ForEach(this.device.voiceCaptions, (line: DeviceVoiceLine) => {
        ListItem() {
          Column({ space: Tokens.fine }) {
            Text(tr(line.role === 'user' ? 'voice_you' : 'voice_assistant'))
              .fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary')).width('100%')
            Text(line.text).fontSize(Tokens.body).width('100%')
          }.alignItems(HorizontalAlign.Start).padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset }).width('100%')
        }.id('voice.caption.' + line.id)
      }, (line: DeviceVoiceLine) => line.id)
      if (this.device.voiceActive && !this.device.voiceCaptions.length) {
        ListItem() { Text(tr('voice_transcript_empty')).fontSize(Tokens.meta).padding(Tokens.page).width('100%') }
      }
    }.width('100%').height('100%').padding({ bottom: Tokens.listTail })
  }

  build() {
    Column() {
      if (this.page === 'voice') this.voiceContent()
      else this.phoneContent()
    }.width('100%').height('100%')
  }
}
