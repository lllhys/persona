import { IconActions } from '../design/IconActions';
import { Colors } from '../design/Colors';
import { ButtonStyles } from '../design/Controls';
import { KnowledgeRecording } from '../models/KnowledgeRecording';
import type { KnowledgeRecordingEntry } from '../models/KnowledgeRecording';
import { Tokens } from '../design/Tokens';
import { tr } from '../design/Strings';

/** Capture controls for the local recording owned by the current Knowledge Hub/manager. */
@ComponentV2
export struct KnowledgeRecordingView {
  @Require @Param model: KnowledgeRecording;
  @Event onDeleteLocal: () => void = () => {};

  aboutToDisappear(): void {
    this.model.stopForBackground().catch(() => undefined);
  }

  private statusKey(): string {
    if (this.model.captureState === 'requesting') return 'knowledge_recording_requesting';
    if (this.model.captureState === 'recording') return 'knowledge_recording_active';
    if (this.model.transcriptionState === 'transcribing') return 'knowledge_recording_transcribing';
    if (this.model.transcriptionState === 'complete') {
      if (this.model.uploadState === 'uploading') return 'knowledge_recording_uploading';
      if (this.model.uploadState === 'stored') return 'knowledge_recording_uploaded';
      if (this.model.uploadState === 'unknown') return 'knowledge_recording_upload_unknown';
      if (this.model.uploadState === 'failed') return 'knowledge_recording_upload_failed';
      if (this.model.uploadState === 'deleted') return 'knowledge_recording_deleted';
      if (this.model.uploadState === 'stale') return 'knowledge_recording_stale';
      if (this.model.uploadState === 'expired') return 'knowledge_recording_expired';
      return 'knowledge_recording_transcribed';
    }
    if (this.model.captureState === 'stopped') return 'knowledge_recording_stopped';
    return 'knowledge_recording_capture_ready';
  }

  private transcribeLabel(): string {
    return this.model.transcriptionState === 'failed' ? tr('knowledge_recording_retry_transcribe') : tr('knowledge_recording_transcribe');
  }

  private confirmTranscription(): void {
    const owner = this.model, entry = owner.entry;
    if (!entry || !owner.canTranscribe()) return;
    this.getUIContext().showAlertDialog({ title: this.transcribeLabel(), message: tr('knowledge_recording_save_notice'),
      primaryButton: { value: tr('cancel'), action: () => {} },
      secondaryButton: { value: tr('confirm'), action: () => {
        if (this.model === owner && owner.entry === entry && owner.canTranscribe()) owner.transcribeAndUpload().catch(() => false);
      } } });
  }

  private recordingOptions(): SelectOption[] {
    return this.model.entries.map((entry: KnowledgeRecordingEntry): SelectOption => ({
      value: entry.startedAt.replace('T', ' ').slice(0, 19)
    }));
  }

  private recordingIndex(): number {
    const selectedId = this.model.entry?.id || '';
    const index = this.model.entries.findIndex((entry: KnowledgeRecordingEntry) => entry.id === selectedId);
    return index >= 0 ? index : 0;
  }

  build() {
    Column({ space: Tokens.gap }) {
      Row({ space: Tokens.gap }) {
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('knowledge_recording_start')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('knowledge_recording_start', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('knowledge_recording_start')).bindTips(tr('knowledge_recording_start')).enabled(this.model.canStart())
          .onClick(() => this.model.start().catch(() => false)).id('knowledge.recording.start')
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('knowledge_recording_stop')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('knowledge_recording_stop', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('knowledge_recording_stop')).bindTips(tr('knowledge_recording_stop')).enabled(this.model.canStop())
          .onClick(() => this.model.stop().catch(() => false)).id('knowledge.recording.stop')
      }.width('100%')

      if (this.model.entries.length > 1) {
        Select(this.recordingOptions()).selected(this.recordingIndex())
          .enabled(this.model.canSelectEntry()).onSelect((index: number) => {
            const selected = this.model.entries[index];
            if (selected) this.model.selectEntry(selected.id);
          }).width('100%').id('knowledge.recording.select')
      }

      Text(tr(this.statusKey())).fontSize(Tokens.meta).width('100%').id('knowledge.recording.status')
      if (this.model.entry && this.model.entry.duration > 0) {
        Text(String(Math.floor(this.model.entry.duration)) + 's').fontSize(Tokens.meta)
          .fontColor($r('sys.color.font_secondary')).width('100%').id('knowledge.recording.duration')
      }
      if (this.model.error) Text(tr(this.model.error)).fontSize(Tokens.meta)
        .fontColor($r('app.color.persona_danger')).width('100%').id('knowledge.recording.error')

      if (this.model.captureState === 'stopped' && this.model.transcriptionState !== 'complete') {
        Button({ type: ButtonType.Circle }) { if (IconActions.busy(this.model.transcriptionState === 'failed' ? 'knowledge_recording_retry_transcribe' : 'knowledge_recording_transcribe')) LoadingProgress().width(Tokens.symbol).height(Tokens.symbol).color(Colors.onAction); else SymbolGlyph(IconActions.symbol(this.model.transcriptionState === 'failed' ? 'knowledge_recording_retry_transcribe' : 'knowledge_recording_transcribe')).fontSize(Tokens.symbolFont).fontColor([IconActions.color(this.model.transcriptionState === 'failed' ? 'knowledge_recording_retry_transcribe' : 'knowledge_recording_transcribe', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(this.transcribeLabel()).bindTips(this.transcribeLabel()).enabled(this.model.canTranscribe())
          .onClick(() => this.confirmTranscription()).id('knowledge.recording.transcribe')
      }
      if (this.model.transcriptionState === 'transcribing') {
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('cancel')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('cancel', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('cancel')).bindTips(tr('cancel')).onClick(() => this.model.cancelTranscription()).id('knowledge.recording.cancel')
      }
      if (this.model.canRetryUpload()) {
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('knowledge_recording_retry_upload')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('knowledge_recording_retry_upload', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('knowledge_recording_retry_upload')).bindTips(tr('knowledge_recording_retry_upload'))
          .onClick(() => this.model.retryUpload().catch(() => false)).id('knowledge.recording.retry_upload')
      }
      if (this.model.entry && this.model.entry.transcript) {
        Text(this.model.entry.transcript).fontSize(Tokens.body).width('100%').id('knowledge.recording.transcript')
      }
      if (this.model.canDeleteLocal()) {
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('knowledge_recording_delete_local')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('knowledge_recording_delete_local', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('knowledge_recording_delete_local')).bindTips(tr('knowledge_recording_delete_local'))
          .onClick(() => this.onDeleteLocal()).id('knowledge.recording.delete_local')
      }
    }.padding(Tokens.inset).width('100%').alignItems(HorizontalAlign.Start)
  }
}
