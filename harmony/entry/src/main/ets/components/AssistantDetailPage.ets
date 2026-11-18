import { IconActions } from '../design/IconActions';
import { ClientModel } from '../models/ClientModel';
import { AssistantFeedModel } from '../models/AssistantFeed';
import { AssistantCards } from './AssistantCards';
import { ResourceRow, Wire } from '../models/Wire';
import type { HubRepository } from '../models/HubRepository';
import { Tokens } from '../design/Tokens';
import { Colors } from '../design/Colors';
import { ButtonStyles } from '../design/Controls';
import { tr } from '../design/Strings';

@ComponentV2
export struct AssistantDetailPage {
  @Require @Param model: ClientModel;
  @Param active: boolean = false;
  @Require @Param itemId: string;
  @Require @Param kind: string;
  @Event onOpenResource: (row: ResourceRow) => void = (_row: ResourceRow) => {};
  @Event onLink: (address: string) => void = (_address: string) => {};
  @Local feed: AssistantFeedModel = new AssistantFeedModel();
  private capturedIdentity: string = '';
  private capturedSource: ResourceRow | undefined;
  private attempt: number = 0;
  private routeActive: boolean = false;

  aboutToAppear(): void { this.syncFeed(); }
  aboutToDisappear(): void {
    this.routeActive = false;
    this.attempt++;
    this.feed.close();
    this.capturedIdentity = '';
    this.capturedSource = undefined;
  }

  @Monitor('active', 'model.selected', 'model.profile', 'model.connected', 'itemId', 'kind')
  contextChanged(): void { this.syncFeed(); }

  private selectedConversation(): ResourceRow | undefined {
    const selected = this.model.selected;
    if (selected.kind !== 'conversation' || !selected.id) return undefined;
    const row = new ResourceRow(selected.id, selected.title, selected.kind);
    row.manager = selected.manager;
    row.group = selected.group;
    row.machine = selected.machine;
    row.session = selected.session;
    row.path = selected.path;
    row.state = selected.state;
    row.detail = selected.detail;
    row.raw = selected.raw;
    return row;
  }

  private currentIdentity(source: ResourceRow): string {
    return JSON.stringify([this.model.scope(), this.model.connectionGeneration(), this.model.target(source),
      source.key(), this.model.profile.manager, this.itemId, this.kind]);
  }

  private capturedContextIsCurrent(identity: string, source: ResourceRow, generation: number, scope: string,
    target: string, repository: HubRepository): boolean {
    if (!this.routeActive || !this.active || identity !== this.capturedIdentity || this.model.scope() !== scope ||
      !this.model.owns(generation, scope) || this.model.selected.key() !== source.key() || this.model.target(source) !== target) return false;
    try { return this.model.repo() === repository; } catch (_) { return false; }
  }

  private syncFeed(): void {
    if (!this.active || !this.model.connected || this.model.demo || !['plan', 'receipt'].includes(this.kind) || !this.itemId) {
      this.routeActive = false;
      this.attempt++;
      this.capturedIdentity = '';
      this.capturedSource = undefined;
      this.feed.close();
      return;
    }
    const source = this.selectedConversation();
    if (!source) {
      this.routeActive = false;
      this.attempt++;
      this.capturedIdentity = '';
      this.capturedSource = undefined;
      this.feed.close();
      this.feed.itemLookupStatus = 'error';
      this.feed.error = 'request_updated';
      return;
    }
    const identity = this.currentIdentity(source);
    if (identity === this.capturedIdentity && this.routeActive) return;

    let repository: HubRepository;
    try { repository = this.model.repo(); }
    catch (_) {
      this.routeActive = false;
      this.capturedIdentity = '';
      this.feed.close();
      this.feed.error = 'offline';
      this.feed.itemLookupStatus = 'error';
      return;
    }
    const generation = this.model.connectionGeneration(), scope = this.model.scope();
    const target = this.model.target(source), sourceKey = source.key(), attempt = ++this.attempt;
    this.capturedIdentity = identity;
    this.capturedSource = source;
    this.routeActive = true;
    const admitted = (): boolean => this.capturedContextIsCurrent(identity, source, generation, scope, target, repository);
    this.feed.open(this.model, source, false, admitted).then(() => {
      if (attempt !== this.attempt || !admitted()) return;
      if (this.feed.error) {
        this.feed.itemLookupStatus = 'error';
        return;
      }
      this.lookup(identity, sourceKey, generation, scope, target, repository, attempt);
    }).catch(() => {
      if (attempt === this.attempt && admitted()) {
        this.feed.error = 'failed';
        this.feed.itemLookupStatus = 'error';
      }
    });
  }

  private lookup(identity: string, sourceKey: string, generation: number, scope: string, target: string,
    repository: HubRepository, attempt: number): void {
    const source = this.capturedSource;
    if (!source || source.key() !== sourceKey || attempt !== this.attempt ||
      !this.capturedContextIsCurrent(identity, source, generation, scope, target, repository)) return;
    this.feed.locateItem(this.itemId, this.kind).catch(() => undefined);
  }

  private retryLookup(): void {
    const source = this.capturedSource;
    if (!this.active || !this.routeActive || !source || !this.itemId || !['plan', 'receipt'].includes(this.kind)) return;
    const identity = this.capturedIdentity, generation = this.model.connectionGeneration(), scope = this.model.scope();
    let repository: HubRepository;
    try { repository = this.model.repo(); }
    catch (_) { this.feed.error = 'offline'; return; }
    const target = this.model.target(source), sourceKey = source.key(), attempt = ++this.attempt;
    this.feed.itemLookupStatus = 'loading';
    this.feed.error = '';
    this.lookup(identity, sourceKey, generation, scope, target, repository, attempt);
  }

  private currentItem(): Record<string, Object> | undefined {
    return this.feed.items.find((row: Record<string, Object>) =>
      Wire.text(row, 'id') === this.itemId && Wire.text(row, 'kind') === this.kind);
  }

  @Builder unavailable() {
    Column({ space: Tokens.gap }) {
      if (this.feed.itemLookupStatus === 'loading') LoadingProgress().width(Tokens.touch).height(Tokens.touch)
      else {
        Text(tr(this.feed.itemLookupStatus === 'missing' || this.feed.itemLookupStatus === 'mismatch' ?
          'assistant_detail_not_found' : this.feed.error || 'failed'))
          .fontSize(Tokens.body).fontColor(Colors.secondary).width('100%')
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('retry')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('retry', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('retry')).bindTips(tr('retry')).onClick(() => this.retryLookup()).id('assistant.detail.retry')
      }
    }.width('100%').alignItems(HorizontalAlign.Center).padding(Tokens.page)
  }

  build() {
    Column() {
      Scroll() {
        Column({ space: Tokens.gap }) {
          if (!this.active || !this.routeActive || this.feed.itemLookupStatus !== 'found' || !this.currentItem()) this.unavailable()
          else AssistantCards({ model: this.model, feed: this.feed, item: this.currentItem(), detailOnly: true,
            active: this.active && this.model.connected, onOpenTodo: (row: ResourceRow) => { if (this.active && this.routeActive) this.onOpenResource(row); }, onOpenSession: (row: ResourceRow) => { if (this.active && this.routeActive) this.onOpenResource(row); }, onOpenResource: (row: ResourceRow) => {
              if (this.active && this.routeActive) this.onOpenResource(row);
            }, onLink: (address: string) => { if (this.active && this.routeActive) this.onLink(address); } })
        }.width('100%').constraintSize({ maxWidth: Tokens.readingMax }).alignItems(HorizontalAlign.Start)
      }.width('100%').layoutWeight(1).scrollBar(BarState.Auto).align(Alignment.Top)
    }.width('100%').height('100%').backgroundColor(Colors.canvas)
  }
}
