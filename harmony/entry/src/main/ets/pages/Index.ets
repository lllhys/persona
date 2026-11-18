import { IconActions } from '../design/IconActions';
import { ThemeScope } from '../design/ThemeScope';
import { util } from '@kit.ArkTS';
import { AdminWorkspacePage } from '../components/AdminWorkspacePage';
import { ExtensionPage } from '../components/ExtensionPage';
import { ExtensionNavigationPage } from '../components/ExtensionNavigationPage';
import { ExtensionNavigation, ExtensionNavigationEntry } from '../models/ExtensionNavigation';
import { extensionIcon } from '../design/ExtensionIcons';
import type { ExtensionUINavigationEffect } from '../models/ExtensionUI';
import { ComputerScreenPage } from '../components/ComputerScreenPage';
import { isPairingAddress } from '../core/WireCompatibility';
import { scanBarcode, scanCore } from '@kit.ScanKit';
import { BusinessError } from '@kit.BasicServicesKit';
import { RemoteDocumentPage } from '../components/RemoteDocumentPage';
import { TodoWorkspacePage } from '../components/TodoWorkspacePage';
import { KnowledgeWorkspacePage } from '../components/KnowledgeWorkspacePage';
import { HealthWorkspacePage } from '../components/HealthWorkspacePage';
import { NavigationContext } from '../models/NavigationContext';
import { LinkReader } from '../components/LinkReader';
import { readerLink, readerHost } from '../platform/ReaderLink';
import { common } from '@kit.AbilityKit';
import { DevicePage } from '../components/DevicePage';
import { deviceFeatures } from '../models/DeviceFeatures';
import { SessionTools } from '../components/SessionTools';
import { SessionWorkspace } from '../models/SessionWorkspace';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { ComposeListItemV2, ContentItemV2, KeyboardAvoidMode, TextModifier, SymbolGlyphModifier, CapsuleSegmentButtonV2, SegmentButtonV2Items, ColorMetrics, LengthMetrics, uiMaterial } from '@kit.ArkUI';
import { ConfigurationConstant } from '@kit.AbilityKit';
import { ClientModel, client } from '../models/ClientModel';
import { ResourceRow, HubProfile, Wire } from '../models/Wire';
import { BoundRow, reconcileBoundRows } from '../models/RowBinding';
import { Pairing, PairingTarget } from '../platform/Pairing';
import { Conversation } from '../components/Conversation';
import { AgentListPage } from '../components/AgentListPage';
import { InteractionHistoryPage } from '../components/InteractionHistoryPage';
import { InboxWorkspacePage } from '../components/InboxWorkspacePage';
import { ResourcePage } from '../components/ResourcePage';
import { SettingsPage } from '../components/SettingsPage';
import { SshSettingsPage } from '../components/SshSettingsPage';
import { DeviceLook, deviceLook } from '../models/DeviceLook';
import { nativeLookTheme } from '../design/LookTheme';
import { IconTypeV2 } from '@kit.ArkUI';
import { Tokens } from '../design/Tokens';
import { Colors } from '../design/Colors';
import { Symbols } from '../design/Symbols';
import { ButtonStyles } from '../design/Controls';
import { AgentWorkspace } from '../models/AgentWorkspace';
import { nativeEnvironment, NativeEnvironment } from '../platform/NativeEnvironment';
import { nativeSheetInset } from '../platform/NativeInsets';
import { mainConversationData } from '../core/DefaultConversation';
import { AssistantDetailPage } from '../components/AssistantDetailPage';
import { tr } from '../design/Strings';
import { isHubStateKey } from '../core/StorageOwnership';

@Entry
@ComponentV2
struct Index {
  @Local model: ClientModel = client;
  @Local look: DeviceLook = deviceLook;
  @Local detailModel: ClientModel = client;
  @Local chatReady: boolean = true;
  @Local detailRoute: string = '';
  @Local detailSelection: string = '';
  private detailOwner: ClientModel | undefined;
  @Local adminRefreshRevision: number = 0;
  @Local inboxRefreshRevision: number = 0;
  @Local extensionTitle: string = '';
  @Local todoTitle: string = '';
  @Local extensionRefreshRevision: number = 0;
  @Local applications: ExtensionNavigation = new ExtensionNavigation();
  @Local shownName: string = '';
  @Local shownParam: string = '';
  @Local chatMenuActions: SheetInfo[] = [];
  @Local chatTitleMenuVisible: boolean = false;
  @Local chatBodyMenuVisible: boolean = false;
  @Local chatMenuRevision: number = 0;
  private routeRevision: number = 0;
  @Local stack: NavPathStack = new NavPathStack();
  @Local breakpoint: WidthBreakpoint = WidthBreakpoint.WIDTH_SM;
  @Local collapsed: boolean = false;
  @Local query: string = '';
  @Local filter: number = 0;
  @Local conversationRowBindings: BoundRow<ResourceRow>[] = [];
  private conversationBindingOwner: Object | undefined;
  private conversationBindingScope: string = '';
  @Local visibilityEpoch: number = 0;
  private rowVisibility: Map<string, number> = new Map();
  @Local readerAddress: string = '';
  @Local readerTitle: string = '';
  @Local readerShown: boolean = false;
  @Local sheetKind: string = '';
  @Local showSheet: boolean = false;
  @Local address: string = '';
  @Local pairCode: string = '';
  private pairingRevision: number = 0;
  @Local scanning: boolean = false;
  private scanRevision: number = 0;
  @Local resourceKind: string = 'todo';
  @Local newTask: string = '';
  @Local selectedMachine: number = 0;
  @Local creatingAgent: boolean = false;
  @Local agentModel: ClientModel = client;
  @Local agentTodo: ResourceRow | undefined = undefined;
  private agentFormRevision: number = 0;
  @Local launcher: string = '';
  @Local directory: string = '';
  @Local settingsPage: string = '';
  @Local activeChild: string = '';
  @Local tool: number = 0;
  private titleMaterial: uiMaterial.Material = uiMaterial.isImmersiveMaterialSupported() ? new uiMaterial.ImmersiveMaterial({ style: uiMaterial.ImmersiveStyle.THIN }) : uiMaterial.Material.empty;
  @Local agentWorkspace: AgentWorkspace = new AgentWorkspace();
  @Local environment: NativeEnvironment = nativeEnvironment;
  @Local spacesShown: boolean = false;
  @Local spacesInset: number = 0;
  private spacesLayoutReady: boolean = false;
  @Local inlineStopIdentity: string = '';
  private defaultMainAttempt: string = '';
  private navigationRestoredScope: string = '';
  private navigationIntent: number = 0;
  private mainReadWaiters: (() => void)[] = [];
  private defaultMainPending: boolean = false;
  private creatingConversations: Map<string, number> = new Map();
  private paths: Map<string, NavPathStack> = new Map();
  private activeScope: string = '';
  private restoring: number = 0;
  private peer(): number { return this.model.section === 1 ? 1 : 0; }
  private headerHeight(): number { return Tokens.header * Math.max(1, this.environment.fontScale / 2); }
  @Local pendingInboxHandoff: string = '';
  @Local mainRowVisible: number = 0;
  private mainDestination(name: string, param: Object): boolean {
    if (name !== 'conversation') return false;
    const row = this.routeRow(param);
    return name === 'conversation' && Wire.bool(row.data(), 'navigation_root') && row.id === this.model.mainId && (!row.group || row.group === this.model.profile.id) &&
      (!row.manager || row.manager === this.model.profile.manager) && (!this.model.mainMachineId || row.machine === this.model.mainMachineId);
  }
  private hideChatTitle(name: string, param: Object): boolean {
    if (name !== 'session' && name !== 'conversation') return false;
    if (!Wire.bool(this.routeRow(param).data(), 'navigation_root')) return false;
    if (name === 'session') return this.hideAgentTitle();
    const row = this.routeRow(param), epoch = this.visibilityEpoch;
    const visible = row.id === this.model.mainId && row.machine === this.model.mainMachineId ? this.mainRowVisible : this.rowVisibility.get(this.model.scope() + this.model.target(row)) || 0;
    return name === 'conversation' && this.detailModel === this.model && this.peer() === 0 && this.wide() && !this.rail() &&
      this.model.target() === this.model.target(row) && visible >= 0.99;
  }
  @Builder peerTitle(_main: boolean = false) {
    Row({ space: Tokens.gap }) {
      CapsuleSegmentButtonV2({ items: new SegmentButtonV2Items([
        { symbol: Symbols.persona, accessibilityText: tr('persona') },
        { symbol: Symbols.agents, accessibilityText: tr('agents') }]),
        selectedIndex: this.peer(), $selectedIndex: (index: number) => this.selectPeer(index),
        itemSelectedBackgroundColor: ColorMetrics.resourceColor(Colors.action),
        itemSymbolFontColor: ColorMetrics.resourceColor(Colors.secondary),
        itemSelectedSymbolFontColor: ColorMetrics.resourceColor(Colors.onAction),
        itemSymbolFontSize: LengthMetrics.vp(Tokens.symbol),
        buttonBackgroundColor: ColorMetrics.resourceColor(Colors.control),
        itemMinHeight: LengthMetrics.vp(Tokens.touch), buttonMinHeight: LengthMetrics.vp(Tokens.touch) })
        .width(Tokens.headerPeerIcons).id('shell.peer')
      Blank().layoutWeight(1)
      this.shellMenu()
    }.width('100%').height(this.headerHeight()).padding({ left: Tokens.page }).alignItems(VerticalAlign.Center)
  }
  private selectPeer(index: number): void {
    if (index === this.model.section) return;
    this.spacesShown = false; this.changeSection(index);
    if (index === 0) this.ensureMainDestination();
  }
  private ensureAgentWorkspace(): void {
    if (!this.agentWorkspace.isBoundTo(this.model)) this.agentWorkspace.open(this.model, this.peer() === 1).catch(() => {});
  }
  @Monitor('model.ready', 'model.mainId', 'model.mainMachineId', 'model.connected') mainAvailabilityChanged(): void { this.ensureMainDestination(); }
  private ensureMainDestination(): void {
    if (!this.model.ready || !this.model.connected || this.model.section !== 0 || this.navigationRestoredScope !== this.model.scope() || this.showSheet || this.spacesShown || this.stack.size() || this.defaultMainPending || (this.creatingConversations.get(JSON.stringify([this.model.scope(), this.model.connectionGeneration()])) || 0) > 0) return;
    const attempt = JSON.stringify([this.model.scope(), this.model.connectionGeneration(), this.model.mainId, this.model.mainMachineId]);
    if (attempt === this.defaultMainAttempt) return;
    this.defaultMainAttempt = attempt; this.resolveMainDestination();
  }
  private async resolveMainDestination(): Promise<void> {
    if (this.defaultMainPending || !this.model.connected) return;
    const owner = this.model, repo = owner.repo(), generation = owner.connectionGeneration(), scope = owner.scope();
    const stack = this.stack, revision = this.routeRevision, selected = owner.selected.key(), section = owner.section, intent = this.navigationIntent;
    const names = stack.getAllPathName(), pathStamp = JSON.stringify(names.map((name: string, index: number) => [name, stack.getParamByIndex(index)]));
    const valid = (): boolean => this.model === owner && owner.connected && owner.owns(generation, scope) && owner.repo() === repo && this.stack === stack &&
      this.routeRevision === revision && this.navigationIntent === intent && !this.showSheet && !this.spacesShown && owner.section === section && owner.selected.key() === selected &&
      JSON.stringify(stack.getAllPathName().map((name: string, index: number) => [name, stack.getParamByIndex(index)])) === pathStamp;
    this.defaultMainPending = true;
    try {
      const response = await repo.call('/api/v1/assistant');
      if (!valid()) return;
      const data = mainConversationData(response);
      if (!data) return;
      const row = ResourceRow.from(data as Object as Record<string, Object>, 'conversation');
      row.group = owner.profile.id; row.manager = owner.profile.manager;
      if (!valid()) return;
      owner.mainId = row.id; owner.mainMachineId = row.machine;
      this.pushChat(row);
      this.defaultMainAttempt = JSON.stringify([owner.scope(), generation, owner.mainId, owner.mainMachineId]);
    } catch (error) { if (valid()) owner.reportError(error as Error); }
    finally { this.defaultMainPending = false; const waiting = this.mainReadWaiters; this.mainReadWaiters = []; waiting.forEach((resolve: () => void) => resolve()); if (scope !== this.model.scope() || generation !== this.model.connectionGeneration()) this.ensureMainDestination(); }
  }
  private openAssistantCard(itemId: string, kind: string): void {
    const source = this.detailModel.selected;
    if (!itemId || !this.activeChild || !['conversation', 'session'].includes(source.kind)) return;
    const row = NavigationContext.rowFrom(JSON.stringify(source));
    const data = row.data(); data['assistant_item_id'] = itemId; data['assistant_item_kind'] = kind; row.raw = JSON.stringify(data);
    this.stack.pushPath({ name: 'assistant', param: JSON.stringify(row) }); this.savePath();
  }
  private wide(): boolean { return this.breakpoint === WidthBreakpoint.WIDTH_LG || this.breakpoint === WidthBreakpoint.WIDTH_XL; }
  private rail(): boolean { return this.wide() && this.collapsed; }
  private filtered(): ResourceRow[] {
    return this.model.sessions.filter((row: ResourceRow) =>
      (!this.query || (row.title + ' ' + row.path + ' ' + row.machine).toLowerCase().includes(this.query.toLowerCase())) &&
      (this.filter === 0 || this.filter === 1 && row.state === 'needs_you' || this.filter === 2 && row.state === 'working'));
  }
  private hideAgentTitle(): boolean {
    const epoch = this.visibilityEpoch;
    const ratio = this.rowVisibility.get(this.model.scope() + this.model.selected.key()) || 0;
    return this.detailModel === this.model && this.model.section === 1 && this.wide() && !this.rail() && this.model.selected.kind === 'session' && ratio >= 0.99;
  }
  private openReader(address: string): void { const safe = readerLink(address); if (safe) { this.readerAddress = safe; this.readerTitle = readerHost(safe); this.readerShown = true; } }
  @Builder readerContent() { LinkReader({ address: this.readerAddress, onHost: (host: string) => this.readerTitle = host }) }
  private openSheet(kind: string): void { this.navigationIntent++; if (kind === 'connect') this.pairingRevision++; this.readerShown = false; if (kind === 'newagent') { this.prepareAgent(); return; } this.sheetKind = kind; this.showSheet = true; }
  private sheetTitle(): string { return tr(this.sheetKind === 'hub' ? 'hub' : this.sheetKind === 'newagent' ? 'new_agent' : 'welcome'); }
  @Builder sheetContent() {
    if (this.sheetKind === 'hub') this.workspaceSheet()
    else if (this.sheetKind === 'newagent') this.newAgentForm()
    else this.pairingForm()
  }
  private updateBreakpoint(): void {
    const old = this.breakpoint; this.breakpoint = this.getUIContext().getWindowWidthBreakpoint();
    hilog.info(0x0000, 'PersonaLayout', 'widthBreakpoint=%{public}s', String(this.breakpoint));
    if (old !== this.breakpoint && this.wide() && this.model.section === 1 && this.stack.size() === 0 && this.model.sessions.length)
      this.openRow(this.model.sessions.find((row: ResourceRow) => row.key() === this.model.selected.key()) || this.model.sessions[0]);
  }
  private bindDeviceOwner(model: ClientModel): void {
    const host = this.getUIContext().getHostContext();
    if (host) deviceFeatures.bind(model, host as common.UIAbilityContext,
      (title: string, message: string, approve: () => void, deny: () => void): void => {
        this.getUIContext().showAlertDialog({ title: title, message: message,
          primaryButton: { value: tr('cancel'), action: deny }, secondaryButton: { value: tr('confirm'), action: approve } });
      });
  }
  private restoreDeviceOwner(owner: ClientModel): void { if (deviceFeatures.boundTo(owner)) this.bindDeviceOwner(this.model); }
  aboutToAppear(): void {
    this.syncConversationRows(); this.getUIContext().setKeyboardAvoidMode(KeyboardAvoidMode.RESIZE); this.updateBreakpoint(); this.consumePairingLink(); this.restoreScope(); this.consumeNotification();
    this.bindDeviceOwner(this.model);
    this.applications.bind(this.model); this.applications.refresh();
  }
  aboutToDisappear(): void { this.agentWorkspace.hide(); this.applications.dispose(); this.dismissChatActions(); }
  @Monitor('model.profile', 'model.connected', 'model.features') applicationOwnerChanged(): void {
    this.applications.bind(this.model); this.applications.connectionChanged();
  }
  @Monitor('model.profile') scopeChanged(): void { if (this.activeScope !== this.model.scope()) { this.pendingInboxHandoff = ''; this.agentWorkspace.hide(); this.spacesShown = false; this.dismissChatActions(); this.showSheet = false; this.agentFormRevision++; this.creatingAgent = false; this.agentTodo = undefined; this.readerShown = false; this.readerAddress = ''; this.readerTitle = ''; this.detailModel.hideConversation(); this.detailModel = this.model; this.chatReady = false; this.detailRoute = ''; this.routeRevision++; this.bindDeviceOwner(this.model); } this.syncConversationRows(); this.restoreScope(); deviceFeatures.context(); }
  @Monitor('model.conversations', 'model.mainId', 'model.mainMachineId') conversationsChanged(): void { this.syncConversationRows(); }
  @Monitor('model.appearance') appearanceChanged(): void {
    this.getUIContext().getHostContext()?.getApplicationContext().setColorMode(this.model.appearance === 1 ? ConfigurationConstant.ColorMode.COLOR_MODE_LIGHT : this.model.appearance === 2 ? ConfigurationConstant.ColorMode.COLOR_MODE_DARK : ConfigurationConstant.ColorMode.COLOR_MODE_NOT_SET);
  }
  @Monitor('detailModel.connected') chatMenuConnectionChanged(): void { if (!this.detailModel.connected && (this.chatTitleMenuVisible || this.chatBodyMenuVisible)) this.dismissChatActions(); }
  @Monitor('model.connected') connectionChanged(): void { deviceFeatures.context(); if (this.model.connected && this.shownName && this.activeChild === this.childKey(this.shownName, this.shownParam)) this.restoreTarget(this.shownName, this.shownParam); }
  @Monitor('model.forgottenEpoch') forgetNavigation(): void {
    for (const key of this.paths.keys()) if (isHubStateKey('view:' + key, this.model.forgottenProfile)) this.paths.delete(key);
    this.rowVisibility.clear(); this.visibilityEpoch++;
  }
  private async restoreScope(): Promise<void> {
    const scope = this.model.scope(); if (scope === this.activeScope) return;
    this.activeScope = scope; this.stack = this.paths.get(scope + ':' + this.model.section) || new NavPathStack(); this.navigationRestoredScope = ''; this.rowVisibility.clear(); const epoch = ++this.restoring, intent = this.navigationIntent;
    let view: Record<string, Object>;
    try { view = Wire.parse(await this.model.loadView('navigation')); }
    catch (_) { if (epoch === this.restoring && scope === this.model.scope()) { this.activeScope = ''; this.model.error = 'cache_unavailable'; } return; }
    if (epoch !== this.restoring || scope !== this.model.scope()) return;
    if (intent === this.navigationIntent) this.model.section = Math.max(0, Math.min(3, Wire.number(view, 'section')));
    Wire.rows(view, 'tabs').forEach((tab: Record<string, Object>) => {
      const key = scope + ':' + Wire.number(tab, 'index');
      if (!this.paths.has(key)) { const stack = new NavPathStack();
        Wire.rows(tab, 'routes').forEach((route: Record<string, Object>, index: number) => {
          const name = Wire.text(route, 'name'); let param = Wire.text(route, 'param');
          if (index === 0 && ['session', 'conversation'].includes(name)) { const row = this.routeRow(param), data = row.data(); data['navigation_root'] = true; row.raw = JSON.stringify(data); param = JSON.stringify(row); }
          stack.pushPath({ name: name, param: param }, false);
        });
        this.paths.set(key, stack);
      }
    });
    this.stack = this.paths.get(scope + ':' + this.model.section) || new NavPathStack();
    this.paths.set(scope + ':' + this.model.section, this.stack);
    this.navigationRestoredScope = scope;
    if (intent !== this.navigationIntent) this.savePath();
    if (this.model.section === 2 && !this.stack.size()) this.openResources('inbox');
    else if (this.model.section === 3 && !this.stack.size()) this.openSettingsRoot();
    else this.ensureMainDestination();
  }
  @Monitor('model.pairingLink') pairingChanged(): void { this.consumePairingLink(); }
  @Monitor('model.notificationTarget') notificationChanged(): void { this.consumeNotification(); }
  private consumeNotification(): void {
    if (!this.model.notificationTarget) return;
    const row = this.routeRow(this.model.notificationTarget); this.model.notificationTarget = '';
    if (!this.model.sessions.concat(this.model.conversations).some((item: ResourceRow) => item.key() === row.key())) return;
    this.changeSection(row.kind === 'conversation' ? 0 : 1); this.openRow(row, false);
  }
  private consumePairingLink(): void {
    if (!this.model.pairingLink) return; this.address = this.model.pairingLink; this.pairCode = ''; this.model.pairingLink = ''; this.openSheet('connect');
  }
  private savePath(): void {
    if (this.activeScope !== this.model.scope()) this.restoreScope();
    this.navigationIntent++;
    const key = this.model.scope() + ':' + this.model.section;
    if (this.navigationRestoredScope === this.model.scope() || this.stack.size() || this.paths.has(key)) this.paths.set(key, this.stack);
    const tabs: Record<string, Object>[] = [];
    for (let index = 0; index < 4; index++) { const stack = this.paths.get(this.model.scope() + ':' + index);
      const routes: Record<string, Object>[] = [];
      stack?.getAllPathName().forEach((name: string, position: number) => routes.push(Wire.payload(['name', 'param'], [name, stack.getParamByIndex(position) as string || ''])));
      tabs.push(Wire.payload(['index', 'routes'], [index, routes]));
    }
    if (this.navigationRestoredScope !== this.model.scope()) return;
    this.model.saveView('navigation', JSON.stringify(Wire.payload(['section', 'tabs'], [this.model.section, tabs]))).catch(() => this.model.error = 'failed');
  }
  private changeSection(index: number): void {
    this.pendingInboxHandoff = ''; this.savePath(); this.routeRevision++; this.activeChild = ''; this.shownName = ''; this.chatReady = false; this.detailRoute = ''; this.detailModel.hideConversation(); this.model.releaseTerminal(); this.model.section = index; this.stack = this.paths.get(this.model.scope() + ':' + index) || new NavPathStack(); this.query = ''; this.filter = 0;
    if (index === 1 && this.wide() && !this.stack.size() && this.model.sessions.length) this.openRow(this.model.sessions[0]);
    if (index === 2) this.model.listResources('inbox'); this.savePath();
  }
  private routeTarget(row: ResourceRow, root: boolean): ResourceRow {
    const copy = NavigationContext.rowFrom(JSON.stringify(row));
    if (copy.kind === 'session' || copy.kind === 'conversation') { const data = copy.data(); data['navigation_root'] = root; copy.raw = JSON.stringify(data); }
    return copy;
  }
  private openRow(row: ResourceRow, root: boolean = true): void {
    const name = NavigationContext.destination(row);
    this.chatReady = false; this.detailRoute = '';
    if (row.kind === 'session') { this.stack.clear(); this.tool = 0; }
    this.stack.pushPath({ name: name, param: JSON.stringify(this.routeTarget(row, root || !this.stack.size())) }); this.savePath();
  }
  private async openLinkedResource(row: ResourceRow): Promise<void> {
    const generation = this.model.connectionGeneration(), scope = this.model.scope(), source = this.activeChild;
    try {
      if (!['inbox', 'todo', 'knowledge', 'file', 'todo_source', 'todo_thread'].includes(row.kind)) await this.model.viewFor(row);
      if (!this.model.owns(generation, scope) || this.activeChild !== source) return;
      this.chatReady = false; this.detailRoute = '';
      const target = this.routeTarget(row, false), name = NavigationContext.destination(target), data = target.data();
      const handoff = target.kind === 'inbox' && Wire.bool(data, 'open_handoff');
      if (Wire.has(data, 'open_handoff')) data['open_handoff'] = false; target.raw = JSON.stringify(data);
      const param = JSON.stringify(target);
      if (handoff) this.pendingInboxHandoff = this.childKey(name, param);
      this.stack.pushPath({ name: name, param: param }); this.savePath();
    } catch (error) { if (this.model.owns(generation, scope) && this.activeChild === source) this.model.reportError(error as Error); }
  }
  private openFrom(name: string, param: Object, row: ResourceRow): void {
    if (this.activeChild === this.childKey(name, param)) { if (!row.group) row.group = NavigationContext.from(name, param as string).row.group || this.model.profile.id; this.openLinkedResource(row); }
  }
  private createAgentFrom(name: string, param: Object, row: ResourceRow): void {
    if (this.activeChild === this.childKey(name, param)) this.prepareAgent(row);
  }
  private openComputer(row: ResourceRow): void {
    if (row.kind !== 'machine' || !row.id) return;
    const param = JSON.stringify(row), names = this.stack.getAllPathName(), top = names.length - 1;
    if (top >= 0 && names[top] === 'computer' && this.stack.getParamByIndex(top) === param) return;
    this.stack.pushPath({ name: 'computer', param: param }); this.savePath();
  }
  private selectedComputer(): ResourceRow | undefined {
    return this.detailModel.machines.find((row: ResourceRow) => row.id === this.detailModel.selected.machine);
  }
  private async handoffResource(row: ResourceRow): Promise<void> {
    const generation = this.model.connectionGeneration(), scope = this.model.scope(), source = this.activeChild;
    try {
      const target = await this.model.handoff(row);
      if (this.model.owns(generation, scope) && this.activeChild === source) await this.openLinkedResource(target);
    } catch (error) { if (this.model.owns(generation, scope) && this.activeChild === source) this.model.reportError(error as Error); }
  }
  private childKey(name: string, param: Object): string { return name + ':' + (param as string); }
  private pushChat(row: ResourceRow): void {
    const names = this.stack.getAllPathName();
    const index = names.findIndex((name: string, position: number) => name === 'conversation' && this.routeRow(this.stack.getParamByIndex(position) as string).key() === row.key());
    if (index >= 0) this.stack.popToIndex(index);
    else this.stack.pushPath({ name: 'conversation', param: JSON.stringify(this.routeTarget(row, !this.stack.size())) });
    this.savePath();
  }
  private async openMain(): Promise<void> {
    this.navigationIntent++; if (this.model.section !== 0) this.changeSection(0);
    const owner = this.model, scope = owner.scope(), generation = owner.connectionGeneration(), intent = this.navigationIntent, revision = this.routeRevision, stack = this.stack;
    const current = (): boolean => this.model === owner && owner.owns(generation, scope) && owner.section === 0 && intent === this.navigationIntent && revision === this.routeRevision && stack === this.stack;
    while (this.defaultMainPending && current()) await new Promise<void>((resolve) => this.mainReadWaiters.push(resolve));
    if (current()) await this.resolveMainDestination();
  }
  private newConversation(): void {
    this.navigationIntent++;
    const scope = this.model.scope(), generation = this.model.connectionGeneration(), ownerKey = JSON.stringify([scope, generation]);
    this.creatingConversations.set(ownerKey, (this.creatingConversations.get(ownerKey) || 0) + 1);
    this.model.newConversation().then((row: ResourceRow) => { if (this.model.owns(generation, scope)) this.pushChat(row); })
      .catch((error: Error) => { if (this.model.owns(generation, scope)) this.model.reportError(error); })
      .finally(() => { const remaining = (this.creatingConversations.get(ownerKey) || 1) - 1; if (remaining) this.creatingConversations.set(ownerKey, remaining); else this.creatingConversations.delete(ownerKey); });
  }
  private openResources(kind: string): void { this.resourceKind = kind; this.model.listResources(kind); this.stack.pushPath({ name: 'resources', param: kind }); this.savePath(); }
  private openSettings(page: string, contextual: boolean = false): void { if (page === 'appearance') this.ensureAgentWorkspace(); this.settingsPage = page; this.stack.pushPath({ name: 'settings', param: NavigationContext.settings(page, contextual ? this.detailModel.selected : undefined) }); this.savePath(); }
  private settingsFamily(page: string): string { return ['extension_detail', 'extension_catalog'].includes(page) ? 'extensions' : page === 'connector_detail' ? 'connectors' : page === 'channel_detail' ? 'channels' : page; }
  private isAdminPage(page: string): boolean { return ['assistant', 'memory', 'managers', 'launchers', 'engine', 'hub_update', 'node_update'].includes(page); }
  private settingsLabel(page: string): string { return page === 'ssh' ? 'ssh_settings' : page === 'assistant' ? 'admin_identity' : page === 'memory' ? 'admin_memory' : page === 'managers' ? 'admin_managers' : page === 'launchers' ? 'admin_launchers' : page === 'engine' ? 'admin_machine_engine' : page === 'hub_update' ? 'admin_hub_update' : page === 'node_update' ? 'admin_node_update' : page; }
  private adminMenus(name: string, param: Object): NavigationMenuItem[] { return [{ value: tr('refresh'), symbolIcon: new SymbolGlyphModifier($r('sys.symbol.arrow_clockwise')), action: () => { if (this.activeChild === this.childKey(name, param)) this.adminRefreshRevision++; } }]; }
  private settingsTitle(param: Object): string { const descriptor = NavigationContext.from('settings', param as string); return descriptor.scoped && ['extension_detail', 'connector_detail', 'channel_detail', 'extension_catalog'].includes(descriptor.page) ? descriptor.row.title : tr(this.settingsLabel(descriptor.page)); }
  private openSettingsDetail(page: string, row: ResourceRow): void { this.stack.pushPath({ name: 'settings', param: NavigationContext.settings(page, row) }); this.savePath(); }
  private openApplications(): void {
    const param = JSON.stringify([this.model.profile.id, this.model.profile.manager]);
    const found = this.stack.getAllPathName().findIndex((name: string): boolean => name === 'applications');
    if (found >= 0) this.stack.popToIndex(found);
    else this.stack.pushPath({ name: 'applications', param: param });
    this.savePath(); this.applications.refresh();
  }
  private openApplication(entry: ExtensionNavigationEntry): void {
    if (!this.applications.accepts(entry)) return;
    const found = this.stack.getAllPathName().findIndex((name: string): boolean => name === 'applications');
    if (found < 0) return;
    this.stack.popToIndex(found);
    this.openExtension(this.model, entry.extensionId, entry.uiId, entry.title, '', {}, entry.extensionId, entry.uiId);
  }
  private applicationAllowed(param: Object): boolean {
    const row = this.routeRow(param), data = row.data(), root = Wire.text(data, 'navigation_extension');
    return !root || this.applications.allows(row.group, row.manager, root, Wire.text(data, 'navigation_ui'));
  }
  private openExtension(owner: ClientModel, extensionId: string, uiId: string, title: string, state: string = '', context: Record<string, Object> = Wire.payload([], []), navigationExtension: string = '', navigationUI: string = ''): void {
    if (!extensionId || !uiId || !owner.profile.id) return;
    if (this.stack.getAllPathName().filter((name: string) => name === 'extension').length >= 8) { this.getUIContext().getPromptAction().showToast({ message: tr('extension_ui_stack_limit') }); return; }
    const row = new ResourceRow(extensionId, title || extensionId, 'extension'); row.group = owner.profile.id; row.manager = owner.profile.manager;
    row.raw = JSON.stringify(Wire.payload(['ui', 'state', 'context', 'instance', 'navigation_extension', 'navigation_ui'], [uiId, state, context, util.generateRandomUUID(), navigationExtension, navigationUI]));
    this.stack.pushPath({ name: 'extension', param: JSON.stringify(row) }); this.savePath();
  }
  private extensionNavigate(name: string, param: Object, effect: ExtensionUINavigationEffect): void {
    if (this.activeChild !== this.childKey(name, param) || !this.applicationAllowed(param) || effect.kind !== 'push') return;
    const source = this.routeRow(param).data();
    this.openExtension(this.detailModel, effect.extensionId, effect.uiId, effect.title, effect.state, effect.context, Wire.text(source, 'navigation_extension'), Wire.text(source, 'navigation_ui'));
  }
  private extensionMenus(name: string, param: Object): NavigationMenuItem[] { return [{ value: tr('refresh'), symbolIcon: new SymbolGlyphModifier($r('sys.symbol.arrow_clockwise')), action: () => { if (this.activeChild === this.childKey(name, param)) this.extensionRefreshRevision++; } }]; }
  private settingsKind(param: Object): string { return NavigationContext.from('settings', param as string).page; }
  private targetReady(name: string, param: Object): boolean {
    if (!this.chatReady || this.detailRoute !== this.childKey(name, param)) return false;
    if (name !== 'session' && name !== 'conversation') return true;
    return this.detailOwner === this.detailModel &&
      this.detailSelection === JSON.stringify([this.detailModel.scope(), this.detailModel.selected.key()]);
  }
  private markDetailReady(key: string): void {
    this.detailOwner = this.detailModel;
    this.detailSelection = JSON.stringify([this.detailModel.scope(), this.detailModel.selected.key()]);
    this.detailRoute = key; this.chatReady = true;
  }
  private settingsModel(param: Object): ClientModel { return NavigationContext.from('settings', param as string).scoped ? this.detailModel : this.model; }
  private async connect(): Promise<void> {
    const generation = this.model.connectionGeneration(), scope = this.model.scope(), address = this.address, code = this.pairCode, revision = this.pairingRevision;
    const current = (): boolean => this.showSheet && this.sheetKind === 'connect' && revision === this.pairingRevision && this.model.owns(generation, scope) && this.address === address && this.pairCode === code;
    this.model.busy = true; this.model.error = '';
    try {
      const pairing = isPairingAddress(this.address.trim()) ? Pairing.parse(this.address.trim()) : Pairing.manual(this.address.trim(), this.pairCode.trim());
      const target = await pairing.inspect(); if (!current()) return; this.model.busy = false;
      this.getUIContext().showAlertDialog({ title: tr('verify'), message: target.name + '\n' + target.baseUrl + '\n' + target.hubId,
        primaryButton: { value: tr('cancel'), action: () => {} },
        secondaryButton: { value: tr('trust'), action: () => { if (current()) this.redeem(pairing, target, current); } } });
    } catch (error) {
      if (!current()) return;
      const detail = error as Error as Object as Record<string, Object>; const code = detail['code'];
      this.model.diagnosticCode = error instanceof Error && /^[a-z_0-9]{1,64}$/.test(error.message) ? error.message : typeof code === 'number' ? String(code) : 'pairing_failed';
      hilog.error(0x0000, 'PersonaPair', '%{public}s', this.model.diagnosticCode);
      this.model.error = 'connection_failed';
    } finally { if (this.model.owns(generation, scope)) this.model.busy = false; }
  }
  private async redeem(pairing: Pairing, target: PairingTarget, admit: () => boolean): Promise<void> {
    const revision = this.pairingRevision, address = this.address, code = this.pairCode;
    try {
      await this.model.pair(pairing, target, admit);
      if (this.sheetKind === 'connect' && revision === this.pairingRevision && this.address === address && this.pairCode === code && this.model.profile.id === target.hubId) { this.pairCode = ''; this.address = ''; this.showSheet = false; }
    } catch (_) { if (admit()) this.model.error = 'connection_failed'; }
  }
  private menus(): NavigationMenuItem[] {
    if (this.rail()) return [];
    return [
      { value: tr(this.peer() === 1 ? 'new_agent' : 'new_chat'), symbolIcon: new SymbolGlyphModifier(Symbols.plus), action: () => this.peer() === 1 ? this.openSheet('newagent') : this.newConversation() },
      { value: tr('conversation_spaces'), symbolIcon: new SymbolGlyphModifier(Symbols.spaces), action: () => this.spacesShown = true }
    ];
  }
  @Builder shellMenu() {
    Row({ space: Tokens.gap }) {
      if (!this.rail()) ForEach(this.menus(), (item: NavigationMenuItem, index: number) => {
        Button({ type: ButtonType.Circle }) { SymbolGlyph(index === 0 ? Symbols.plus : Symbols.spaces).fontSize(Tokens.symbolFont).fontColor([Colors.ink]) }
          .attributeModifier(ButtonStyles.chrome).accessibilityText(item.value as string).onClick(() => item.action?.()).id(index === 0 ? 'shell.new' : 'shell.spaces')
      })
    }.height(this.headerHeight()).alignItems(VerticalAlign.Center).padding({ right: Tokens.page })
  }
  @Builder destinationMenus(name: string, param: Object) {
    if (!this.mainDestination(name, param)) Row({ space: Tokens.gap }) {
      ForEach(this.destinationMenuItems(name, param), (item: NavigationMenuItem, index: number) => {
        Button({ type: ButtonType.Circle }) {
          if (item.symbolIcon) SymbolGlyph().attributeModifier(item.symbolIcon).fontSize(Tokens.symbolFont)
          else Image(item.icon).width(Tokens.symbol).height(Tokens.symbol).fillColor(Colors.ink)
        }.attributeModifier(ButtonStyles.chrome).accessibilityText(item.value as string).enabled(item.isEnabled !== false).onClick(() => item.action?.())
          .id(name === 'session' || name === 'conversation' ? 'chat.title.more' : 'destination.action.' + String(index))
          .bindMenu((name === 'session' || name === 'conversation') && this.chatTitleMenuVisible && this.activeChild === this.childKey(name, param),
            this.chatActionsMenu, this.chatMenuOptions(this.chatMenuRevision))
      })
    }.height(this.headerHeight()).alignItems(VerticalAlign.Center).padding({ right: Tokens.page })
  }
  private openSettingsRoot(): void { this.stack.pushPath({ name: 'settings_root', param: '' }); this.savePath(); }
  private openSpace(kind: string): void {
    this.spacesShown = false;
    if (kind === 'persona') { this.selectPeer(0); this.openMain(); }
    else if (kind === 'settings') this.openSettingsRoot();
    else if (kind === 'devices') this.openSettings('machines');
    else if (kind === 'applications') this.openApplications();
    else if (kind === 'hub') this.openSheet('hub');
    else if (kind === 'collapse') this.collapsed = true;
    else this.openResources(kind);
  }
  private spaceSymbol(kind: string): Resource {
    if (kind === 'inbox') return Symbols.inbox;
    if (kind === 'todo') return Symbols.check;
    if (kind === 'knowledge') return Symbols.document;
    if (kind === 'health') return Symbols.health;
    if (kind === 'devices') return Symbols.device;
    if (kind === 'settings') return Symbols.gear;
    if (kind === 'applications') return Symbols.apps;
    if (kind === 'collapse') return Symbols.collapse;
    return Symbols.spaces;
  }
  private spaceColor(kind: string): ResourceColor {
    if (kind === 'inbox' || kind === 'persona') return Colors.link;
    if (kind === 'todo') return Colors.positive;
    if (kind === 'knowledge') return Colors.ai;
    if (kind === 'health') return Colors.teal;
    if (kind === 'devices') return Colors.working;
    return Colors.secondary;
  }
  private spaceTitle(kind: string): string {
    if (kind === 'hub') return this.model.profile.name || tr('hub');
    if (kind === 'persona') return tr('main_chat');
    if (kind === 'todo') return tr('todos');
    if (kind === 'health') return tr('health_home_title');
    if (kind === 'devices') return tr('machines');
    return tr(kind);
  }
  @Builder spaceRow(kind: string) {
    ListItem() {
      Row({ space: Tokens.inset }) {
        SymbolGlyph(this.spaceSymbol(kind)).fontSize(Tokens.symbolFont).fontColor([this.spaceColor(kind)]).width(Tokens.symbol)
        Text(this.spaceTitle(kind)).fontSize(Tokens.body).fontColor(Colors.ink).layoutWeight(1)
      }.padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset }).constraintSize({ minHeight: Tokens.touch }).width('100%')
    }.selectable(true).selected(this.activeModule() === kind).onClick(() => this.openSpace(kind)).id('spaces.' + kind)
  }
  @Builder spacesContent() {
    Column() {
      List() {
        this.spaceRow('persona')
        ForEach(this.conversationRowBindings, (binding: BoundRow<ResourceRow>) => {
          ListItem() { this.conversationRow(binding, true) }.onClick(() => { this.spacesShown = false; if (this.model.section !== 0) this.changeSection(0); this.openRow(binding.value); })
        }, (binding: BoundRow<ResourceRow>) => binding.key)
        this.spaceRow('inbox')
        this.spaceRow('todo')
        this.spaceRow('knowledge')
        this.spaceRow('health')
        if (this.applications.visible) this.spaceRow('applications')
        this.spaceRow('devices')
        this.spaceRow('settings')
        this.spaceRow('hub')
        if (this.wide() && !this.rail()) this.spaceRow('collapse')
      }.width('100%').layoutWeight(1)
    }.width('100%').height('100%').padding({ bottom: this.spacesInset + Tokens.page }).id('shell.spaces.content')
      .onAreaChange(() => this.measureSpaces())
  }
  private measureSpaces(): void {
    if (!this.spacesLayoutReady) return;
    const value = nativeSheetInset(this.getUIContext(), 'shell.spaces.content', this.environment.safeBottomPx);
    if (Math.abs(value - this.spacesInset) > 0.1) this.spacesInset = value;
  }
  @Monitor('environment.safeBottomPx') safeBottomChanged(): void { this.measureSpaces(); }
  @Monitor('showSheet', 'spacesShown') modalChanged(): void { if (!this.showSheet && !this.spacesShown) this.ensureMainDestination(); }
  private openHistory(): void { this.stack.pushPath({ name: 'history', param: JSON.stringify(this.detailModel.selected) }); this.savePath(); }
  private openTools(path: string = ''): void { const data = Wire.record(JSON.parse(JSON.stringify(this.detailModel.selected)) as Object); if (path) data['open_file'] = path; this.stack.pushPath({ name: 'tools', param: JSON.stringify(data) }); this.savePath(); }
  private destinationMenuItems(name: string, param: Object): NavigationMenuItem[] {
    if (name === 'settings' && this.isAdminPage(this.settingsKind(param))) return this.adminMenus(name, param);
    if (name === 'extension' && this.applicationAllowed(param)) return this.extensionMenus(name, param);
    if (this.mainDestination(name, param)) return [];
    if (name === 'session' || name === 'conversation') return [{ value: tr('more'), icon: $r('sys.media.ohos_ic_public_more'), isEnabled: this.targetReady(name, param), action: () => this.showChatActions(name, param) }];
    return [];
  }
  private chatMenuOptions(revision: number): MenuOptions {
    return { onDisappear: () => this.closeChatActions(revision) };
  }
  @Builder chatActionsMenu() {
    Menu() {
      ForEach(this.chatMenuActions, (item: SheetInfo) => {
        MenuItem({ content: item.title }).constraintSize({ minHeight: Tokens.touch }).onClick(() => item.action())
      }, (_item: SheetInfo, index: number) => String(this.chatMenuRevision) + ':' + String(index))
    }
  }
  private closeChatActions(revision: number): void {
    if (revision !== this.chatMenuRevision) return;
    this.chatTitleMenuVisible = false; this.chatBodyMenuVisible = false;
  }
  private dismissChatActions(): void {
    this.chatMenuRevision++; this.chatTitleMenuVisible = false; this.chatBodyMenuVisible = false; this.chatMenuActions = [];
  }
  private showChatActions(name: string, param: Object): void {
    const owner = this.detailModel, key = owner.selected.key(), generation = owner.connectionGeneration(), scope = owner.scope();
    const valid = (): boolean => this.activeChild === this.childKey(name, param) && this.targetReady(name, param) &&
      this.detailModel === owner && owner.owns(generation, scope) && owner.selected.key() === key;
    if (!valid()) { this.dismissChatActions(); return; }
    const sheets: SheetInfo[] = [];
    if (owner.isChat() && owner.selected.machine) sheets.push({ title: tr('interaction_history'), action: () => { if (valid()) this.openHistory(); } });
    const computer = this.selectedComputer();
    if (computer) sheets.push({ title: tr('computer_screen_open'), action: () => { if (valid()) this.openComputer(computer); } });
    if (owner.selected.machine) sheets.push({ title: tr('workspace'), action: () => { if (valid()) this.openTools(); } });
    for (const page of ['permissions', 'voice']) sheets.push({ title: tr(page), action: () => {
      if (valid()) this.openSettings(page === 'permissions' ? 'session_permissions' : page, true);
    } });
    const identity = JSON.stringify([owner.scope(), owner.connectionGeneration(), owner.target()]);
    const data = owner.selected.data();
    if (owner.selected.kind === 'session' && Wire.bool(Wire.object(data, 'capabilities'), 'modes') && Wire.strings(data, 'efforts').length) sheets.push({ title: tr('effort'), action: () => { if (valid()) this.openSettings('session_effort', true); } });
    if (owner.canStop() && this.inlineStopIdentity !== identity) sheets.push({ title: tr('stop'), action: () => { if (valid()) owner.stop(valid); } });
    this.chatMenuRevision++; this.chatMenuActions = sheets;
    const body = this.mainDestination(name, param) || this.hideChatTitle(name, param);
    this.chatTitleMenuVisible = !body; this.chatBodyMenuVisible = body;
  }
  private syncConversationRows(): void {
    const scope = JSON.stringify([this.model.scope(), this.model.profile.id, this.model.profile.manager]);
    const rows = this.model.conversations.filter((row: ResourceRow) => row.id !== this.model.mainId || !!this.model.mainMachineId && row.machine !== this.model.mainMachineId);
    this.conversationRowBindings = reconcileBoundRows(this.conversationRowBindings, rows,
      (row: ResourceRow) => row.key(), this.conversationBindingOwner, this.model, this.conversationBindingScope, scope);
    this.conversationBindingOwner = this.model;
    this.conversationBindingScope = scope;
  }
  private conversationDetail(row: ResourceRow): string {
    return row.kind === 'session' ? [this.model.machineName(row.machine), this.model.shortPath(row.path), row.state ? tr(row.state) : ''].filter((value: string) => value.length > 0).join(' · ') : row.detail;
  }
  @Builder conversationRow(binding: BoundRow<ResourceRow>, showDetail: boolean) {
    Row({ space: Tokens.inset }) {
      SymbolGlyph(Symbols.spaces).fontSize(Tokens.symbolFont).fontColor([Colors.secondary]).width(Tokens.symbol)
      Column({ space: Tokens.fine }) {
        Text(binding.value.title || binding.value.id).fontSize(Tokens.body).fontColor(Colors.ink).width('100%').maxLines(2).textOverflow({ overflow: TextOverflow.Ellipsis })
        if (showDetail && this.conversationDetail(binding.value)) Text(this.conversationDetail(binding.value)).fontSize(Tokens.meta).fontColor(Colors.secondary).width('100%').maxLines(2).textOverflow({ overflow: TextOverflow.Ellipsis })
      }.layoutWeight(1).alignItems(HorizontalAlign.Start)
    }.padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset }).constraintSize({ minHeight: Tokens.touch }).width('100%')
  }
  @Builder state() {
    if (this.model.busy) LoadingProgress().height(Tokens.touch).width(Tokens.touch)
    if (this.model.error) {
      ListItem() { Text(tr(this.model.error)).fontSize(Tokens.meta).padding(Tokens.page) }
    }
  }
  private retryRoot(): void {
    if (this.navigationRestoredScope !== this.model.scope()) { this.activeScope = ''; this.model.error = ''; this.restoreScope(); }
    else if (this.model.connected) { this.model.error = ''; this.defaultMainAttempt = ''; this.model.refresh().then(() => this.ensureMainDestination()); }
    else this.model.foregrounded();
  }
  @Builder connectionState() {
    Row({ space: Tokens.gap }) { Text(tr(this.model.error)).fontSize(Tokens.meta).layoutWeight(1)
      Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('retry')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('retry', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('retry')).bindTips(tr('retry')).onClick(() => this.retryRoot())
    }.padding(Tokens.inset)
  }
  @Builder personaList() {
    List({ space: Tokens.section }) {
      if (this.model.error) ListItem() { this.connectionState() }
      ListItemGroup() {
      ListItem() { Row({ space: Tokens.inset }) { Image(this.look.avatarUri || $r('app.media.foreground')).width(Tokens.symbol).height(Tokens.symbol).objectFit(ImageFit.Contain); Text(tr('main_chat')).fontSize(Tokens.body).fontWeight(FontWeight.Medium).fontColor(Colors.ink).layoutWeight(1) }.padding({ left: Tokens.page, right: Tokens.page, top: Tokens.inset, bottom: Tokens.inset }).constraintSize({ minHeight: Tokens.header }).width('100%') }.selectable(true).selected(this.chatSelectionShown('conversation') && this.detailModel === this.model && this.detailModel.selected.id === this.model.mainId && this.detailModel.selected.kind === 'conversation' && (!this.model.mainMachineId || this.detailModel.selected.machine === this.model.mainMachineId))
        .onVisibleAreaChange([0, 1], (visible: boolean, ratio: number) => { this.mainRowVisible = visible ? ratio : 0; const row = new ResourceRow(this.model.mainId, '', 'conversation'); row.manager = this.model.profile.manager; row.machine = this.model.mainMachineId; row.session = row.id; if (row.id && row.machine) { this.rowVisibility.set(this.model.scope() + this.model.target(row), visible ? ratio : 0); this.visibilityEpoch++; } })
        .onDisAppear(() => this.mainRowVisible = 0).onClick(() => this.openMain()).id('persona.main')
      ForEach(this.conversationRowBindings, (binding: BoundRow<ResourceRow>) => {
        ListItem() {
          this.conversationRow(binding, false)
        }.selectable(true).selected(this.chatSelectionShown('conversation') && this.detailModel === this.model && this.model.target() === this.model.target(binding.value))
          .onVisibleAreaChange([0, 1], (visible: boolean, ratio: number) => { this.rowVisibility.set(this.model.scope() + this.model.target(binding.value), visible ? ratio : 0); if (this.model.target() === this.model.target(binding.value)) this.visibilityEpoch++; })
          .onDisAppear(() => { this.rowVisibility.delete(this.model.scope() + this.model.target(binding.value)); this.visibilityEpoch++; }).onClick(() => this.openRow(binding.value))
      }, (binding: BoundRow<ResourceRow>) => binding.key)
      }.backgroundColor(Colors.canvas)
      ListItemGroup() {
      this.spaceRow('inbox')
      this.spaceRow('todo')
      this.spaceRow('knowledge')
      this.spaceRow('health')
      if (this.applications.visible) this.spaceRow('applications')
      this.spaceRow('devices')
      this.spaceRow('settings')
      }.backgroundColor(Colors.canvas)
    }.width('100%').height('100%').backgroundColor(Colors.canvas).padding({ bottom: Tokens.listTail })
  }
  private activeModule(): string {
    if (!this.activeChild) return '';
    const names = this.stack.getAllPathName();
    for (let index = names.length - 1; index >= 0; index--) {
      const name = names[index];
      const value: Object | undefined = this.stack.getParamByIndex(index) as Object | undefined;
      const param = typeof value === 'string' ? value : '';
      if (name === 'resources') return param;
      if (['todo', 'knowledge', 'inbox'].includes(name)) return name;
      if (name === 'settings_root') return 'settings';
      if (name === 'settings') return this.settingsKind(param) === 'machines' ? 'devices' : 'settings';
      if (name === 'applications' || name === 'extension' && !!Wire.text(this.routeRow(param).data(), 'navigation_extension')) return 'applications';
      if (['conversation', 'session', 'assistant', 'tools', 'history'].includes(name)) return 'chat';
    }
    return '';
  }
  private chatSelectionShown(kind: string): boolean {
    return this.activeModule() === 'chat' && this.stack.getAllPathName().includes(kind);
  }
  private readingAgentDetail(): boolean {
    return this.model.section === 1 && this.detailModel.selected.kind === 'session' &&
      (!!this.activeChild || this.stack.getAllPathName().includes('session'));
  }
  @Builder agentsList() {
    AgentListPage({ model: this.model, workspace: this.agentWorkspace, retainWorkspace: true, active: this.model.section === 1, selectedRow: this.detailModel.selected, selectionVisible: this.chatSelectionShown('session'), rail: this.rail(), readingDetail: this.readingAgentDetail(),
      onOpen: (row: ResourceRow) => this.openRow(row), onVisible: (row: ResourceRow, ratio: number) => {
        this.rowVisibility.set(this.model.scope() + row.key(), ratio); if (row.key() === this.detailModel.selected.key()) this.visibilityEpoch++;
      } })
  }
  @Builder inboxList() {
    InboxWorkspacePage({ model: this.model, visible: this.model.section === 2, refreshRevision: this.inboxRefreshRevision, onChanged: () => this.inboxRefreshRevision++, onOpen: (row: ResourceRow) => this.openRow(row), onLink: (address: string) => this.openReader(address),
      onHandoff: (row: ResourceRow) => this.openLinkedResource(row) })
  }
  @Builder settingsList() {
    List() {
      ForEach(['assistant', 'memory', 'machines', 'managers', 'launchers', 'engine', 'models', 'schedules', 'extensions', 'connectors', 'channels', 'translation', 'triage', 'ledger', 'phone', 'ssh', 'appearance', 'notifications', 'hub_update', 'node_update', 'privacy', 'diagnostics'], (page: string) => {
        ListItem() { ComposeListItemV2({ contentItemV2: new ContentItemV2({ primaryText: tr(this.settingsLabel(page)) }) }) }.selectable(true).selected(!!this.activeChild && (this.shownName === 'extension' ? page === 'extensions' : this.shownName === 'settings' && this.settingsFamily(this.settingsKind(this.shownParam)) === page)).onClick(() => this.openSettings(page)).id('settings.' + page)
      }, (page: string) => page)
    }.width('100%').height('100%').padding({ bottom: Tokens.listTail })
  }
  @Builder primaryContent() {
    if (this.peer() === 1) this.agentsList()
    else this.personaList()
  }
  @Builder workspaceSheet() {
    List() {
      ForEach(this.model.profiles, (profile: HubProfile) => { ListItem() { Text(profile.name).padding(Tokens.page) }
        .onClick(() => { this.model.selectProfile(profile); this.showSheet = false; }) }, (profile: HubProfile) => profile.id)
      ForEach(this.model.managers, (manager: ResourceRow) => { ListItem() { Text(manager.title).padding(Tokens.page) }
        .onClick(() => { this.model.selectManager(manager.id); this.showSheet = false; }) }, (manager: ResourceRow) => manager.id)
      ListItem() { Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('connect')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('connect', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('connect')).bindTips(tr('connect')).onClick(() => { this.showSheet = false; this.openSheet('connect'); }) }
    }.width('100%')
  }
  private async scanPairing(): Promise<void> {
    const host = this.getUIContext().getHostContext(); if (!host || this.scanning) return;
    const revision = ++this.scanRevision, generation = this.model.connectionGeneration(), scope = this.model.scope(); this.scanning = true;
    try {
      const result = await scanBarcode.startScanForResult(host, { scanTypes: [scanCore.ScanType.QR_CODE], enableAlbum: true, enableMultiMode: false });
      if (revision !== this.scanRevision || !this.showSheet || this.sheetKind !== 'connect' || !this.model.owns(generation, scope)) return;
      Pairing.parse(result.originalValue); this.address = result.originalValue; this.pairCode = ''; this.model.error = '';
    } catch (error) { if (revision === this.scanRevision && (error as BusinessError).code !== 1000500002) this.model.error = 'connection_failed'; }
    finally { if (revision === this.scanRevision) this.scanning = false; }
  }
  @Builder pairingForm() {
    Column({ space: Tokens.page }) {
      Row({ space: Tokens.gap }) {
        TextInput({ text: this.address, placeholder: tr('hub_address') }).onChange((value: string) => this.address = value).id('pair.address').layoutWeight(1)
        if (canIUse('SystemCapability.Multimedia.Scan.ScanBarcode')) Button({ type: ButtonType.Circle }) {
          SymbolGlyph($r('sys.symbol.qrcode')).fontSize(Tokens.symbolFont).fontColor([Colors.link])
        }.attributeModifier(ButtonStyles.chrome).accessibilityText(tr('scan_qr')).enabled(!this.scanning && !this.model.busy).onClick(() => this.scanPairing()).id('pair.scan')
      }.width('100%')
      TextInput({ text: this.pairCode, placeholder: tr('pair_code') }).type(InputType.Password).onChange((value: string) => this.pairCode = value).id('pair.code')
      if (this.model.error) Text(tr(this.model.error)).fontColor($r('app.color.persona_danger'))
      Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('verify')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('verify', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('verify')).bindTips(tr('verify')).enabled(!!this.address.trim() && (!!this.pairCode.trim() || isPairingAddress(this.address.trim())) && !this.model.busy).onClick(() => this.connect()).id('pair.verify').alignSelf(ItemAlign.End)
    }.padding(Tokens.page).width('100%')
  }
  private createAgentKey(): string {
    if (this.creatingAgent) return 'sending';
    return this.agentModel.operationBlocked('/api/v1/sessions') ? 'result_unknown' : 'create';
  }

  @Builder newAgentForm() {
    Column({ space: Tokens.page }) {
      TextArea({ text: this.newTask, placeholder: tr('task') }).onChange((value: string) => { this.newTask = value; this.saveAgentDraft(); }).height(Tokens.inputExpanded).id('agent.task')
      Select(this.agentMachines().map((row: ResourceRow): SelectOption => ({ value: row.title || row.id }))).selected(this.selectedMachine).value(this.agentMachines()[this.selectedMachine]?.title || this.agentMachines()[this.selectedMachine]?.id || tr('machines'))
        .onSelect((index: number) => this.selectedMachine = index).id('agent.machine')
      TextInput({ text: this.directory, placeholder: tr('directory') }).onChange((value: string) => { this.directory = value; this.saveAgentDraft(); }).id('agent.directory')
      TextInput({ text: this.launcher, placeholder: tr('launcher') }).onChange((value: string) => { this.launcher = value; this.saveAgentDraft(); }).id('agent.launcher')
      if (this.agentModel.error) Text(tr(this.agentModel.error)).fontSize(Tokens.meta).fontColor($r('app.color.persona_danger'))
      Button({ type: ButtonType.Circle }) { if (IconActions.busy(this.createAgentKey())) LoadingProgress().width(Tokens.symbol).height(Tokens.symbol).color(Colors.onAction); else SymbolGlyph(IconActions.symbol(this.createAgentKey())).fontSize(Tokens.symbolFont).fontColor([IconActions.color(this.createAgentKey(), true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr(this.createAgentKey())).bindTips(tr(this.createAgentKey())).enabled(!this.creatingAgent && !this.agentModel.operationBlocked('/api/v1/sessions') && this.newTask.trim().length > 0 && this.agentMachines().length > 0)
        .onClick(() => this.confirmCreateAgent()).id('agent.create').alignSelf(ItemAlign.End)
    }.padding(Tokens.page)
  }
  private agentDraftKey(): string { return 'new-agent:' + (this.agentTodo?.id || 'standalone'); }
  private async prepareAgent(todo?: ResourceRow): Promise<void> {
    const revision = ++this.agentFormRevision, generation = this.model.connectionGeneration(), scope = this.model.scope();
    this.readerShown = false; this.creatingAgent = false; this.selectedMachine = 0; this.newTask = ''; this.directory = ''; this.launcher = ''; this.agentTodo = todo;
    try {
      const owner = todo ? await this.model.viewFor(todo) : this.model;
      if (revision !== this.agentFormRevision || !this.model.owns(generation, scope)) return;
      this.agentModel = owner;
      const raw = await owner.loadView(this.agentDraftKey());
      if (revision !== this.agentFormRevision || !this.model.owns(generation, scope)) return;
      const saved = Wire.parse(raw); this.newTask = Wire.text(saved, 'task', todo ? todo.title + (Wire.text(todo.data(), 'notes') ? '\n\n' + Wire.text(todo.data(), 'notes') : '') : '');
      this.directory = Wire.text(saved, 'directory'); this.launcher = Wire.text(saved, 'launcher');
      this.sheetKind = 'newagent'; this.showSheet = true;
    } catch (error) { if (revision === this.agentFormRevision && this.model.owns(generation, scope)) this.model.reportError(error as Error); }
  }
  private saveAgentDraft(): void {
    if (this.sheetKind !== 'newagent' || !this.showSheet) return;
    const owner = this.agentModel, generation = owner.connectionGeneration(), scope = owner.scope();
    owner.saveView(this.agentDraftKey(), JSON.stringify(Wire.payload(['task', 'directory', 'launcher'], [this.newTask, this.directory, this.launcher])))
      .catch(() => { if (owner.owns(generation, scope)) owner.error = 'cache_unavailable'; });
  }
  private agentMachines(): ResourceRow[] { return this.agentModel.machines.filter((row: ResourceRow) => row.manager === this.agentModel.profile.manager && Wire.text(row.data(), 'trust') !== 'pending' && !Wire.bool(row.data(), 'pending_confirm') && !Wire.bool(row.data(), 'pending') && Wire.bool(row.data(), 'online')); }
  private agentFingerprint(): string { return JSON.stringify([this.agentModel.scope(), this.agentTodo?.id || '', this.agentMachines()[this.selectedMachine]?.id || '', this.newTask, this.directory, this.launcher]); }
  private confirmCreateAgent(): void {
    const machine = this.agentMachines()[this.selectedMachine]; if (!machine || this.creatingAgent) return;
    const revision = this.agentFormRevision, fingerprint = this.agentFingerprint(), owner = this.agentModel, generation = owner.connectionGeneration(), scope = owner.scope();
    this.getUIContext().showAlertDialog({ title: tr('new_agent'), message: owner.profile.name + ' · ' + owner.profile.manager + '\n' + (machine.title || machine.id) + '\n' + this.directory + '\n\n' + this.newTask,
      primaryButton: { value: tr('cancel'), action: () => {} }, secondaryButton: { value: tr('create'), action: () => {
        if (revision === this.agentFormRevision && owner.owns(generation, scope) && fingerprint === this.agentFingerprint()) this.createAgent();
      } } });
  }
  private async createAgent(): Promise<void> {
    const machine = this.agentMachines()[this.selectedMachine]; if (!machine || this.creatingAgent) return;
    const owner = this.agentModel, generation = owner.connectionGeneration(), scope = owner.scope(), rootGeneration = this.model.connectionGeneration(), rootScope = this.model.scope(), revision = this.agentFormRevision, fingerprint = this.agentFingerprint(), draftKey = this.agentDraftKey();
    const valid = (): boolean => owner.owns(generation, scope) && this.model.owns(rootGeneration, rootScope) && revision === this.agentFormRevision && this.showSheet && this.sheetKind === 'newagent' && fingerprint === this.agentFingerprint();
    this.creatingAgent = true;
    try {
      const row = await owner.createAgent(this.newTask, machine, this.directory, this.launcher, this.agentTodo?.id || '', valid);
      if (!valid()) return;
      this.showSheet = false; this.newTask = ''; owner.saveView(draftKey, '').catch(() => {}); this.openRow(row);
    } catch (error) { if (valid()) owner.reportError(error as Error); }
    finally { if (revision === this.agentFormRevision) this.creatingAgent = false; }
  }
  private routeRow(param: Object): ResourceRow { return NavigationContext.rowFrom(param as string); }
  private chatTitle(param: Object): string {
    const row = this.routeRow(param);
    const current = this.detailModel.sessions.concat(this.detailModel.conversations).find((item: ResourceRow) => item.key() === row.key());
    return current?.title || row.title || tr(row.kind === 'conversation' ? 'main_chat' : 'agents');
  }
  private async restoreTarget(name: string, param: Object): Promise<void> {
    const descriptor = NavigationContext.from(name, param as string);
    if (!descriptor.scoped) return;
    const key = this.childKey(name, param), revision = ++this.routeRevision, generation = this.model.connectionGeneration(), scope = this.model.scope();
    const keepContent = this.targetReady(name, param);
    if (!keepContent) { this.chatReady = false; this.detailRoute = ''; }
    try {
      let row = descriptor.row; const view = await this.model.viewFor(row);
      if (['session', 'conversation'].includes(row.kind) && !row.machine) {
        let resolved = view.conversations.concat(view.sessions).find((item: ResourceRow) => item.id === row.id || item.session === row.session && !!row.session);
        if (!resolved && view.connected) { await view.refresh(); resolved = view.conversations.concat(view.sessions).find((item: ResourceRow) => item.id === row.id || item.session === row.session && !!row.session); }
        if (!resolved) throw new Error('request_updated'); row = resolved;
      }
      if (revision !== this.routeRevision || this.activeChild !== key || !this.model.owns(generation, scope)) return;
      if (this.detailModel !== view) this.detailModel.hideConversation();
      this.detailModel = view;
      if (row.key() !== view.selected.key()) await view.select(row);
      else if (name === 'session' || name === 'conversation') { if (view.connected) view.showConversation(row); }
      if (revision !== this.routeRevision || this.activeChild !== key || !this.model.owns(generation, scope) || view.selected.key() !== row.key()) return;
      this.markDetailReady(key);
      if (name === 'assistant' || name === 'settings' && descriptor.page === 'voice') view.watchDecisions(true);
    } catch (error) { if (revision === this.routeRevision && this.activeChild === key && this.model.owns(generation, scope)) this.model.reportError(error as Error); }
  }
  private routeShown(name: string, param: Object): void {
    if (this.activeChild !== this.childKey(name, param)) this.dismissChatActions();
    this.activeChild = this.childKey(name, param); if (name === 'extension') this.extensionTitle = this.routeRow(param).title; if (name === 'todo') this.todoTitle = this.routeRow(param).title; this.shownName = name; this.shownParam = param as string;
    this.detailModel.watchDecisions(false);
    if (!['session', 'conversation'].includes(name)) this.detailModel.hideConversation(name !== 'tools');
    if (name === 'applications') this.applications.refresh();
    if (name === 'settings' && this.settingsKind(param) === 'appearance') this.ensureAgentWorkspace();
    const descriptor = NavigationContext.from(name, param as string);
    if (descriptor.scoped) this.restoreTarget(name, param);
    else { this.routeRevision++; this.markDetailReady(this.activeChild); if (name === 'resources') this.model.listResources(param as string); }
  }
  @Builder targetLoading(name: string, param: Object) {
    Column({ space: Tokens.gap }) {
      if (this.model.error) {
        Text(tr(this.model.error)).fontSize(Tokens.meta)
        Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('retry')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('retry', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('retry')).bindTips(tr('retry')).onClick(() => this.restoreTarget(name, param))
      } else LoadingProgress().width(Tokens.touch).height(Tokens.touch)
    }.width('100%').height('100%').justifyContent(FlexAlign.Center)
  }
  @Builder destinationTitle(name: string, param: Object) {
    if (this.mainDestination(name, param) && !this.wide()) this.peerTitle(true)
    else Text(this.destinationTitleText(name, param)).height(this.headerHeight()).fontSize(Tokens.title).fontWeight(FontWeight.Medium).maxLines(1).textOverflow({ overflow: TextOverflow.Ellipsis })
      .padding({ left: this.destinationBackHidden(name, param) ? Tokens.page : 0 })
  }
  private destinationBackHidden(name: string, param: Object): boolean {
    if (this.mainDestination(name, param) || name === 'session' && Wire.bool(this.routeRow(param).data(), 'navigation_root') && this.wide() && this.model.section === 1) return true;
    return this.wide() && this.stack.size() === 1 && this.stack.getAllPathName()[0] === name && this.stack.getParamByIndex(0) === param;
  }
  private destinationTitleText(name: string, param: Object): string {
    if (name === 'settings_root') return tr('settings');
    if (name === 'assistant') return tr(Wire.text(this.routeRow(param).data(), 'assistant_item_kind') === 'plan' ? 'assistant_feed_plan' : 'assistant_feed_receipt');
    return name === 'session' || name === 'conversation' ? this.chatTitle(param) : name === 'resources' ? tr(param === 'todo' ? 'todos' : param === 'inbox' ? 'inbox' : param === 'health' ? 'health_home_title' : 'knowledge') : name === 'applications' ? tr('applications') : name === 'extension' ? this.extensionTitle : name === 'todo' ? this.todoTitle || this.routeRow(param).title || this.routeRow(param).id : name === 'settings' ? this.settingsTitle(param) : name === 'tools' ? tr('workspace') : name === 'history' ? tr('interaction_history') : this.routeRow(param).title || this.routeRow(param).id;
  }
  @Builder destination(name: string, param: Object) {
    NavDestination() {
      if (name === 'session' || name === 'conversation') {
        if (this.targetReady(name, param)) Conversation({ model: this.detailModel, admission: (): boolean => this.activeChild === this.childKey(name, param) && this.targetReady(name, param), onOpenFile: (path: string) => this.openTools(path), active: this.activeChild === this.childKey(name, param), onOpenResource: (row: ResourceRow) => this.openFrom(name, param, row), onLink: (address: string) => this.openReader(address), showContextMenu: this.mainDestination(name, param) || this.hideChatTitle(name, param), onOpenCard: (itemId: string, kind: string) => { if (this.activeChild === this.childKey(name, param) && this.targetReady(name, param)) this.openAssistantCard(itemId, kind); }, onPrimaryStop: (shown: boolean) => { if (this.activeChild === this.childKey(name, param) && this.targetReady(name, param)) this.inlineStopIdentity = shown ? JSON.stringify([this.detailModel.scope(), this.detailModel.connectionGeneration(), this.detailModel.target()]) : ''; }, onShowActions: () => this.showChatActions(name, param), actionItems: this.chatMenuActions, actionsVisible: this.chatBodyMenuVisible && this.activeChild === this.childKey(name, param), actionsRequest: this.chatMenuRevision, onHideActions: (request: number) => this.closeChatActions(request) })
        else this.targetLoading(name, param)
      } else if (name === 'history') {
        if (this.targetReady(name, param)) InteractionHistoryPage({ model: this.detailModel, active: this.activeChild === this.childKey(name, param), onLink: (address: string) => this.openReader(address) })
        else this.targetLoading(name, param)
      } else if (name === 'tools') {
        if (this.targetReady(name, param)) SessionTools({ model: this.detailModel, onOpenLink: (address: string) => this.openReader(address), initialFile: Wire.text(Wire.parse(param as string), 'open_file'), onWorkspaceReady: (workspace: SessionWorkspace | undefined) => this.detailModel.setWorkspace(workspace) })
        else this.targetLoading(name, param)
      } else if (name === 'applications') {
        ExtensionNavigationPage({ directory: this.applications, onOpen: (entry: ExtensionNavigationEntry): void => { if (this.activeChild === this.childKey(name, param)) this.openApplication(entry); } })
      } else if (name === 'extension') {
        Column() {
          if (!this.applicationAllowed(param)) Row({ space: Tokens.gap }) {
            Text(tr('application_unavailable')).fontSize(Tokens.meta).layoutWeight(1)
            Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('retry')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('retry', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('retry')).bindTips(tr('retry')).enabled(!this.applications.loading).onClick(() => this.applications.refresh())
          }.width('100%').padding(Tokens.page)
          Column() {
        if (this.targetReady(name, param)) ExtensionPage({ model: this.detailModel, extensionId: this.routeRow(param).id, uiId: Wire.text(this.routeRow(param).data(), 'ui'), state: Wire.text(this.routeRow(param).data(), 'state'), context: Wire.object(this.routeRow(param).data(), 'context'), active: this.activeChild === this.childKey(name, param), authorized: this.applicationAllowed(param), refreshRevision: this.extensionRefreshRevision + (Wire.text(this.routeRow(param).data(), 'navigation_extension') ? this.applications.revision : 0), onTitle: (title: string) => { if (this.activeChild === this.childKey(name, param)) this.extensionTitle = title; }, onNavigate: (effect: ExtensionUINavigationEffect) => this.extensionNavigate(name, param, effect), onLink: (address: string) => { if (this.activeChild === this.childKey(name, param)) this.openReader(address); }, onBack: () => { if (this.activeChild === this.childKey(name, param)) { this.stack.pop(); this.savePath(); } } })
            else this.targetLoading(name, param)
          }.width('100%').layoutWeight(1).visibility(this.applicationAllowed(param) ? Visibility.Visible : Visibility.None)
        }.width('100%').height('100%')
      } else if (name === 'computer') {
        if (this.targetReady(name, param)) ComputerScreenPage({ model: this.detailModel, row: this.routeRow(param), active: this.activeChild === this.childKey(name, param), onClose: () => { if (this.activeChild === this.childKey(name, param)) { this.stack.pop(); this.savePath(); } } })
        else this.targetLoading(name, param)
      } else if (name === 'file') {
        if (this.targetReady(name, param)) RemoteDocumentPage({ model: this.detailModel, row: this.routeRow(param), active: this.activeChild === this.childKey(name, param), onLink: (address: string) => this.openReader(address) })
        else this.targetLoading(name, param)
      } else if (name === 'inbox' || name === 'todo_source' || name === 'todo_thread' || name === 'resources' && param === 'inbox') {
        if (name === 'resources' || this.targetReady(name, param)) InboxWorkspacePage({ model: name === 'resources' ? this.model : this.detailModel, row: name === 'resources' ? undefined : this.routeRow(param), list: name === 'resources', visible: this.activeChild === this.childKey(name, param), refreshRevision: this.inboxRefreshRevision, handoffRequested: this.pendingInboxHandoff === this.childKey(name, param), onHandoffConsumed: () => { if (this.pendingInboxHandoff === this.childKey(name, param)) this.pendingInboxHandoff = ''; },
          onChanged: () => { if (this.activeChild === this.childKey(name, param)) this.inboxRefreshRevision++; }, onOpen: (row: ResourceRow) => this.openFrom(name, param, row), onHandoff: (row: ResourceRow) => this.openFrom(name, param, row), onLink: (address: string) => this.openReader(address) })
        else this.targetLoading(name, param)
      } else if (name === 'todo' || name === 'resources' && param === 'todo') {
        if (name === 'resources' || this.targetReady(name, param)) TodoWorkspacePage({ model: name === 'resources' ? this.model : this.detailModel, row: name === 'todo' ? this.routeRow(param) : undefined, list: name === 'resources', visible: this.activeChild === this.childKey(name, param),
          onTitle: (title: string) => { if (name === 'todo' && this.activeChild === this.childKey(name, param)) this.todoTitle = title; }, onOpen: (row: ResourceRow) => this.openFrom(name, param, row), onOpenResource: (row: ResourceRow) => this.openFrom(name, param, row), onLink: (address: string) => this.openReader(address), onCreateAgent: (todo: ResourceRow) => this.createAgentFrom(name, param, todo) })
        else this.targetLoading(name, param)
      } else if (name === 'knowledge' || name === 'resources' && param === 'knowledge') {
        if (name === 'resources' || this.targetReady(name, param)) KnowledgeWorkspacePage({ model: name === 'resources' ? this.model : this.detailModel, row: name === 'knowledge' ? this.routeRow(param) : undefined, visible: this.activeChild === this.childKey(name, param),
          onOpenResource: (row: ResourceRow) => this.openFrom(name, param, row), onChat: (row: ResourceRow) => this.openFrom(name, param, row), onLink: (address: string) => this.openReader(address) })
        else this.targetLoading(name, param)
      } else if (name === 'resources' && param === 'health') {
        HealthWorkspacePage({ model: this.model, onOpenResource: (row: ResourceRow) => this.openFrom(name, param, row), onChat: (row: ResourceRow) => this.openFrom(name, param, row), onLink: (address: string) => this.openReader(address) })
      } else if (name === 'resources' || name === 'resource') {
        if (name === 'resources' || this.targetReady(name, param)) ResourcePage({ model: name === 'resources' ? this.model : this.detailModel, onLink: (address: string) => this.openReader(address), kind: name === 'resources' ? param as string : this.routeRow(param).kind, list: name === 'resources', onOpen: (row: ResourceRow) => this.openFrom(name, param, row),
          onHandoff: (row: ResourceRow) => this.handoffResource(row) })
        else this.targetLoading(name, param)
      } else if (name === 'settings_root') {
        this.settingsList()
      } else if (name === 'assistant') {
        if (this.targetReady(name, param)) AssistantDetailPage({ model: this.detailModel, active: this.activeChild === this.childKey(name, param), itemId: Wire.text(this.routeRow(param).data(), 'assistant_item_id'), kind: Wire.text(this.routeRow(param).data(), 'assistant_item_kind'), onOpenResource: (row: ResourceRow) => this.openFrom(name, param, row), onLink: (address: string) => this.openReader(address) })
        else this.targetLoading(name, param)
      } else if (name === 'settings' && (this.settingsKind(param) === 'phone' || this.settingsKind(param) === 'voice')) {
        if (!NavigationContext.from(name, param as string).scoped || this.targetReady(name, param)) DevicePage({ model: this.settingsModel(param), page: this.settingsKind(param), active: this.activeChild === this.childKey(name, param), onLeave: (owner: ClientModel) => this.restoreDeviceOwner(owner), onLink: (address: string) => this.openReader(address) })
        else this.targetLoading(name, param)
      } else if (name === 'settings' && this.isAdminPage(this.settingsKind(param))) {
        if (!NavigationContext.from(name, param as string).scoped || this.targetReady(name, param)) AdminWorkspacePage({ model: this.settingsModel(param), page: this.settingsKind(param), row: NavigationContext.from(name, param as string).scoped ? NavigationContext.from(name, param as string).row : undefined, active: this.activeChild === this.childKey(name, param), refreshRevision: this.adminRefreshRevision,
          onOpenResource: (row: ResourceRow) => this.openFrom(name, param, row), onOpenMachine: (row: ResourceRow) => { if (this.activeChild === this.childKey(name, param)) this.openSettingsDetail('machines', row); } })
        else this.targetLoading(name, param)
      } else if (name === 'settings' && this.settingsKind(param) === 'ssh') {
        SshSettingsPage({ tunnel: this.settingsModel(param).ssh, active: this.activeChild === this.childKey(name, param),
          onSaved: (): void => { const owner = this.settingsModel(param); if (this.activeChild === this.childKey(name, param)) owner.selectProfile(owner.profile, true); } })
      } else if (name === 'settings') {
        if (!NavigationContext.from(name, param as string).scoped || this.targetReady(name, param)) SettingsPage({ model: this.settingsKind(param) === 'appearance' ? this.model : this.settingsModel(param), agentWorkspace: this.agentWorkspace, onChat: (row: ResourceRow) => { if (this.activeChild === this.childKey(name, param)) this.openLinkedResource(row); }, active: this.activeChild === this.childKey(name, param), page: this.settingsKind(param), onDetail: (page: string, row: ResourceRow) => { if (this.activeChild === this.childKey(name, param)) this.openSettingsDetail(page, row); }, onExtension: (extensionId: string, uiId: string, title: string) => { if (this.activeChild === this.childKey(name, param)) this.openExtension(this.settingsModel(param), extensionId, uiId, title); }, onBack: () => { if (this.activeChild === this.childKey(name, param)) { this.stack.pop(); this.savePath(); } }, onComputer: (row: ResourceRow) => { if (this.activeChild === this.childKey(name, param)) this.openComputer(row); }, onOpen: (page: string) => this.openSettings(page, NavigationContext.from(name, param as string).scoped) })
        else this.targetLoading(name, param)
      }
    }.title({ builder: () => this.destinationTitle(name, param), height: this.headerHeight() }, { systemMaterial: this.titleMaterial })
      .hideTitleBar(this.hideChatTitle(name, param)).hideBackButton(this.destinationBackHidden(name, param))
      .backgroundColor(name === 'resources' && param === 'inbox' ? Colors.grouped : Colors.canvas)
      .menus(this.destinationMenus(name, param))
      .onShown(() => this.routeShown(name, param))
      .onHidden(() => { if (this.pendingInboxHandoff === this.childKey(name, param)) this.pendingInboxHandoff = ''; if (this.activeChild === this.childKey(name, param)) { this.activeChild = ''; this.dismissChatActions(); } if ((name === 'session' || name === 'conversation') && this.routeRow(param).key() === this.detailModel.selected.key()) this.detailModel.hideConversation(false); if (name === 'assistant') this.detailModel.watchDecisions(false); if (name === 'tools') this.detailModel.releaseTerminal(); })
      .onBackPressed((): boolean => { if (this.pendingInboxHandoff === this.childKey(name, param)) this.pendingInboxHandoff = ''; this.detailModel.releaseTerminal(); this.stack.pop(); this.savePath(); return true; })
  }
  build() {
    Stack() {
    WithTheme({ theme: nativeLookTheme(this.look, this.model.style, this.environment.dark), colorMode: this.environment.dark ? ThemeColorMode.DARK : ThemeColorMode.LIGHT }) {
    ThemeScope() {
    Column() {
      if (!this.model.profile.id) {
        Column({ space: Tokens.section }) {
          Text(tr('welcome')).fontSize(Tokens.title).fontWeight(FontWeight.Bold)
          if (this.model.error) Text(tr(this.model.error)).fontSize(Tokens.meta).fontColor($r('app.color.persona_danger'))
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('connect')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('connect', true)]) }.attributeModifier(ButtonStyles.iconPrimary).accessibilityText(tr('connect')).bindTips(tr('connect')).onClick(() => this.openSheet('connect')).id('welcome.connect')
          Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('experience')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('experience', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('experience')).bindTips(tr('experience')).onClick(() => this.model.useDemo()).id('welcome.demo')
          if (this.model.pendingCleanupCount > 0) Button({ type: ButtonType.Circle }) { SymbolGlyph(IconActions.symbol('retry_cleanup')).fontSize(Tokens.symbolFont).fontColor([IconActions.color('retry_cleanup', false)]) }.attributeModifier(ButtonStyles.icon).accessibilityText(tr('retry_cleanup')).bindTips(tr('retry_cleanup')).onClick(() => this.model.retryCleanup())
        }.justifyContent(FlexAlign.Center).width('100%').height('100%')
          .bindSheet($$this.showSheet, this.sheetContent(), { title: { title: this.sheetTitle() }, height: SheetSize.LARGE, showClose: true })
      } else {
        Navigation(this.stack) { this.primaryContent() }.title({ builder: this.peerTitle, height: this.headerHeight() }, { systemMaterial: this.titleMaterial }).titleMode(NavigationTitleMode.Mini)
          .hideBackButton(true).hideTitleBar(this.rail()).menus([])
          .backgroundColor(Colors.canvas)
          .toolbarConfiguration(this.rail() ? [{ value: tr('expand'), icon: $r('sys.media.ohos_ic_public_arrow_right'), action: () => this.collapsed = false }] : [])
          .mode(this.wide() ? NavigationMode.Split : NavigationMode.Stack)
          .navBarWidth(this.rail() ? Tokens.collapsed : Tokens.master).navBarWidthRange(this.rail() ? [Tokens.collapsed, Tokens.collapsed] : [Tokens.masterMin, Tokens.masterMax])
          .minContentWidth(Tokens.detailMin).enableDragBar(!this.rail()).navDestination(this.destination)
          .onAreaChange(() => this.updateBreakpoint()).width('100%').height('100%')

          .bindSheet($$this.showSheet, this.sheetContent(), { title: { title: this.sheetTitle() }, height: SheetSize.LARGE, showClose: true })
      }
    }.width('100%').height('100%')
      .bindSheet($$this.spacesShown, this.spacesContent(), { title: { title: tr('conversation_spaces') }, height: SheetSize.LARGE, showClose: true, onAppear: () => { this.spacesLayoutReady = true; this.measureSpaces(); }, onDisappear: () => this.spacesLayoutReady = false })
    }
    }
    }.width('100%').height('100%').backgroundColor(Colors.canvas)
      .bindSheet($$this.readerShown, this.readerContent(), { title: { title: this.readerTitle }, height: Tokens.readerFull, width: Tokens.readerFull, preferType: SheetType.BOTTOM,
        detents: [Tokens.readerFull, Tokens.readerPartial], enableOutsideInteractive: true, showClose: true })
  }
}
