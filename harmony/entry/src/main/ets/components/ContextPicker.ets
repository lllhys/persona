import { IconActions } from '../design/IconActions';
import { ClientModel } from '../models/ClientModel';
import { HttpFailure } from '../platform/HubTransport';
import { HubRepository, segment } from '../models/HubRepository';
import { ResourceRow, Wire } from '../models/Wire';
import { Tokens } from '../design/Tokens';
import { Colors } from '../design/Colors';
import { ButtonStyles } from '../design/Controls';
import { tr } from '../design/Strings';

const KNOWLEDGE_KINDS: string[] = ['projects', 'chats', 'people', 'docs', 'meetings', 'events', 'recordings'];

@ComponentV2
export struct ContextPicker {
  @Require @Param model: ClientModel;
  @Require @Param kind: string;
  @Param active: boolean = true;
  @Event onSelect: (row: ResourceRow) => void = () => {};
  @Event onRetired: () => void = () => {};
  @Local rows: ResourceRow[] = [];
  @Local query: string = '';
  @Local loading: boolean = true;
  @Local failed: boolean = false;
  @Local errorKey: string = 'failed';
  @Local knowledgeKind: string = 'projects';
  private queryTimer: number = -1;
  private revision: number = 0;
  private target: string = '';
  private scope: string = '';
  private generation: number = 0;
  private repository: HubRepository | undefined;
  private mounted: boolean = false;

  aboutToAppear(): void {
    this.mounted = true; this.target = this.model.target(); this.scope = this.model.scope(); this.generation = this.model.connectionGeneration();
    try { this.repository = this.model.repo(); this.load(); }
    catch (_) { this.mounted = false; this.onRetired(); }
  }
  aboutToDisappear(): void { this.mounted = false; this.revision++; if (this.queryTimer !== -1) clearTimeout(this.queryTimer); }
  @Monitor('active', 'model.profile', 'model.selected', 'model.connected') ownerChanged(): void {
    if (!this.current()) { this.revision++; this.rows = []; this.onRetired(); }
  }
  private current(): boolean {
    return this.mounted && this.active && this.model.connected && this.model.owns(this.generation, this.scope) &&
      this.model.target() === this.target && this.sameRepository();
  }
  private sameRepository(): boolean { try { return this.model.repo() === this.repository; } catch (_) { return false; } }
  private async load(): Promise<void> {
    const repository = this.repository, revision = ++this.revision;
    if (!repository || !this.current()) return;
    this.loading = true; this.failed = false;
    try {
      const rows = this.kind === 'todo' ? await repository.todos() : this.kind === 'knowledge' ? await this.loadKnowledge(repository) : await repository.inbox();
      if (!this.current() || revision !== this.revision) return;
      this.rows = rows.filter((row: ResourceRow) => row.manager === this.model.profile.manager && (!row.group || row.group === this.model.profile.id));
    } catch (error) { if (this.current() && revision === this.revision) { this.failed = true; this.errorKey = error instanceof HttpFailure && error.status === 403 ? 'permission_denied' : 'failed'; } }
    finally { if (this.current() && revision === this.revision) this.loading = false; }
  }
  private knowledgeOptions(): SelectOption[] { return KNOWLEDGE_KINDS.map((kind: string): SelectOption => ({ value: tr('knowledge_kind_' + kind) })); }
  private async loadKnowledge(repository: HubRepository): Promise<ResourceRow[]> {
    if (!this.query.trim()) return repository.knowledge(this.knowledgeKind);
    const result = await repository.call('/api/v1/kb-search?q=' + segment(this.query.trim()) + '&results=1');
    return Wire.rows(result, 'results').map((hit: Record<string, Object>) => {
      const data = Wire.object(hit, 'entry'), row = ResourceRow.from(data, 'knowledge');
      row.path = Wire.text(data, 'kind'); row.manager = Wire.text(data, 'manager', this.model.profile.manager);
      return row;
    }).filter((row: ResourceRow) => !!row.id && KNOWLEDGE_KINDS.includes(row.path));
  }
  private queryChanged(value: string): void {
    this.query = value;
    if (this.kind !== 'knowledge') return;
    this.revision++;
    if (this.queryTimer !== -1) clearTimeout(this.queryTimer);
    this.queryTimer = setTimeout(() => { this.queryTimer = -1; if (this.current()) this.load(); }, 250);
  }
  private visibleRows(): ResourceRow[] {
    const query = this.kind === 'knowledge' ? '' : this.query.trim().toLocaleLowerCase();
    return this.rows.filter((row: ResourceRow) => !query || (row.title + ' ' + row.detail).toLocaleLowerCase().includes(query));
  }
  build() {
    Column({ space: Tokens.gap }) {
      Search({ value: this.query, placeholder: tr('search') }).onChange((value: string) => this.queryChanged(value))
        .margin({ left: Tokens.page, right: Tokens.page }).id('context.search')
      if (this.kind === 'knowledge' && !this.query.trim()) Select(this.knowledgeOptions()).selected(KNOWLEDGE_KINDS.indexOf(this.knowledgeKind)).value(tr('knowledge_kind_' + this.knowledgeKind)).font({ size: Tokens.body }).onSelect((index: number) => { this.knowledgeKind = KNOWLEDGE_KINDS[index]; this.load(); }).margin({ left: Tokens.page, right: Tokens.page })
      if (this.loading) LoadingProgress().width(Tokens.touch).height(Tokens.touch)
      else if (this.failed) {
        Text(tr(this.errorKey)).fontSize(Tokens.meta).fontColor(Colors.secondary).padding(Tokens.page)
        if (this.errorKey !== 'permission_denied') Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('retry')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('retry', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('retry')).bindTips(tr('retry')).onClick(() => this.load()).id('context.retry')
      }
      else {
        List() {
          if (!this.visibleRows().length) ListItem() { Text(tr(this.query ? 'no_results' : 'empty')).fontSize(Tokens.meta).fontColor(Colors.secondary).padding(Tokens.page) }
          ForEach(this.visibleRows(), (row: ResourceRow) => {
            ListItem() { Text(row.title || row.id).fontSize(Tokens.body).fontColor(Colors.ink).padding(Tokens.page).constraintSize({ minHeight: Tokens.touch }).width('100%') }
              .onClick(() => { if (this.current()) this.onSelect(row); }).id('context.' + row.kind + '.' + row.path + '.' + row.id)
          }, (row: ResourceRow) => row.key() + '|' + row.path)
        }.layoutWeight(1).width('100%')
      }
    }.width('100%').height('100%')
  }
}
