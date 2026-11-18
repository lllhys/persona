import { IconActions } from '../design/IconActions';
import { LengthMetrics } from '@kit.ArkUI';
import { QuestionDraft } from '../models/QuestionWorkspace';
import { Symbols } from '../design/Symbols';
import { ClientModel } from '../models/ClientModel';
import { Decision, DecisionField, Wire } from '../models/Wire';
import {
  InteractionChoice, InteractionSchemaField, InteractionValidation,
  interactionCanDefer, interactionCanSubmit, interactionElicitationDefaults, interactionElicitationMessage,
  interactionElicitationMode, interactionElicitationSupported, interactionElicitationUrl, interactionHistoryResolution,
  interactionPermissionFieldPlaceholder, interactionQuestionOptions,
  interactionSchemaFields, interactionScreenCanCancel, interactionScreenMulti, interactionScreenOptions,
  interactionScreenQuestion, interactionScreenSubmitLabel, validateInteractionElicitation
} from '../models/InteractionDecision';
import { Tokens } from '../design/Tokens';
import { Colors } from '../design/Colors';
import { ButtonStyles } from '../design/Controls';
import { tr } from '../design/Strings';

@ComponentV2
export struct InteractionDecisionForm {
  @Require @Param model: ClientModel;
  @Require @Param request: Decision;
  @Param readOnly: boolean = false;
  @Param pinFooter: boolean = false;
  @Param fillAvailable: boolean = false;
  @Param draft: QuestionDraft | undefined = undefined;
  @Param draftSession: string = '';
  @Event admission: () => boolean = (): boolean => true;
  @Event onLater: () => void = (): void => {};
  @Event onResolved: () => void = (): void => {};
  @Event onLink: (address: string) => void = (_address: string) => {};
  @Local selected: string = '';
  @Local selectedScreen: string = '';
  @Local screenOptions: string[] = [];
  @Local screenText: string = '';
  @Local openedUrl: string = '';
  @Local busy: boolean = false;
  @Local deferred: boolean = false;
  @Local textValues: Record<string, string> = Wire.payload([], []) as Record<string, string>;
  @Local questionOptions: Record<string, InteractionChoice[]> = {};
  @Local questionPicks: Record<string, string[]> = Wire.payload([], []) as Record<string, string[]>;
  @Local formValues: Record<string, Object> = Wire.payload([], []);

  aboutToAppear(): void { this.resetForm(); if (this.draft && this.draft.id === this.request.id && this.draft.raw === this.request.raw && this.draft.owns(this.draftSession)) { const text: Record<string, string> = {}, picks: Record<string, string[]> = {}; Object.keys(this.draft.text).forEach((key: string) => text[key] = this.draft!.text[key]); Object.keys(this.draft.picks).forEach((key: string) => picks[key] = this.draft!.picks[key].slice()); this.textValues = text; this.questionPicks = picks; } }
  aboutToDisappear(): void { if (this.draft && this.draft.id === this.request.id && this.draft.raw === this.request.raw) this.draft.save(this.textValues, this.questionPicks, this.draftSession); }
  @Monitor('request.id', 'request.raw') requestChanged(): void { this.resetForm(); }
  @Monitor('request.revision') revisionChanged(): void {
    this.selectedScreen = '';
    this.screenOptions = [];
    this.screenText = '';
    this.deferred = false;
  }

  private resetForm(): void {
    this.selected = '';
    this.selectedScreen = '';
    this.screenOptions = [];
    this.screenText = '';
    this.openedUrl = '';
    this.busy = false;
    this.deferred = false;
    this.textValues = Wire.payload([], []) as Record<string, string>;
    this.questionPicks = Wire.payload([], []) as Record<string, string[]>;
    this.formValues = interactionElicitationDefaults(this.request);
    const choices: Record<string, InteractionChoice[]> = {};
    if (this.request.kind === 'question') this.request.fields.forEach((field: DecisionField) => choices[field.key] = interactionQuestionOptions(this.request, field.key));
    this.questionOptions = choices;
  }

  private persistQuestionDraft(): void { if (this.request.kind === 'question' && this.draft && this.admission()) this.draft.save(this.textValues, this.questionPicks, this.draftSession); }
  private canRespond(): boolean {
    return this.admission() && !this.busy && this.model.canApprove() && interactionCanSubmit(this.request, this.readOnly);
  }

  private canDefer(): boolean {
    return interactionCanDefer(this.request, this.model.selected.kind, this.readOnly, this.busy);
  }

  private headerPrompt(): string {
    if (this.request.kind === 'screen') return interactionScreenQuestion(this.request);
    if (this.request.kind === 'elicitation') return interactionElicitationMessage(this.request);
    // Each question already labels its own body; only a distinct Hub prompt belongs above it.
    return this.request.prompt;
  }

  private submitButtonLabel(): string {
    return this.request.kind === 'screen' && interactionScreenMulti(this.request) ?
      interactionScreenSubmitLabel(this.request) : tr('confirm');
  }

  private submitIconKey(): string {
    if (this.request.kind === 'plan' || this.request.kind === 'permission') {
      if (this.selected === 'deny') return 'deny';
      if (this.selected === 'approve') return 'approve';
    }
    return 'confirm';
  }
  private submitIconLabel(): string {
    if ((this.request.kind === 'plan' || this.request.kind === 'permission') && ['approve', 'deny'].includes(this.selected))
      return tr('confirm') + ' ' + tr(this.selected === 'deny' ? 'deny' : this.request.kind === 'permission' ? 'allow' : 'approve');
    return this.submitButtonLabel();
  }

  private submitButtonOption(): string {
    return this.request.kind === 'screen' && interactionScreenMulti(this.request) ? 'submit' :
      this.request.kind === 'screen' ? this.selectedScreen : this.selected;
  }

  private statusLabel(): string {
    if (this.request.status === 'open') return tr('answer');
    if (this.request.status === 'pending') return tr('pending');
    if (this.request.status === 'resolved') return tr('completed');
    if (this.request.status === 'unknown') return tr('result_unknown');
    if (this.request.status === 'expired' || this.request.status === 'cancelled' || this.request.status === 'dismissed') return tr('canceled');
    return this.request.status;
  }

  private setText(key: string, value: string): void {
    if (!this.admission()) return;
    const next: Record<string, string> = Wire.payload([], []) as Record<string, string>;
    Object.keys(this.textValues).forEach((name: string) => next[name] = this.textValues[name]);
    next[key] = value;
    this.textValues = next;
    this.persistQuestionDraft();
  }

  private setValue(key: string, value: Object | undefined): void {
    const next: Record<string, Object> = Wire.payload([], []);
    Object.keys(this.formValues).filter((name: string) => name !== key).forEach((name: string) => next[name] = this.formValues[name]);
    if (value !== undefined && value !== null) next[key] = value;
    this.formValues = next;
  }

  private setQuestionPicks(key: string, values: string[]): void {
    const next: Record<string, string[]> = Wire.payload([], []) as Record<string, string[]>;
    Object.keys(this.questionPicks).forEach((name: string) => next[name] = this.questionPicks[name]);
    next[key] = values;
    this.questionPicks = next;
  }

  private setScreenOptions(values: string[]): void { this.screenOptions = values; }

  private questionChoices(field: DecisionField): InteractionChoice[] { return this.questionOptions[field.key] || []; }

  private pickedQuestion(field: DecisionField, value: string): boolean {
    return (this.questionPicks[field.key] || []).includes(value);
  }

  private pickQuestion(field: DecisionField, choice: InteractionChoice, checked: boolean | undefined = undefined): void {
    if (field.kind !== 'multi_choice') {
      this.setQuestionPicks(field.key, [choice.label]);
      this.setText(field.key + ':other', '');
      return;
    }
    const next = (this.questionPicks[field.key] || []).filter((item: string) => item !== choice.label);
    const picked = checked === undefined ? !this.pickedQuestion(field, choice.label) : checked;
    if (picked) next.push(choice.label);
    const ordered = this.questionChoices(field).filter((item: InteractionChoice) => next.includes(item.label)).map((item: InteractionChoice) => item.label);
    this.setQuestionPicks(field.key, ordered);
    this.persistQuestionDraft();
  }

  private toggleScreenOption(choice: InteractionChoice, checked: boolean | undefined = undefined): void {
    const next = this.screenOptions.filter((key: string) => key !== choice.key);
    const picked = checked === undefined ? !this.screenOptions.includes(choice.key) : checked;
    if (picked) next.push(choice.key);
    const ordered = interactionScreenOptions(this.request).filter((item: InteractionChoice) => next.includes(item.key)).map((item: InteractionChoice) => item.key);
    this.setScreenOptions(ordered);
  }

  private screenPicked(choice: InteractionChoice): boolean {
    return this.screenOptions.includes(choice.key);
  }

  private screenFreeText(): boolean {
    return interactionScreenOptions(this.request).some((choice: InteractionChoice) => choice.kind === 'type_something');
  }

  private selectedScreenChoice(): InteractionChoice | undefined {
    return interactionScreenOptions(this.request).find((choice: InteractionChoice) => choice.key === this.selectedScreen);
  }

  private schemaFields(): InteractionSchemaField[] { return interactionSchemaFields(this.request); }

  private schemaText(field: InteractionSchemaField): string {
    const value = this.formValues[field.key];
    return value === undefined || value === null ? '' : String(value);
  }

  private schemaPicked(field: InteractionSchemaField, index: number): boolean {
    const value = this.formValues[field.key], choice = field.choices[index];
    if (field.type === 'array') return Array.isArray(value) && (value as Object[]).some((item: Object) => String(item) === String(choice));
    return value !== undefined && String(value) === String(choice);
  }

  private schemaValidation(): InteractionValidation {
    return validateInteractionElicitation(this.request, this.formValues);
  }

  private inputType(field: InteractionSchemaField): InputType {
    if (field.secret) return InputType.Password;
    if (field.type === 'number' || field.type === 'integer') return InputType.Number;
    return InputType.Normal;
  }

  private canSubmit(): boolean {
    if (!this.canRespond()) return false;
    if (this.request.kind === 'plan' || this.request.kind === 'permission') {
      if (!this.selected) return false;
      if (this.selected === 'deny') return true;
      if (this.request.kind === 'permission') return !this.request.fields.some((field: DecisionField) =>
        field.required && !(this.textValues[field.key] || '').trim());
      return true;
    }
    if (this.request.kind === 'question') return this.request.fields.length > 0 && this.request.fields.every((field: DecisionField) =>
      !!(this.textValues[field.key + ':other'] || (this.questionPicks[field.key] || []).join(', ')).trim());
    if (this.request.kind === 'screen') {
      if (interactionScreenMulti(this.request)) return this.screenOptions.length > 0 || !!this.screenText.trim();
      const choice = this.selectedScreenChoice();
      return !!choice && choice.kind !== 'unknown' && (choice.kind !== 'type_something' || !!this.screenText.trim());
    }
    if (this.request.kind === 'elicitation') {
      const mode = interactionElicitationMode(this.request);
      if (mode === 'url') return !!interactionElicitationUrl(this.request) && this.openedUrl === interactionElicitationUrl(this.request);
      return interactionElicitationSupported(this.request) && this.schemaValidation().valid;
    }
    return false;
  }

  private answerValues(): Record<string, Object> {
    const values: Record<string, Object> = Wire.payload([], []);
    if (this.request.kind === 'question') {
      this.request.fields.forEach((field: DecisionField) => {
        const other = (this.textValues[field.key + ':other'] || '').trim();
        const options = this.questionChoices(field).filter((choice: InteractionChoice) => (this.questionPicks[field.key] || []).includes(choice.label))
          .map((choice: InteractionChoice) => choice.label);
        const answer = other || options.join(', ');
        if (answer) values[field.key] = answer;
      });
    } else if (this.request.kind === 'plan') {
      values['feedback'] = this.textValues['feedback'] || '';
    } else if (this.request.kind === 'permission') {
      if (this.selected !== 'deny') this.request.fields.forEach((field: DecisionField) => {
        const value = (this.textValues[field.key] || '').trim();
        if (value) values[field.key] = value;
      });
    } else if (this.request.kind === 'screen') {
      values['options'] = this.screenOptions;
      values['text'] = this.screenText;
    } else if (this.request.kind === 'elicitation') {
      Object.keys(this.formValues).forEach((key: string) => values[key] = this.formValues[key]);
      if (this.openedUrl) values['opened_url'] = this.openedUrl;
    }
    return values;
  }

  private clearSecrets(): void {
    const keys: string[] = this.request.kind === 'permission' ? this.request.fields.filter((field: DecisionField) => field.kind === 'secret').map((field: DecisionField) => field.key) :
      this.request.kind === 'elicitation' ? this.schemaFields().filter((field: InteractionSchemaField) => field.secret).map((field: InteractionSchemaField) => field.key) : [];
    const next: Record<string, string> = Wire.payload([], []) as Record<string, string>;
    Object.keys(this.textValues).filter((key: string) => !keys.includes(key)).forEach((key: string) => next[key] = this.textValues[key]);
    this.textValues = next;
    keys.forEach((key: string) => this.setValue(key, undefined));
  }

  private submit(option: string): void {
    if (!this.canRespond()) return;
    if (this.request.kind === 'question' && option === 'cancel') {
      if (!interactionScreenCanCancel(this.request)) return;
    } else if (this.request.kind === 'elicitation' && (option === 'decline' || option === 'cancel')) {
      // Decline and cancel remain available when a form is unsupported or incomplete.
    } else if (!this.canSubmit()) return;
    const values = this.request.kind === 'question' && option === 'cancel' || this.request.kind === 'elicitation' && option !== 'accept' ?
      Wire.payload([], []) : this.answerValues();
    this.busy = true;
    this.clearSecrets();
    this.model.answer(option, values, this.request, this.admission).then(() => { this.busy = false; if (this.request.status === 'resolved') this.onResolved(); });
  }

  private historySummary(): string {
    const result = interactionHistoryResolution(this.request), rows: string[] = [];
    if (this.request.kind === 'question') Object.keys(result).forEach((key: string) => rows.push(key + ': ' + Wire.text(result, key)));
    else if (this.request.kind === 'plan') {
      if (result['approve'] === true) rows.push(tr('approve'));
      else if (result['approve'] === false) rows.push(tr('deny'));
      if (Wire.text(result, 'feedback')) rows.push(Wire.text(result, 'feedback'));
    } else if (this.request.kind === 'permission') {
      if (result['allow'] === true) rows.push(tr('allow'));
      else if (result['allow'] === false) rows.push(tr('deny'));
    } else if (this.request.kind === 'screen') {
      const choice = Wire.text(result, 'option_label');
      if (choice) rows.push(choice);
      const options = Wire.strings(result, 'options_labels');
      if (options.length) rows.push(options.join(', '));
      if (Wire.text(result, 'text')) rows.push(Wire.text(result, 'text'));
    } else if (this.request.kind === 'elicitation') {
      const action = Wire.text(result, 'action');
      if (action === 'accept') rows.push(tr('approve'));
      else if (action === 'decline') rows.push(tr('deny'));
      else if (action === 'cancel') rows.push(tr('canceled'));
    }
    return rows.join('\n');
  }

  @Builder actionChoice(label: string, value: string) {
    Row({ space: Tokens.inset }) {
      Text(label).fontSize(Tokens.body).fontColor(Colors.ink).layoutWeight(1)
      Radio({ value: value, group: 'persona-interaction-action-' + this.request.id })
        .checked(this.selected === value)
        .radioStyle({ checkedBackgroundColor: Colors.action, uncheckedBorderColor: Colors.secondary, indicatorColor: Colors.onAction })
        .accessibilityText(label).enabled(this.canRespond())
        .onChange((checked: boolean) => { if (checked) this.selected = value; })
    }.width('100%').constraintSize({ minHeight: Tokens.touch }).padding({ left: Tokens.inset, right: Tokens.inset, top: Tokens.fine, bottom: Tokens.fine })
      .enabled(this.canRespond()).onClick(() => { if (this.canRespond()) this.selected = value; })
      .id('interaction.action.' + value)
  }

  @Builder questionChoice(field: DecisionField, choice: InteractionChoice) {
    Row({ space: Tokens.inset }) {
      Column({ space: Tokens.fine }) {
        Text(choice.label).fontSize(Tokens.body).fontColor(Colors.ink).width('100%')
        if (choice.detail) Text(choice.detail).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
      }.layoutWeight(1).alignItems(HorizontalAlign.Start).onClick(() => {
        if (this.canRespond()) this.pickQuestion(field, choice);
      })
      if (field.kind === 'multi_choice') Checkbox({ name: choice.label })
        .select(this.pickedQuestion(field, choice.label)).selectedColor(Colors.action).unselectedColor(Colors.secondary)
        .accessibilityText(choice.label).responseRegion({ x: -Tokens.fine, y: -Tokens.fine, width: Tokens.touch, height: Tokens.touch }).enabled(this.canRespond())
        .onChange((checked: boolean) => this.pickQuestion(field, choice, checked))
      else Radio({ value: choice.label, group: 'persona-interaction-question-' + this.request.id + '-' + field.key })
        .checked(this.pickedQuestion(field, choice.label))
        .radioStyle({ checkedBackgroundColor: Colors.action, uncheckedBorderColor: Colors.secondary, indicatorColor: Colors.onAction })
        .accessibilityText(choice.label).responseRegion({ x: -Tokens.fine, y: -Tokens.fine, width: Tokens.touch, height: Tokens.touch }).enabled(this.canRespond())
        .onChange((checked: boolean) => { if (checked) this.pickQuestion(field, choice); })
    }.width('100%').constraintSize({ minHeight: Tokens.touch }).padding({ left: Tokens.inset, right: Tokens.inset, top: Tokens.fine, bottom: Tokens.fine })
      .enabled(this.canRespond()).onClick(() => { if (this.canRespond() && field.kind !== 'multi_choice') this.pickQuestion(field, choice); })
      .id('interaction.question.' + field.key + '.' + choice.label)
  }

  @Builder screenChoice(choice: InteractionChoice) {
    Row({ space: Tokens.inset }) {
      Column({ space: Tokens.fine }) {
        Text(choice.label).fontSize(Tokens.body).fontColor(Colors.ink).width('100%')
        if (choice.detail) Text(choice.detail).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
      }.layoutWeight(1).alignItems(HorizontalAlign.Start).onClick(() => {
        if (!this.canRespond()) return;
        if (interactionScreenMulti(this.request) && choice.kind === 'plain') this.toggleScreenOption(choice);
        else if (!interactionScreenMulti(this.request)) this.selectedScreen = choice.key;
      })
      if (interactionScreenMulti(this.request) && choice.kind === 'plain') Checkbox({ name: choice.label })
        .select(this.screenPicked(choice)).selectedColor(Colors.action).unselectedColor(Colors.secondary)
        .accessibilityText(choice.label).responseRegion({ x: -Tokens.fine, y: -Tokens.fine, width: Tokens.touch, height: Tokens.touch }).enabled(this.canRespond())
        .onChange((checked: boolean) => this.toggleScreenOption(choice, checked))
      else if (!interactionScreenMulti(this.request)) Radio({ value: choice.key, group: 'persona-interaction-screen-' + this.request.id })
        .checked(this.selectedScreen === choice.key)
        .radioStyle({ checkedBackgroundColor: Colors.action, uncheckedBorderColor: Colors.secondary, indicatorColor: Colors.onAction })
        .accessibilityText(choice.label).responseRegion({ x: -Tokens.fine, y: -Tokens.fine, width: Tokens.touch, height: Tokens.touch }).enabled(this.canRespond())
        .onChange((checked: boolean) => { if (checked) this.selectedScreen = choice.key; })
    }.width('100%').constraintSize({ minHeight: Tokens.touch }).padding({ left: Tokens.inset, right: Tokens.inset, top: Tokens.fine, bottom: Tokens.fine })
      .enabled(this.canRespond()).onClick(() => {
        if (!this.canRespond()) return;
        if (!interactionScreenMulti(this.request)) this.selectedScreen = choice.key;
      }).id('interaction.screen.' + choice.key)
  }

  @Builder schemaChoice(field: InteractionSchemaField, index: number) {
    Row({ space: Tokens.inset }) {
      Text(field.choiceLabels[index]).fontSize(Tokens.body).fontColor(Colors.ink).layoutWeight(1).onClick(() => {
        if (!this.canRespond()) return;
        if (field.type === 'array') this.toggleSchemaChoice(field, index, !this.schemaPicked(field, index));
        else this.setValue(field.key, field.choices[index]);
      })
      if (field.type === 'array') Checkbox({ name: field.choiceLabels[index] })
        .select(this.schemaPicked(field, index)).selectedColor(Colors.action).unselectedColor(Colors.secondary)
        .accessibilityText(field.choiceLabels[index]).responseRegion({ x: -Tokens.fine, y: -Tokens.fine, width: Tokens.touch, height: Tokens.touch }).enabled(this.canRespond())
        .onChange((checked: boolean) => this.toggleSchemaChoice(field, index, checked))
      else Radio({ value: String(field.choices[index]), group: 'persona-interaction-field-' + this.request.id + '-' + field.key })
        .checked(this.schemaPicked(field, index))
        .radioStyle({ checkedBackgroundColor: Colors.action, uncheckedBorderColor: Colors.secondary, indicatorColor: Colors.onAction })
        .accessibilityText(field.choiceLabels[index]).responseRegion({ x: -Tokens.fine, y: -Tokens.fine, width: Tokens.touch, height: Tokens.touch }).enabled(this.canRespond())
        .onChange((checked: boolean) => { if (checked) this.setValue(field.key, field.choices[index]); })
    }.width('100%').constraintSize({ minHeight: Tokens.touch }).padding({ left: Tokens.inset, right: Tokens.inset, top: Tokens.fine, bottom: Tokens.fine })
      .enabled(this.canRespond()).onClick(() => {
        if (!this.canRespond()) return;
        if (field.type !== 'array') this.setValue(field.key, field.choices[index]);
      }).id('interaction.field.choice.' + field.key + '.' + String(index))
  }

  private toggleSchemaChoice(field: InteractionSchemaField, index: number, checked: boolean): void {
    const selected: Object[] = Array.isArray(this.formValues[field.key]) ? this.formValues[field.key] as Object[] : [];
    const choice = field.choices[index], next = selected.filter((item: Object) => String(item) !== String(choice));
    if (checked) next.push(choice);
    const ordered = field.choices.filter((item: Object) => next.some((value: Object) => String(value) === String(item)));
    this.setValue(field.key, ordered.length ? ordered : undefined);
  }

  @Builder questionField(field: DecisionField) {
    Column({ space: Tokens.gap }) {
      Text(field.title).fontSize(Tokens.body).fontWeight(FontWeight.Medium).width('100%')
      ForEach(this.questionChoices(field), (choice: InteractionChoice) => this.questionChoice(field, choice), (choice: InteractionChoice) => choice.label)
      TextInput({ text: this.textValues[field.key + ':other'] || '', placeholder: tr('other') })
        .width('100%').constraintSize({ minHeight: Tokens.touch }).fontColor(Colors.ink).placeholderColor(Colors.secondary).caretColor(Colors.action)
        .fontSize(Tokens.body).placeholderFont({ size: Tokens.body })
        .enabled(this.canRespond()).onChange((value: string) => this.setText(field.key + ':other', value)).id('interaction.question.' + field.key + '.other')
    }.width('100%').alignItems(HorizontalAlign.Start)
  }

  @Builder permissionField(field: DecisionField) {
    Column({ space: Tokens.gap }) {
      Text(field.title).fontSize(Tokens.body).fontWeight(FontWeight.Medium).width('100%')
      TextInput({ text: this.textValues[field.key] || '', placeholder: interactionPermissionFieldPlaceholder(this.request, field.key) })
        .type(field.kind === 'secret' ? InputType.Password : InputType.Normal).showPasswordIcon(false)
        .width('100%').constraintSize({ minHeight: Tokens.touch }).fontColor(Colors.ink).placeholderColor(Colors.secondary).caretColor(Colors.action)
        .fontSize(Tokens.body).placeholderFont({ size: Tokens.body })
        .enabled(this.canRespond()).onChange((value: string) => this.setText(field.key, value)).id('interaction.field.' + field.key)
    }.width('100%').alignItems(HorizontalAlign.Start)
  }

  @Builder elicitationField(field: InteractionSchemaField) {
    Column({ space: Tokens.gap }) {
      Row({ space: Tokens.gap }) {
        Text(field.title).fontSize(Tokens.body).fontWeight(FontWeight.Medium).layoutWeight(1)
        if (field.required) Text(tr('required')).fontSize(Tokens.meta).fontColor(Colors.secondary)
      }.width('100%')
      if (field.description) Text(field.description).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
      if (field.type === 'boolean') {
        Toggle({ type: ToggleType.Switch, isOn: this.formValues[field.key] === true })
          .selectedColor(Colors.action).enabled(this.canRespond()).onChange((checked: boolean) => this.setValue(field.key, checked)).id('interaction.field.' + field.key)
      } else if (field.choices.length > 0) {
        ForEach(field.choices, (_choice: Object, index: number) => this.schemaChoice(field, index), (choice: Object, index: number) => field.key + ':' + String(index) + ':' + String(choice))
      } else {
        TextInput({ text: this.schemaText(field), placeholder: field.title }).type(this.inputType(field)).showPasswordIcon(false)
          .width('100%').constraintSize({ minHeight: Tokens.touch }).fontColor(Colors.ink).placeholderColor(Colors.secondary).caretColor(Colors.action)
          .fontSize(Tokens.body).placeholderFont({ size: Tokens.body })
          .enabled(this.canRespond()).onChange((value: string) => this.setValue(field.key, value || undefined)).id('interaction.field.' + field.key)
      }
      if (this.schemaValidation().errors[field.key]) Text(tr(this.schemaValidation().errors[field.key] === 'required' ? 'required' : 'failed')).fontSize(Tokens.meta)
        .fontColor(Colors.needs).width('100%')
    }.width('100%').alignItems(HorizontalAlign.Start)
  }

  @Builder elicitationForm() {
    if (interactionElicitationSupported(this.request)) {
      ForEach(this.schemaFields(), (field: InteractionSchemaField) => this.elicitationField(field), (field: InteractionSchemaField) => field.key)
    } else {
      Text(tr('unknown_field')).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
    }
  }

  @Builder bodyContent() {
    if (this.request.kind === 'question') {
      ForEach(this.request.fields, (field: DecisionField) => this.questionField(field), (field: DecisionField) => field.key)
    } else if (this.request.kind === 'plan') {
      this.actionChoice(tr('approve'), 'approve')
      this.actionChoice(tr('deny'), 'deny')
      if (this.selected === 'deny') TextArea({ text: this.textValues['feedback'] || '', placeholder: tr('feedback') })
        .width('100%').constraintSize({ minHeight: Tokens.touch }).fontSize(Tokens.body).lineHeight(Tokens.lineHeight)
        .fontColor(Colors.ink).placeholderColor(Colors.secondary).caretColor(Colors.action)
        .enabled(this.canRespond()).onChange((value: string) => this.setText('feedback', value))
    } else if (this.request.kind === 'permission') {
      ForEach(this.request.fields, (field: DecisionField) => this.permissionField(field), (field: DecisionField) => field.key)
      this.actionChoice(tr('allow'), 'approve')
      this.actionChoice(tr('deny'), 'deny')
    } else if (this.request.kind === 'screen') {
      ForEach(interactionScreenOptions(this.request), (choice: InteractionChoice) => this.screenChoice(choice), (choice: InteractionChoice) => choice.key)
      if (interactionScreenMulti(this.request) && this.screenFreeText()) TextInput({ text: this.screenText, placeholder: tr('other') })
        .width('100%').constraintSize({ minHeight: Tokens.touch }).fontSize(Tokens.body).placeholderFont({ size: Tokens.body })
        .fontColor(Colors.ink).placeholderColor(Colors.secondary).caretColor(Colors.action)
        .enabled(this.canRespond()).onChange((value: string) => this.screenText = value).id('interaction.screen.other')
      else if (!interactionScreenMulti(this.request) && this.selectedScreenChoice()?.kind === 'type_something') TextInput({ text: this.screenText, placeholder: tr('other') })
        .width('100%').constraintSize({ minHeight: Tokens.touch }).fontSize(Tokens.body).placeholderFont({ size: Tokens.body })
        .fontColor(Colors.ink).placeholderColor(Colors.secondary).caretColor(Colors.action)
        .enabled(this.canRespond()).onChange((value: string) => this.screenText = value).id('interaction.screen.other')
    } else if (this.request.kind === 'elicitation') {
      if (interactionElicitationMode(this.request) === 'url' && interactionElicitationUrl(this.request)) {
        Text(interactionElicitationUrl(this.request)).fontSize(Tokens.meta).width('100%')
      } else if (interactionElicitationMode(this.request) === 'url') {
        Text(tr('unsafe_endpoint')).fontSize(Tokens.meta).fontColor(Colors.needs).width('100%')
      } else {
        this.elicitationForm()
      }
    }
  }

  private openUrl(): void {
    const address = interactionElicitationUrl(this.request);
    if (!address || !this.canRespond()) return;
    this.onLink(address);
    this.openedUrl = address;
  }

  @Builder headerContent() {
    Column({ space: Tokens.gap }) {
      if (this.request.status !== 'open' || this.readOnly || this.deferred) Row({ space: Tokens.gap }) {
        Text(this.deferred ? tr('interaction_question_later') : this.statusLabel()).fontSize(Tokens.meta).fontWeight(FontWeight.Medium).layoutWeight(1)
        if (this.readOnly) Text(tr('read_only')).fontSize(Tokens.meta).fontColor(Colors.secondary)
      }.width('100%')
      if (this.headerPrompt()) Text(this.headerPrompt()).fontSize(Tokens.title).fontWeight(FontWeight.Medium).fontColor(Colors.ink).width('100%')
    }.width('100%').alignItems(HorizontalAlign.Start)

  }

  @Builder responseFooter() {
    Flex({ wrap: FlexWrap.Wrap, justifyContent: FlexAlign.End, alignItems: ItemAlign.Center, space: { main: LengthMetrics.vp(Tokens.gap), cross: LengthMetrics.vp(Tokens.gap) } }) {
      if (this.deferred && !this.readOnly) Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('interaction_question_reopen')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('interaction_question_reopen', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('interaction_question_reopen')).bindTips(tr('interaction_question_reopen'))
        .onClick(() => this.deferred = false).id('interaction.reopen')
      else if (!this.readOnly && this.request.kind === 'question') {
        if (this.pinFooter || this.fillAvailable) Flex({ wrap: FlexWrap.Wrap, justifyContent: FlexAlign.End, alignItems: ItemAlign.Center, space: { main: LengthMetrics.vp(0), cross: LengthMetrics.vp(Tokens.gap) } }) {
          if (this.pinFooter || this.canDefer()) Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('interaction_question_later')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('interaction_question_later', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('interaction_question_later')).bindTips(tr('interaction_question_later')).flexShrink(0).margin({ end: LengthMetrics.vp(Tokens.gap) }).enabled(this.canDefer())
            .onClick(() => { this.deferred = true; this.onLater(); }).id('interaction.later')
          if (interactionScreenCanCancel(this.request) && !this.pinFooter) Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('cancel')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('cancel', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('cancel')).bindTips(tr('cancel')).flexShrink(0).margin({ end: LengthMetrics.vp(Tokens.gap) })
            .enabled(this.canRespond()).onClick(() => this.submit('cancel')).id('interaction.cancel')
          if (interactionScreenCanCancel(this.request) && this.pinFooter) Button({ type: ButtonType.Circle }) { Image(Symbols.more).width(Tokens.symbol).height(Tokens.symbol).fillColor(Colors.ink) }
            .attributeModifier(ButtonStyles.chrome).enabled(this.canRespond()).accessibilityText(tr('more'))
            .margin({ end: LengthMetrics.vp(Tokens.gap) }).bindMenu([{ value: tr('interaction_question_cancel'), action: () => this.submit('cancel') }]).flexShrink(0).id('interaction.more')
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('confirm')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('confirm', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('confirm')).bindTips(tr('confirm')).flexShrink(0).enabled(this.canSubmit())
            .onClick(() => this.submit('answer')).id('interaction.submit')
        }.width('100%')
        else {
          if (this.canDefer()) Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('interaction_question_later')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('interaction_question_later', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('interaction_question_later')).bindTips(tr('interaction_question_later'))
            .onClick(() => { this.deferred = true; this.onLater(); }).id('interaction.later')
          if (interactionScreenCanCancel(this.request)) Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('cancel')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('cancel', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('cancel')).bindTips(tr('cancel'))
            .enabled(this.canRespond()).onClick(() => this.submit('cancel')).id('interaction.cancel')
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('confirm')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('confirm', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('confirm')).bindTips(tr('confirm')).enabled(this.canSubmit())
            .onClick(() => this.submit('answer')).id('interaction.submit')
        }
      } else if (!this.readOnly && this.request.kind === 'elicitation') {
        if (interactionElicitationMode(this.request) === 'url' && interactionElicitationUrl(this.request)) {
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('open')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('open', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('open')).bindTips(tr('open')).enabled(this.canRespond())
            .onClick(() => this.openUrl()).id('interaction.open')
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('continue')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('continue', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('continue')).bindTips(tr('continue')).enabled(this.canSubmit())
            .onClick(() => this.submit('accept')).id('interaction.continue')
        } else if (interactionElicitationMode(this.request) !== 'url' && interactionElicitationSupported(this.request)) {
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('confirm')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('confirm', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('confirm')).bindTips(tr('confirm')).enabled(this.canSubmit())
            .onClick(() => this.submit('accept')).id('interaction.accept')
        }
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('deny')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('deny', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('deny')).bindTips(tr('deny')).enabled(this.canRespond())
          .onClick(() => this.submit('decline')).id('interaction.decline')
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('cancel')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('cancel', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('cancel')).bindTips(tr('cancel')).enabled(this.canRespond())
          .onClick(() => this.submit('cancel')).id('interaction.cancel')
      } else if (!this.readOnly) {
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol(this.submitIconKey())).fontSize(Tokens.symbolFont).fontColor([IconActions.color(this.submitIconKey(), this.submitIconKey() !== 'deny')]) }.attributeModifier(this.submitIconKey() === 'deny' ? ButtonStyles.icon : ButtonStyles.iconPrimary).accessibilityText(this.submitIconLabel()).bindTips(this.submitIconLabel()).enabled(this.canSubmit())
          .onClick(() => this.submit(this.submitButtonOption())).id('interaction.submit')
      }
      if (this.model.error) Text(tr(this.model.error)).fontSize(Tokens.meta).fontColor(Colors.needs).width('100%')
    }.width('100%').padding({ top: Tokens.gap })

  }

  @Builder formBody() {
    this.headerContent()
    if (this.readOnly) {
      if (this.historySummary()) Text(this.historySummary()).fontSize(Tokens.body).fontColor(Colors.ink).width('100%')
    } else if (!this.deferred) this.bodyContent()
  }

  build() {
    if (this.fillAvailable && !this.pinFooter && this.request.kind !== 'question') Scroll() {
      Column({ space: Tokens.gap }) { this.formBody(); this.responseFooter() }
        .width('100%').padding({ top: Tokens.page }).alignItems(HorizontalAlign.Start)
    }.width('100%').height('100%').scrollBar(BarState.Auto).align(Alignment.TopStart).id('interaction.body')
    else Column({ space: Tokens.gap }) {
      if (this.pinFooter || this.fillAvailable) Scroll() {
        Column({ space: Tokens.gap }) { this.formBody() }.width('100%').alignItems(HorizontalAlign.Start)
      }.width('100%').layoutWeight(1).scrollBar(BarState.Auto).align(Alignment.TopStart).id('interaction.body')
      else this.formBody()
      this.responseFooter()
    }.width('100%').height(this.pinFooter || this.fillAvailable ? '100%' : undefined)
      .padding({ left: this.fillAvailable ? 0 : Tokens.page, right: this.fillAvailable ? 0 : Tokens.page, top: Tokens.page, bottom: this.pinFooter || this.fillAvailable ? 0 : Tokens.page }).alignItems(HorizontalAlign.Start)
  }
}
