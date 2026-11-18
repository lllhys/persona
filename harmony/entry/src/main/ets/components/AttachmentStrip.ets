import { IconActions } from '../design/IconActions';
import { ChatAttachment } from '../models/ChatAttachments';
import { Tokens } from '../design/Tokens';
import { Colors } from '../design/Colors';
import { Symbols } from '../design/Symbols';
import { ButtonStyles } from '../design/Controls';
import { tr } from '../design/Strings';

@ComponentV2
export struct AttachmentStrip {
  @Require @Param attachments: ChatAttachment[];
  @Param errorKey: string = '';
  @Param actionsEnabled: boolean = true;
  @Event onRemove: (id: string) => void = () => {};
  @Event onEdit: (id: string) => void = () => {};
  @Event onPreview: (item: ChatAttachment) => void = () => {};
  @Builder imageMenu(item: ChatAttachment) {
    Menu() { MenuItem({ content: tr('edit') }).enabled(this.actionsEnabled).onClick(() => this.onEdit(item.id)) }
  }
  @Builder item(item: ChatAttachment) {
    Row({ space: Tokens.gap }) {
      if (item.kind === 'image') Image(item.previewUri).width(Tokens.attachmentPreview).height(Tokens.attachmentPreview).objectFit(ImageFit.Cover)
        .accessibilityText(item.name).onClick(() => this.onPreview(item)).bindContextMenu(this.imageMenu(item), ResponseType.LongPress)
      else SymbolGlyph(Symbols.document).fontSize(Tokens.symbolFont).fontColor([Colors.link])
      Column({ space: Tokens.fine }) {
        Text(item.name).fontSize(Tokens.meta).fontColor(Colors.ink).maxLines(1).textOverflow({ overflow: TextOverflow.Ellipsis }).width('100%').onClick(() => this.onPreview(item))
        if (item.status === 'unknown' || item.status === 'uploading')
          Text(tr(item.status === 'uploading' ? 'chat_attachment_uploading' : 'chat_attachment_upload_unknown')).fontSize(Tokens.meta).fontColor(Colors.secondary)
      }.layoutWeight(1).alignItems(HorizontalAlign.Start)
      Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('remove')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('remove', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('remove') + ' ' + item.name).bindTips(tr('remove') + ' ' + item.name).enabled(this.actionsEnabled).onClick(() => this.onRemove(item.id))
    }.width('100%').constraintSize({ minHeight: Tokens.touch }).alignItems(VerticalAlign.Center)
  }
  build() {
    if (this.attachments.length > 0 || this.errorKey) Column({ space: Tokens.gap }) {
      if (this.errorKey && !this.attachments.some((item: ChatAttachment) => item.status === 'unknown' || item.status === 'uploading'))
        Text(tr(this.errorKey)).fontSize(Tokens.meta).fontColor($r('app.color.persona_danger')).width('100%')
      ForEach(this.attachments, (item: ChatAttachment) => this.item(item), (item: ChatAttachment) => item.id)
    }.width('100%')
  }
}
