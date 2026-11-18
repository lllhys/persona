import { IconActions } from '../design/IconActions';
import { Symbols } from '../design/Symbols';
import { util } from '@kit.ArkTS';
import { NativeEnvironment, nativeEnvironment } from '../platform/NativeEnvironment';
import { nativeSheetInset } from '../platform/NativeInsets';
import { AssistantViewCard } from './AssistantViewCard';
import { assistantCardResource } from '../models/AssistantCardLinks';
import { ComposeListItemV2, ContentItemV2, OperateItemV2, OperateCheckV2, LengthMetrics } from '@kit.ArkUI';
import { ClientModel } from '../models/ClientModel';
import { AssistantFeedModel, PlanEditTask } from '../models/AssistantFeed';
import { ResourceRow, Wire } from '../models/Wire';
import { MarkdownText } from './MarkdownText';
import { Tokens } from '../design/Tokens';
import { Colors } from '../design/Colors';
import { ButtonStyles } from '../design/Controls';
import { tr } from '../design/Strings';
import { projectAssistantReceipt, AssistantReceiptProjection, AssistantReceiptCriterion } from '../models/AssistantReceipt';

// The Hub accepts bypassPermissions for writable task plans; read-only tasks
// are locked to plan mode and cannot be upgraded from this editor.
const PLAN_PERMISSIONS: string[] = ['bypassPermissions'];

@ComponentV2
export struct AssistantCards {
  @Require @Param model: ClientModel;
  @Require @Param feed: AssistantFeedModel;
  @Param item: Record<string, Object> | undefined = undefined;
  @Param dockOnly: boolean = false;
  @Param detailOnly: boolean = false;
  @Param active: boolean = false;
  @Event onOpenResource: (row: ResourceRow) => void = () => {};
  @Event onOpenSession: (row: ResourceRow) => void = (_row: ResourceRow) => {};
  @Event onOpenTodo: (row: ResourceRow) => void = (_row: ResourceRow) => {};
  @Event onOpenInbox: (row: ResourceRow) => void = (_row: ResourceRow) => {};
  @Event onOpenCard: (itemId: string, kind: string) => void = (_itemId: string, _kind: string) => {};
  @Event onLink: (address: string) => void = (_address: string) => {};

  @Local environment: NativeEnvironment = nativeEnvironment;
  @Local editorInset: number = 0;
  private editorReady: boolean = false;
  private editorNodeId: string = 'assistant.editor.' + util.generateRandomUUID();
  @Monitor('environment.safeBottomPx') editorBoundaryChanged(): void { this.measureEditor(); }
  private measureEditor(): void {
    if (!this.editorReady) return;
    const value = nativeSheetInset(this.getUIContext(), this.editorNodeId, this.environment.safeBottomPx);
    if (Math.abs(value - this.editorInset) > 0.1) this.editorInset = value;
  }
  @Local receiptView: AssistantReceiptProjection = projectAssistantReceipt({}, {}, false);
  @Local receiptViewStamp: string = '';
  aboutToAppear(): void { this.updateReceiptView(); }
  @Monitor('item', 'feed.items', 'feed.todos', 'feed.todosReady', 'feed.todoError', 'feed.busyKeys', 'feed.uncertainKeys', 'model.sessions', 'model.machines', 'model.unresolvedWrites', 'model.selected', 'model.profile', 'model.pendingDecisions', 'model.decision', 'model.connected')
  receiptFactsChanged(): void { this.updateReceiptView(); }
  private updateReceiptView(): void {
    if (!this.item || Wire.text(this.item, 'kind') !== 'receipt') return;
    const projection = this.receiptProjection(this.item), todo = Wire.text(this.item, 'ref');
    this.receiptView = projection;
    const task = this.receiptTodo(this.item), agents = Wire.rows(task, 'agents');
    const sessions = this.model.sessions.filter((row: ResourceRow) => agents.some((agent: Record<string, Object>) => Wire.text(agent, 'machine_id') === row.machine && Wire.text(agent, 'session_id') === (row.session || row.id)))
      .map((row: ResourceRow) => [row.key(), row.title, row.machine, this.model.machineName(row.machine)]);
    this.receiptViewStamp = JSON.stringify([Wire.text(this.item, 'id'), todo, projection, this.feed.todosReady, Wire.text(task, 'id'), agents, sessions, this.feed.busyKeys.includes('todo:' + todo)]);
  }

  private tasks(): Record<string, Object>[] { return this.feed.taskCardsForConversation(); }

  private stateLabel(state: string): string {
    const known: string[] = ['not_started', 'working', 'waiting', 'needs_you', 'interrupted', 'usage_limit', 'unread', 'followup', 'done', 'failed', 'cancelled', 'accepted'];
    return tr(known.includes(state) ? 'assistant_feed_state_' + state : 'assistant_feed_state_unknown');
  }

  private machineLabel(id: string): string { return id ? this.model.machineName(id) || id : ''; }

  private planId(item: Record<string, Object>): string { return Wire.text(item, 'ref'); }
  private plan(item: Record<string, Object>): Record<string, Object> {
    const id = this.planId(item), current = Wire.record(this.feed.plans[id]);
    if (Object.keys(current).length > 0) return current;
    const snapshot = Wire.object(this.itemBody(item), 'plan');
    return Wire.text(snapshot, 'id') === id ? snapshot : Wire.payload([], []);
  }
  private planTasks(item: Record<string, Object>): Record<string, Object>[] { return Wire.rows(this.plan(item), 'tasks'); }
  private planState(item: Record<string, Object>): string { return Wire.text(this.plan(item), 'state', 'unknown'); }
  private planSummaryTitle(item: Record<string, Object>): string {
    const tasks = this.planTasks(item);
    return tasks.length > 0 ? Wire.text(tasks[0], 'title', tr('assistant_feed_plan_unavailable')) : tr('assistant_feed_plan_unavailable');
  }
  private planSummaryMore(item: Record<string, Object>): string {
    const count = this.planTasks(item).length;
    return count > 1 ? tr('assistant_detail_more_tasks') + ' ' + String(count - 1) : '';
  }
  private taskBrief(task: Record<string, Object>): Record<string, Object> { return Wire.object(task, 'brief'); }
  private taskGoal(task: Record<string, Object>): string {
    return Wire.text(this.taskBrief(task), 'goal') || Wire.text(task, 'goal');
  }
  private taskOutOfBounds(task: Record<string, Object>): string[] {
    const values = Wire.strings(this.taskBrief(task), 'out_of_bounds');
    return values.length ? values : Wire.strings(task, 'out_of_bounds');
  }
  private taskDoneWhen(task: Record<string, Object>): string[] {
    const brief = this.taskBrief(task);
    if (Wire.has(brief, 'criteria')) return Wire.strings(brief, 'criteria');
    const values = Wire.strings(brief, 'done_when');
    return values.length ? values : Wire.strings(task, 'done_when');
  }
  private taskBriefSource(task: Record<string, Object>): string[] {
    const brief = this.taskBrief(task), request = Wire.object(brief, 'request');
    return [Wire.text(request, 'text'), Wire.text(brief, 'handover'), Wire.text(brief, 'reason')]
      .filter((value: string) => !!value.trim());
  }
  private itemBody(item: Record<string, Object>): Record<string, Object> { return Wire.object(item, 'body'); }
  private reportText(item: Record<string, Object>): string {
    const body = this.itemBody(item);
    return Wire.text(body, 'detail', Wire.text(body, 'text', Wire.text(body, 'summary')));
  }
  private todoId(card: Record<string, Object>): string { return Wire.text(card, 'todo_id'); }
  private taskTodo(card: Record<string, Object>): Record<string, Object> { return this.feed.todo(this.todoId(card)); }
  private activeAgents(card: Record<string, Object>): Record<string, Object>[] {
    return Wire.rows(this.taskTodo(card), 'agents').filter((agent: Record<string, Object>) =>
      !Wire.text(agent, 'released_at') && !Wire.text(agent, 'dismissed_at'));
  }
  private todoAgents(todoId: string): Record<string, Object>[] {
    return this.activeAgents(Wire.payload(['todo_id'], [todoId]));
  }
  private openTodo(id: string): void {
    const row = this.feed.openTodo(id);
    if (row) this.onOpenTodo(row);
  }
  private openAgentSession(todoId: string, agent: Record<string, Object>): void {
    const row = this.feed.openAgent(todoId, agent);
    if (row) this.onOpenSession(row);
  }
  private agentTitle(todoId: string, agent: Record<string, Object>): string {
    const row = this.feed.openAgent(todoId, agent);
    return row ? row.title || row.session : Wire.text(agent, 'agent');
  }
  private agentMachine(todoId: string, agent: Record<string, Object>): string {
    const row = this.feed.openAgent(todoId, agent);
    return row ? this.machineLabel(row.machine) : '';
  }
  private messageId(item: Record<string, Object>): string { return Wire.text(item, 'id'); }
  private messageState(item: Record<string, Object>): string { return this.feed.messageState(item); }
  private messageTodoId(item: Record<string, Object>): string { return Wire.text(this.itemBody(item), 'todo_id'); }
  private messageTodo(item: Record<string, Object>): Record<string, Object> { return this.feed.todo(this.messageTodoId(item)); }
  private openMessageTodo(item: Record<string, Object>): void { this.openTodo(this.messageTodoId(item)); }

  private openDetail(item: Record<string, Object>): void {
    const id = Wire.text(item, 'id'), kind = Wire.text(item, 'kind');
    if (this.active && id && ['plan', 'receipt'].includes(kind)) this.onOpenCard(id, kind);
  }

  @Builder planSummary(item: Record<string, Object>) {
    Button({ type: ButtonType.Normal }) {
      Row({ space: Tokens.inset }) {
        SymbolGlyph(Symbols.document).fontSize(Tokens.symbolFont).fontColor([Colors.ai])
        Column({ space: Tokens.fine }) {
          Text(tr('assistant_feed_plan')).fontSize(Tokens.meta).fontColor(Colors.ai).width('100%')
          Text(this.planSummaryTitle(item)).fontSize(Tokens.body).fontWeight(FontWeight.Medium).fontColor(Colors.ink).width('100%')
          if (this.planTasks(item).length > 1) Text(this.planSummaryMore(item)).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
        }.layoutWeight(1).alignItems(HorizontalAlign.Start)
        SymbolGlyph(Symbols.expand).fontSize(Tokens.meta).fontColor([Colors.secondary])
      }.width('100%').alignItems(VerticalAlign.Center)
    }.buttonStyle(ButtonStyleMode.NORMAL).backgroundColor(Colors.grouped).borderRadius(Tokens.card)
      .padding(Tokens.page).width('100%').enabled(this.active).onClick(() => this.openDetail(item))
      .id('assistant.detail.plan.' + Wire.text(item, 'id')).accessibilityText(tr('assistant_detail_view_plan') + ' ' + this.planSummaryTitle(item) + (this.planTasks(item).length > 1 ? ' ' + this.planSummaryMore(item) : ''))
  }

  @Builder planMenu(item: Record<string, Object>) {
    Menu() {
      MenuItem({ content: tr('assistant_detail_dismiss_plan') })
        .enabled(this.feed.planCanDismiss(this.planId(item)))
        .onClick(() => this.feed.dismissPlan(this.planId(item), JSON.stringify(this.plan(item))))
    }
  }

  @Builder planCard(item: Record<string, Object>) {
    Column({ space: Tokens.page }) {
      if (!Object.keys(this.plan(item)).length) {
        Text(tr('assistant_feed_plan_unavailable')).fontSize(Tokens.body).fontColor($r('sys.color.font_secondary'))
      } else {
        if (!this.feed.planIsFresh(this.planId(item))) {
          if (this.feed.planLoading(this.planId(item))) LoadingProgress().width(Tokens.touch).height(Tokens.touch)
          else Row({ space: Tokens.gap }) {
            Text(tr(this.feed.planActionError(this.planId(item)) || this.feed.error || 'assistant_detail_plan_refresh'))
              .fontSize(Tokens.meta).fontColor(Colors.secondary).layoutWeight(1)
            Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('retry')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('retry', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('retry')).bindTips(tr('retry')).onClick(() => this.feed.refreshPlan(this.planId(item)))
          }.width('100%')
        }
        Row({ space: Tokens.gap }) {
          if (!this.detailOnly) Text(tr('assistant_feed_plan')).fontSize(Tokens.body).fontWeight(FontWeight.Medium).layoutWeight(1)
          else Blank().layoutWeight(1)
          Text(tr('assistant_feed_plan_' + (['open', 'started', 'dismissed'].includes(this.planState(item)) ? this.planState(item) : 'unknown')))
            .fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
        }.width('100%')
        ForEach(this.planTasks(item), (task: Record<string, Object>, index: number) => {
          this.plannedTask(task, index)
        }, (task: Record<string, Object>, index: number) => String(index) + ':' + JSON.stringify(task))
        if (this.planState(item) === 'open') {
          ComposeListItemV2({ contentItemV2: new ContentItemV2({ primaryText: tr('assistant_feed_remember') }),
            operateItemV2: new OperateItemV2({ checkbox: new OperateCheckV2({ isCheck: this.feed.rememberPlan(this.planId(item)),
              accessibilityText: tr('assistant_feed_remember'), onChange: (_checked: boolean) => this.feed.toggleRememberPlan(this.planId(item)) }) }) })
          Row({ space: Tokens.inset }) {
            Button({ type: ButtonType.Circle }) { SymbolGlyph(Symbols.edit).fontSize(Tokens.symbolFont).fontColor([Colors.link]) }
              .attributeModifier(ButtonStyles.chrome).accessibilityText(tr('edit')).enabled(this.feed.planCanEdit(this.planId(item)))
              .onClick(() => this.feed.editPlan(this.planId(item))).id('assistant.plan.edit')
            Button({ type: ButtonType.Circle }) { Image(Symbols.more).width(Tokens.symbol).height(Tokens.symbol).fillColor(Colors.link) }
              .attributeModifier(ButtonStyles.chrome).accessibilityText(tr('more')).enabled(this.feed.planCanDismiss(this.planId(item)))
              .bindMenu(this.planMenu(item)).id('assistant.plan.more')
            Blank().layoutWeight(1)
            Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('assistant_feed_start')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('assistant_feed_start', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('assistant_feed_start')).bindTips(tr('assistant_feed_start')).id('assistant.plan.start')
              .enabled(this.feed.planCanStart(this.planId(item)) && this.planTasks(item).length > 0)
              .onClick(() => this.feed.startPlan(this.planId(item), this.feed.rememberPlan(this.planId(item)), undefined,
                JSON.stringify(this.plan(item))))
          }.width('100%')
        }
        if (this.feed.planActionError(this.planId(item)) && this.feed.planIsFresh(this.planId(item))) Row({ space: Tokens.gap }) {
          Text(tr(this.feed.planActionError(this.planId(item)))).fontSize(Tokens.meta).fontColor(Colors.needs).layoutWeight(1)
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('refresh')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('refresh', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('refresh')).bindTips(tr('refresh')).enabled(!this.feed.planLoading(this.planId(item)))
            .onClick(() => this.feed.refreshPlan(this.planId(item))).accessibilityText(tr('refresh'))
        }.width('100%')
      }
    }.width('100%').padding(Tokens.page).alignItems(HorizontalAlign.Start)
      .backgroundColor(Colors.canvas)
  }

  @Builder plannedTask(task: Record<string, Object>, index: number) {
    Column({ space: Tokens.page }) {
      Text((index + 1).toString() + '. ' + Wire.text(task, 'title', tr('assistant_feed_task')))
        .fontSize(Tokens.title).fontWeight(FontWeight.Medium).fontColor(Colors.ink).width('100%')
      Flex({ wrap: FlexWrap.Wrap, space: { main: LengthMetrics.vp(Tokens.gap), cross: LengthMetrics.vp(Tokens.gap) } }) {
        if (Wire.text(task, 'machine')) Text(this.machineLabel(Wire.text(task, 'machine')))
          .fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary')).constraintSize({ maxWidth: '100%' })
        if (Wire.text(task, 'cwd')) Text(Wire.text(task, 'cwd')).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary')).constraintSize({ maxWidth: '100%' })
        if (Wire.bool(task, 'readonly')) Text(tr('read_only')).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary')).constraintSize({ maxWidth: '100%' })
        else if (Wire.text(task, 'permission')) Text(tr('assistant_feed_permission_' + Wire.text(task, 'permission')))
          .fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary')).constraintSize({ maxWidth: '100%' })
      }.width('100%')
      if (Wire.text(task, 'why')) Text(Wire.text(task, 'why')).fontSize(Tokens.meta)
        .fontColor($r('sys.color.font_secondary')).width('100%')
      if (this.taskGoal(task) || this.taskOutOfBounds(task).length || this.taskDoneWhen(task).length || this.taskBriefSource(task).length)
        Divider().strokeWidth(Tokens.hairline).color(Colors.divider)
      if (this.taskGoal(task)) Column({ space: Tokens.fine }) {
        Text(tr('assistant_detail_goal')).fontSize(Tokens.meta).fontWeight(FontWeight.Medium).fontColor(Colors.ai).width('100%')
        Text(this.taskGoal(task)).fontSize(Tokens.body).width('100%')
      }.width('100%').alignItems(HorizontalAlign.Start)
      if (this.taskOutOfBounds(task).length > 0) Column({ space: Tokens.fine }) {
        Text(tr('assistant_detail_out_of_bounds')).fontSize(Tokens.meta).fontWeight(FontWeight.Medium).fontColor(Colors.ink).width('100%')
        ForEach(this.taskOutOfBounds(task), (value: string, valueIndex: number) =>
          Text(value).fontSize(Tokens.body).width('100%'), (_value: string, valueIndex: number) => String(valueIndex))
      }.width('100%').alignItems(HorizontalAlign.Start)
      if (this.taskDoneWhen(task).length > 0) Column({ space: Tokens.fine }) {
        Text(tr('assistant_detail_done_when')).fontSize(Tokens.meta).fontWeight(FontWeight.Medium).fontColor(Colors.ink).width('100%')
        ForEach(this.taskDoneWhen(task), (value: string, valueIndex: number) =>
          Text(value).fontSize(Tokens.body).width('100%'), (_value: string, valueIndex: number) => String(valueIndex))
      }.width('100%').alignItems(HorizontalAlign.Start)
      if (this.taskBriefSource(task).length > 0) Column({ space: Tokens.fine }) {
        Text(tr('assistant_detail_brief')).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
        ForEach(this.taskBriefSource(task), (value: string, valueIndex: number) =>
          Text(value).fontSize(Tokens.body).width('100%'), (_value: string, valueIndex: number) => String(valueIndex))
      }.width('100%').alignItems(HorizontalAlign.Start)
      if (Wire.text(task, 'prompt') && (this.taskGoal(task) || this.taskOutOfBounds(task).length || this.taskDoneWhen(task).length || this.taskBriefSource(task).length))
        Divider().strokeWidth(Tokens.hairline).color(Colors.divider)
      if (Wire.text(task, 'prompt')) MarkdownText({ text: Wire.text(task, 'prompt'), onLink: (address: string) => this.onLink(address) })
      if (Wire.text(task, 'error')) Text(Wire.text(task, 'error')).fontSize(Tokens.meta).fontColor($r('app.color.persona_danger'))
    }.width('100%').alignItems(HorizontalAlign.Start).padding({ top: Tokens.gap, bottom: Tokens.gap })
  }

  private sessionProgress(todoId: string, agent: Record<string, Object>): string {
    const row = this.feed.openAgent(todoId, agent);
    if (!row) return '';
    const detail: string[] = [];
    const place = [this.machineLabel(row.machine), row.path].filter((value: string) => !!value).join(' · ');
    if (place) detail.push(place);
    if (row.detail) detail.push(row.detail);
    const plan = Wire.object(row.data(), 'plan');
    const total = Wire.number(plan, 'total'), done = Wire.number(plan, 'done');
    if (total > 0 && done >= 0) detail.push(tr('assistant_feed_progress') + ' ' + String(done) + '/' + String(total));
    return detail.join(' · ');
  }

  @Builder taskRow(card: Record<string, Object>) {
    Column({ space: Tokens.fine }) {
      ComposeListItemV2({ contentItemV2: new ContentItemV2({
        primaryText: Wire.text(this.taskTodo(card), 'title', tr('assistant_feed_task')),
        secondaryText: this.stateLabel(Wire.text(card, 'state', 'unknown'))
      }) }).onClick(() => this.openTodo(this.todoId(card))).accessibilityText(tr('assistant_feed_open_todo'))
      ForEach(this.activeAgents(card), (agent: Record<string, Object>) => this.agentRow(this.todoId(card), agent),
        (agent: Record<string, Object>) => Wire.text(agent, 'machine_id') + '/' + Wire.text(agent, 'session_id'))
      if (this.feed.todoError) Text(tr(this.feed.todoError)).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary')).padding({ left: Tokens.page, right: Tokens.page })
    }.width('100%')
  }

  @Builder agentRow(todoId: string, agent: Record<string, Object>) {
    if (this.feed.openAgent(todoId, agent)) {
      ComposeListItemV2({ contentItemV2: new ContentItemV2({
        primaryText: this.agentTitle(todoId, agent),
        secondaryText: this.sessionProgress(todoId, agent) || Wire.text(agent, 'agent') || this.agentMachine(todoId, agent)
      }) }).onClick(() => this.openAgentSession(todoId, agent)).accessibilityText(tr('assistant_feed_open_agent'))
    }
  }

  @Builder taskDock() {
    if (this.tasks().length > 0) {
      Column({ space: Tokens.gap }) {
        Text(tr('assistant_feed_task_dock')).fontSize(Tokens.body).fontWeight(FontWeight.Medium).padding({ left: Tokens.page })
        ForEach(this.tasks(), (card: Record<string, Object>) => this.taskRow(card), (card: Record<string, Object>) => Wire.text(card, 'id'))
      }.width('100%').alignItems(HorizontalAlign.Start).padding({ top: Tokens.gap, bottom: Tokens.gap })
    }
  }

  private receiptTodo(item: Record<string, Object>): Record<string, Object> { return this.feed.todo(Wire.text(item, 'ref')); }

  private receiptProjection(item: Record<string, Object>): AssistantReceiptProjection {
    const todoId = Wire.text(item, 'ref'), todo = this.receiptTodo(item);
    return projectAssistantReceipt(item, todo, this.feed.todosReady, this.feed.receiptLeadAgent(todoId),
      this.feed.canRetryReceipt(Wire.text(item, 'id'), todoId), this.feed.canContinueReceipt(Wire.text(item, 'id'), todoId), this.model.canApprove());
  }

  @Builder receiptSummary(item: Record<string, Object>) {
    ForEach([this.receiptViewStamp], (_stamp: string) => { this.receiptSummaryContent(item, this.receiptView) }, (stamp: string) => stamp)
  }

  private receiptTone(projection: AssistantReceiptProjection): ResourceColor {
    if (projection.outcome === 'done') return Colors.positive;
    if (projection.outcome === 'failed') return Colors.danger;
    return Colors.secondary;
  }

  @Builder receiptSummaryContent(item: Record<string, Object>, projection: AssistantReceiptProjection) {
    Button({ type: ButtonType.Normal }) {
      Row({ space: Tokens.inset }) {
        SymbolGlyph(projection.outcome === 'done' ? Symbols.check : Symbols.document).fontSize(Tokens.symbolFont)
          .fontColor([this.receiptTone(projection)])
        Column({ space: Tokens.fine }) {
          Row({ space: Tokens.gap }) {
            Text(projection.title || tr('assistant_feed_receipt')).fontSize(Tokens.body).fontWeight(FontWeight.Medium).fontColor(Colors.ink).layoutWeight(1)
            Text(tr('assistant_detail_receipt_' + projection.outcome)).fontSize(Tokens.meta).fontColor(this.receiptTone(projection))
          }.width('100%')
          if (projection.summary) Text(projection.summary).fontSize(Tokens.body).fontColor(Colors.secondary).width('100%').maxLines(3)
        }.layoutWeight(1).alignItems(HorizontalAlign.Start)
        SymbolGlyph(Symbols.expand).fontSize(Tokens.meta).fontColor([Colors.secondary])
      }.width('100%')
    }.buttonStyle(ButtonStyleMode.NORMAL).backgroundColor(Colors.grouped).borderRadius(Tokens.card)
      .padding(Tokens.page).width('100%').enabled(this.active).onClick(() => this.openDetail(item))
      .id('assistant.detail.receipt.' + Wire.text(item, 'id')).accessibilityText(tr('assistant_detail_view_receipt') + ' ' + projection.title + ' ' + tr('assistant_detail_receipt_' + projection.outcome) + ' ' + projection.summary)
  }

  @Builder receiptCriteriaRow(criterion: AssistantReceiptCriterion) {
    Column({ space: Tokens.fine }) {
      Row({ space: Tokens.gap }) {
        Text(criterion.criterion).fontSize(Tokens.body).layoutWeight(1)
        Text(tr('assistant_detail_criterion_' + criterion.state)).fontSize(Tokens.meta)
          .fontColor(criterion.state === 'met' ? Colors.positive : Colors.secondary)
      }.width('100%')
      if (criterion.evidence) Text(criterion.evidence).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
      if (criterion.change) Text(criterion.change).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
    }.width('100%').alignItems(HorizontalAlign.Start).padding({ top: Tokens.gap, bottom: Tokens.gap })
  }

  @Builder receipt(item: Record<string, Object>) {
    ForEach([this.receiptViewStamp], (_stamp: string) => { this.receiptContent(item, this.receiptView, Wire.text(item, 'ref'), this.receiptTodo(item)) }, (stamp: string) => stamp)
  }

  @Builder receiptContent(item: Record<string, Object>, projection: AssistantReceiptProjection, todoId: string,
    todo: Record<string, Object>) {
    Column({ space: Tokens.gap }) {
      Text(projection.title || tr('assistant_feed_receipt')).fontSize(Tokens.body).fontWeight(FontWeight.Medium).width('100%')
      Text(tr('assistant_detail_receipt_' + projection.outcome)).fontSize(Tokens.meta)
        .fontColor(projection.outcome === 'done' ? Colors.positive : Colors.secondary)
      if (projection.continued) Text(tr('assistant_detail_continued')).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
      if (projection.modelServiceInterruption) Text(tr('assistant_detail_model_service_interruption'))
        .fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
      if (projection.summary) MarkdownText({ text: projection.summary, onLink: (address: string) => this.onLink(address) })
      if (projection.error && projection.error !== projection.summary) Text(projection.error).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
      if (projection.pullRequest) MarkdownText({ text: projection.pullRequest, onLink: (address: string) => this.onLink(address) })
      if (projection.criteria.length > 0) {
        Text(tr('assistant_detail_criteria')).fontSize(Tokens.body).fontWeight(FontWeight.Medium).width('100%')
        Text(tr('assistant_detail_criteria_met') + ' ' + String(projection.metCount) + ' / ' + String(projection.criteria.length))
          .fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
        ForEach(projection.criteria, (criterion: AssistantReceiptCriterion) => this.receiptCriteriaRow(criterion),
          (criterion: AssistantReceiptCriterion) => JSON.stringify(criterion))
      }
      if (projection.criteriaMore > 0) Text(tr('assistant_detail_criteria_more') + ' ' + String(projection.criteriaMore))
        .fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
      if (this.feed.todoError) {
        Text(tr(this.feed.todoError)).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%')
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('retry')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('retry', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('retry')).bindTips(tr('retry')).onClick(() => this.feed.refresh())
      }
      if (this.feed.error) Row({ space: Tokens.gap }) {
        Text(tr(this.feed.error)).fontSize(Tokens.meta).fontColor(Colors.needs).layoutWeight(1)
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('refresh')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('refresh', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('refresh')).bindTips(tr('refresh')).onClick(() =>
          this.feed.locateItem(Wire.text(item, 'id'), 'receipt'))
      }.width('100%')
      if (projection.recovery) {
        Button({ type: ButtonType.Circle }) { if (IconActions.busy('assistant_detail_' + projection.recovery)) LoadingProgress().width(Tokens.symbol).height(Tokens.symbol).color(Colors.onAction); else SymbolGlyph(IconActions.symbol('assistant_detail_' + projection.recovery)).fontSize(Tokens.symbolFont).fontColor([IconActions.color('assistant_detail_' + projection.recovery, true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('assistant_detail_' + projection.recovery)).bindTips(tr('assistant_detail_' + projection.recovery))
          .enabled(projection.recovery === 'retry' ? this.feed.canRetryReceipt(Wire.text(item, 'id'), todoId) :
            this.feed.canContinueReceipt(Wire.text(item, 'id'), todoId))
          .onClick(() => this.feed.retryReceipt(Wire.text(item, 'id'), todoId, projection.recovery))
          .id('assistant.detail.recovery.' + Wire.text(item, 'id'))
      }
      if (todoId && Object.keys(todo).length > 0) {
        ComposeListItemV2({ contentItemV2: new ContentItemV2({ primaryText: tr('assistant_detail_open_task') }) })
          .onClick(() => this.openTodo(todoId)).accessibilityText(tr('assistant_detail_open_task'))
        ForEach(this.todoAgents(todoId), (agent: Record<string, Object>) => this.agentRow(todoId, agent),
          (agent: Record<string, Object>) => Wire.text(agent, 'machine_id') + '/' + Wire.text(agent, 'session_id'))
      }
    }.width('100%').alignItems(HorizontalAlign.Start).padding(Tokens.page)
      .backgroundColor(Colors.grouped)
  }

  @Builder answered(item: Record<string, Object>) {
    Column({ space: Tokens.gap }) {
      Text(tr('assistant_feed_answered')).fontSize(Tokens.body).fontWeight(FontWeight.Medium)
      if (Wire.text(this.itemBody(item), 'summary')) Text(Wire.text(this.itemBody(item), 'summary')).fontSize(Tokens.body)
      if (Wire.rows(this.itemBody(item), 'items').length === 0) {
        if (Wire.text(this.itemBody(item), 'question')) Text(Wire.text(this.itemBody(item), 'question')).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
        if (Wire.text(this.itemBody(item), 'answer')) Text(Wire.text(this.itemBody(item), 'answer')).fontSize(Tokens.body)
      }
      ForEach(Wire.rows(this.itemBody(item), 'items'), (entry: Record<string, Object>, index: number) => {
        Column({ space: Tokens.fine }) {
          if (Wire.text(entry, 'question')) Text(Wire.text(entry, 'question')).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
          if (Wire.text(entry, 'answer')) Text(Wire.text(entry, 'answer')).fontSize(Tokens.body)
        }.width('100%').alignItems(HorizontalAlign.Start)
      }, (_entry: Record<string, Object>, index: number) => String(index))
    }.width('100%').alignItems(HorizontalAlign.Start).padding(Tokens.page)
      .backgroundColor($r('sys.color.comp_background_tertiary'))
  }

  @Builder review(item: Record<string, Object>) {
    Column({ space: Tokens.gap }) {
      Text(Wire.text(this.itemBody(item), 'title', tr('assistant_feed_review'))).fontSize(Tokens.body).fontWeight(FontWeight.Medium)
      Text(tr('assistant_feed_review_' + Wire.text(this.itemBody(item), 'verdict', 'unknown'))).fontSize(Tokens.meta)
        .fontColor($r('sys.color.font_secondary'))
      if (Wire.text(this.itemBody(item), 'note')) MarkdownText({ text: Wire.text(this.itemBody(item), 'note'), onLink: (address: string) => this.onLink(address) })
      ForEach(Wire.rows(this.itemBody(item), 'checks'), (check: Record<string, Object>, index: number) => {
        Row({ space: Tokens.gap }) {
          Text(Wire.text(check, 'criterion')).layoutWeight(1)
          Text(tr('assistant_feed_check_' + (Wire.bool(check, 'met') ? 'met' : 'not_met'))).fontColor($r('sys.color.font_secondary'))
        }.width('100%')
      }, (_check: Record<string, Object>, index: number) => String(index))
    }.width('100%').alignItems(HorizontalAlign.Start).padding(Tokens.page)
      .backgroundColor($r('sys.color.comp_background_tertiary'))
  }

  @Builder routed(item: Record<string, Object>) {
    Column({ space: Tokens.gap }) {
      Text(Wire.text(this.itemBody(item), 'from') === 'assistant' ? tr('assistant_feed_persona_message') : tr('assistant_feed_task_message'))
        .fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
      if (Wire.text(this.itemBody(item), 'text')) MarkdownText({ text: Wire.text(this.itemBody(item), 'text'), onLink: (address: string) => this.onLink(address) })
      if (Wire.bool(this.itemBody(item), 'interrupted')) Text(tr('assistant_feed_interrupted')).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
    }.width('100%').alignItems(HorizontalAlign.Start).padding(Tokens.page)
      .backgroundColor($r('sys.color.comp_background_tertiary'))
  }

  @Builder report(item: Record<string, Object>) {
    Column({ space: Tokens.gap }) {
      Text(tr('assistant_feed_report')).fontSize(Tokens.body).fontWeight(FontWeight.Medium)
      if (this.reportText(item)) MarkdownText({ text: this.reportText(item), onLink: (address: string) => this.onLink(address) })
      else Text(tr('assistant_feed_report_unavailable')).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
    }.width('100%').alignItems(HorizontalAlign.Start).padding(Tokens.page)
      .backgroundColor($r('sys.color.comp_background_tertiary'))
  }

  @Builder message(item: Record<string, Object>) {
    Column({ space: Tokens.gap }) {
      Text([Wire.text(this.itemBody(item), 'sender'), Wire.text(this.itemBody(item), 'chat')].filter((value: string) => !!value).join(' · ') || tr('assistant_feed_message'))
        .fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary')).width('100%')
      if (Wire.text(this.itemBody(item), 'text')) MarkdownText({ text: Wire.text(this.itemBody(item), 'text'), onLink: (address: string) => this.onLink(address) })
      if (this.messageTodoId(item) && Object.keys(this.messageTodo(item)).length > 0) {
        ComposeListItemV2({ contentItemV2: new ContentItemV2({ primaryText: Wire.text(this.messageTodo(item), 'title'),
          secondaryText: this.stateLabel(this.todoState(this.messageTodoId(item))) }) }).onClick(() => this.openMessageTodo(item))
      }
      Text(tr('assistant_feed_message_' + this.messageState(item))).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
      if (this.messageState(item) === 'sent') {
        Text(Wire.text(this.itemBody(item), 'sent', Wire.text(this.itemBody(item), 'draft'))).fontSize(Tokens.body).width('100%')
      } else {
        TextArea({ text: this.feed.messageDraft(item), placeholder: tr('assistant_feed_draft') })
          .height(Tokens.inputExpanded).enabled(this.messageState(item) === 'open' && !this.feed.busyKeys.includes('message:' + this.messageId(item)))
          .onChange((value: string) => this.feed.setMessageDraft(this.messageId(item), value)).id('assistant-feed.draft.' + this.messageId(item))
      }
      if (this.messageState(item) === 'open') {
        Row({ space: Tokens.gap }) {
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('assistant_feed_send')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('assistant_feed_send', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('assistant_feed_send')).bindTips(tr('assistant_feed_send')).enabled(this.feed.messageCanSend(item)).onClick(() => this.confirmSend(item))
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('assistant_feed_dismiss')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('assistant_feed_dismiss', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('assistant_feed_dismiss')).bindTips(tr('assistant_feed_dismiss')).enabled(this.feed.messageCanDismiss(item))
            .onClick(() => this.feed.dismissMessage(this.messageId(item)))
        }.width('100%')
      } else if (this.messageState(item) === 'pending') {
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('refresh')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('refresh', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('refresh')).bindTips(tr('refresh')).onClick(() => this.feed.refresh())
      } else if (this.messageState(item) === 'dismissed' && this.feed.messageCanUndo(item)) {
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('assistant_feed_undo_dismiss')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('assistant_feed_undo_dismiss', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('assistant_feed_undo_dismiss')).bindTips(tr('assistant_feed_undo_dismiss')).onClick(() => this.feed.undoDismissMessage(this.messageId(item)))
      }
      if (Wire.text(this.itemBody(item), 'filed_at')) Text(tr('assistant_feed_filed_elsewhere')).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
      if (Wire.text(item, 'ref')) Button({ type: ButtonType.Normal }) { Row({ space: Tokens.inset }) { Text(tr('assistant_feed_open_inbox')).fontSize(Tokens.body).fontColor(Colors.ink).layoutWeight(1); SymbolGlyph($r('sys.symbol.chevron_right')).fontSize(Tokens.meta).fontColor([Colors.secondary]) }.width('100%').constraintSize({ minHeight: Tokens.touch }) }.buttonStyle(ButtonStyleMode.TEXTUAL).backgroundColor(Color.Transparent).width('100%').padding({ left: Tokens.page, right: Tokens.page }).onClick(() => this.openInbox(item))
    }.width('100%').alignItems(HorizontalAlign.Start).padding(Tokens.page)
      .backgroundColor($r('sys.color.comp_background_tertiary'))
  }

  private todoState(todoId: string): string {
    const card = this.tasks().find((value: Record<string, Object>) => Wire.text(value, 'todo_id') === todoId);
    return card ? Wire.text(card, 'state', 'unknown') : 'unknown';
  }

  private confirmSend(item: Record<string, Object>): void {
    const id = Wire.text(item, 'id'), body = Wire.object(item, 'body'), draft = this.feed.messageDraft(item);
    const destination = [Wire.text(body, 'source'), Wire.text(body, 'sender'), Wire.text(body, 'chat')].filter((value: string) => !!value).join(' · ');
    const details = [destination, Wire.text(body, 'text'), draft].filter((value: string) => !!value).join('\n\n');
    this.getUIContext().showAlertDialog({ title: tr('assistant_feed_confirm_send'), message: details,
      primaryButton: { value: tr('cancel'), action: () => {} },
      secondaryButton: { value: tr('assistant_feed_send'), action: () => this.feed.sendMessage(id, draft) } });
  }

  private openInbox(item: Record<string, Object>): void {
    const body = Wire.object(item, 'body'), id = Wire.text(item, 'ref');
    if (!id || !this.feed.owner) return;
    const row = new ResourceRow(id, Wire.text(body, 'sender', Wire.text(body, 'chat', id)), 'inbox');
    row.manager = this.feed.owner; row.raw = JSON.stringify(body);
    this.onOpenInbox(row);
  }

  private openCardResource(address: string): void {
    if (!this.active) return;
    const row = assistantCardResource(address, this.model.profile.id, this.feed.owner, this.model.sessions.concat(this.model.conversations));
    if (!row) { this.model.error = 'assistant_view_invalid_target'; return; }
    if (!row.title) row.title = tr(row.kind === 'todo' ? 'todos' : row.kind === 'knowledge' ? 'knowledge' : row.kind === 'conversation' ? 'main_chat' : 'agents');
    this.onOpenResource(row);
  }
  private cardKey(item: Record<string, Object>): string { return Wire.text(item, 'ref', Wire.text(Wire.object(this.itemBody(item), 'view'), 'id')); }

  @Builder unsupported(item: Record<string, Object>) {
    Column({ space: Tokens.gap }) {
      Text(tr('assistant_feed_unsupported_activity')).fontSize(Tokens.body)
    }.width('100%').alignItems(HorizontalAlign.Start).padding(Tokens.page)
      .backgroundColor($r('sys.color.comp_background_tertiary'))
  }

  @Builder activity(item: Record<string, Object>) {
    if (Wire.text(item, 'kind') === 'view') AssistantViewCard({ model: this.model, feed: this.feed, item: item, cardKey: this.cardKey(item), active: this.active, onLink: (address: string) => this.onLink(address), onOpenResource: (address: string) => this.openCardResource(address) })
    else if (Wire.text(item, 'kind') === 'plan') { if (this.detailOnly) this.planCard(item); else this.planSummary(item) }
    else if (Wire.text(item, 'kind') === 'receipt') { if (this.detailOnly) this.receipt(item); else this.receiptSummary(item) }
    else if (Wire.text(item, 'kind') === 'answered') this.answered(item)
    else if (Wire.text(item, 'kind') === 'review') this.review(item)
    else if (Wire.text(item, 'kind') === 'routed') this.routed(item)
    else if (Wire.text(item, 'kind') === 'report') this.report(item)
    else if (Wire.text(item, 'kind') === 'message') this.message(item)
    else this.unsupported(item)
  }

  private planEditMachineIds(): string[] {
    const ids = this.model.machines.filter((row: ResourceRow) => Wire.text(row.data(), 'trust') !== 'pending' &&
      !Wire.bool(row.data(), 'pending') && !Wire.bool(row.data(), 'pending_confirm')).map((row: ResourceRow) => row.id).filter((id: string) => !!id);
    return ids;
  }

  @Builder editTask(task: PlanEditTask, index: number, ids: string[]) {
    Column({ space: Tokens.gap }) {
      if (this.feed.editTasks.length > 1) Text(tr('assistant_feed_task') + ' ' + String(index + 1))
        .fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
      TextInput({ text: task.title, placeholder: tr('title') }).onChange((value: string) => this.feed.updateEditTask(index, 'title', value))
      if (ids.length > 0) {
        if (!ids.includes(task.machine)) Text(this.machineLabel(task.machine)).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
        Select(ids.map((id: string): SelectOption => ({ value: this.machineLabel(id) })))
          .value(ids.includes(task.machine) ? this.machineLabel(task.machine) : '').onSelect((choice: number) => {
            if (choice >= 0 && choice < ids.length) this.feed.updateEditTask(index, 'machine', ids[choice]);
          })
      } else {
        TextInput({ text: task.machine, placeholder: tr('assistant_feed_machine') })
          .onChange((value: string) => this.feed.updateEditTask(index, 'machine', value))
      }
      TextInput({ text: task.cwd, placeholder: tr('assistant_feed_folder') }).onChange((value: string) => this.feed.updateEditTask(index, 'cwd', value))
      if (task.readonly) Text(tr('read_only')).fontSize(Tokens.meta).fontColor($r('sys.color.font_secondary'))
      else Select(PLAN_PERMISSIONS.map((mode: string): SelectOption => ({ value: tr('assistant_feed_permission_' + mode) })))
        .value(tr('assistant_feed_permission_' + task.permission)).onSelect((choice: number) => {
          if (choice >= 0 && choice < PLAN_PERMISSIONS.length) this.feed.updateEditTask(index, 'permission', PLAN_PERMISSIONS[choice]);
        })
      TextArea({ text: task.prompt, placeholder: tr('assistant_feed_prompt') }).height(Tokens.inputExpanded)
        .onChange((value: string) => this.feed.updateEditTask(index, 'prompt', value))
    }.width('100%').alignItems(HorizontalAlign.Start)
  }

  @Builder planEditor() {
    Column({ space: Tokens.page }) {
      Scroll() {
        Column({ space: Tokens.section }) {
          ForEach(this.feed.editTasks, (task: PlanEditTask, index: number) => this.editTask(task, index, this.planEditMachineIds()),
            (_task: PlanEditTask, index: number) => String(index))
        }.width('100%').alignItems(HorizontalAlign.Start)
      }.layoutWeight(1).width('100%').align(Alignment.TopStart)
      if (this.feed.error) Text(tr(this.feed.error)).fontSize(Tokens.meta).fontColor($r('app.color.persona_danger'))
      Row({ space: Tokens.gap }) {
        Blank().layoutWeight(1)
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('assistant_feed_start')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('assistant_feed_start', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('assistant_feed_start')).bindTips(tr('assistant_feed_start')).id('assistant.plan.editor.start')
          .enabled(this.feed.planCanStart(this.feed.editingPlanId) && this.feed.editTasks.every((task: PlanEditTask) =>
            task.title.trim().length > 0 && task.prompt.trim().length > 0 && task.machine.trim().length > 0 && task.cwd.trim().length > 0 &&
            (task.readonly || task.permission === 'bypassPermissions')))
          .onClick(() => this.feed.startEditedPlan())
      }.width('100%')
    }.width('100%').height('100%').padding({ left: Tokens.page, right: Tokens.page, top: Tokens.page, bottom: this.editorInset + Tokens.page }).alignItems(HorizontalAlign.Start)
      .id(this.editorNodeId).onAreaChange(() => this.measureEditor())
  }

  build() {
    if (this.dockOnly) {
      Column() {
        this.taskDock()
      }.width('100%')
        .bindSheet($$this.feed.editing, this.planEditor(), {
          title: { title: tr('assistant_feed_edit_plan') }, height: SheetSize.LARGE, showClose: true,
          onAppear: () => { this.editorReady = true; this.measureEditor(); }, onDisappear: () => this.editorReady = false,
          onWillDismiss: () => this.feed.cancelPlanEdit()
        })
    } else if (this.item) {
      if (this.detailOnly) {
        Column() { this.activity(this.item) }.width('100%')
          .bindSheet($$this.feed.editing, this.planEditor(), {
            title: { title: tr('assistant_feed_edit_plan') }, height: SheetSize.LARGE, showClose: true,
            onAppear: () => { this.editorReady = true; this.measureEditor(); }, onDisappear: () => this.editorReady = false,
          onWillDismiss: () => this.feed.cancelPlanEdit()
          })
      } else this.activity(this.item)
    }
  }
}
