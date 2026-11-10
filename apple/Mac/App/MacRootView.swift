import BeautifulUI
import SwiftUI

/// The window's sidebar sections: the iPhone's four tabs, plus Knowledge and Machines (on the phone a page inside
/// Persona, and Machines inside Agents / Settings), which a Mac with room for a sidebar shows as sections of their own
/// (docs/apple/mac.md「Main window」), and Messages while Settings › Lark › Show Messages on Home is on (the iPhone's extra tab, #392). ⌘1…⌘8: see
/// `shortcutOrder`.
enum MacSection: String, CaseIterable, Identifiable {
    case assistant, agents, todos, messages, inbox, knowledge, machines, settings

    var id: String { rawValue }

    /// The sections in ⌘ order. ⌘1…⌘6 stay where they were; 「待办」 (added later) takes ⌘7, 「消息」 ⌘8. The sidebar shows
    /// `allCases` (To-dos after Agents, Messages before Inbox as on the iPhone), so the two orders are kept apart.
    static let shortcutOrder: [MacSection] = [.assistant, .agents, .inbox, .knowledge, .machines, .settings, .todos, .messages]

    /// The ⌘ number of the section, 1-based.
    var shortcut: Int? { MacSection.shortcutOrder.firstIndex(of: self).map { $0 + 1 } }

    /// The sections the window shows: 「待办」 only when the hub has todos (an old hub answers 404, #397), 「消息」 only while
    /// its switch is on and the hub has Feishu chats (`LarkHomePreference.shows`).
    static func visible(supportsTodos: Bool, showsMessages: Bool = false) -> [MacSection] {
        allCases.filter { (supportsTodos || $0 != .todos) && (showsMessages || $0 != .messages) }
    }

    /// What the window shows right now (the shell's sidebar, the ⌘ menu, ⌘K).
    @MainActor static var visibleNow: [MacSection] {
        visible(supportsTodos: TodoStore.shared.isSupported, showsMessages: LarkHomePreference.shared.shows)
    }

    /// The iPhone tab it is; nil for the Mac's own sections.
    var tab: AppTab? {
        switch self {
        case .assistant: .assistant
        case .agents: .chats
        case .inbox: .mail
        case .settings: .settings
        case .todos, .messages, .knowledge, .machines: nil
        }
    }

    /// The section on screen: one of the Mac's own while picked, else the tab's (a notification, ⌘K or a link that
    /// moves the tab moves the section with it, MacShell clears the pick).
    static func shown(override: MacSection?, tab: AppTab) -> MacSection { override ?? MacSection(tab) }

    /// What picking a section changes: the tab, or the Mac-side pick.
    static func pick(_ s: MacSection) -> (tab: AppTab?, override: MacSection?) {
        if let tab = s.tab { return (tab, nil) }
        return (nil, s)
    }

    /// The Mac-side pick across launches (the tabs are AppModel's to remember).
    static let storageKey = "mac.section"
    /// View › Hide Sidebar (⌃⌘S): the sections column folded away, across launches.
    static let sidebarHiddenKey = "mac.sidebar.hidden"
    static func remember(_ s: MacSection?, in d: UserDefaults = .standard) {
        // Not at once: a write to UserDefaults draws every `AppStorage` view in every kept page again (#390).
        DeferredDefaults.set(s.flatMap { $0.tab == nil ? $0.rawValue : nil }, forKey: storageKey, in: d)
    }
    static func remembered(in d: UserDefaults = .standard) -> MacSection? {
        d.string(forKey: storageKey).flatMap(MacSection.init(rawValue:)).flatMap { $0.tab == nil ? $0 : nil }
    }

    init(_ tab: AppTab) {
        switch tab {
        case .assistant: self = .assistant
        case .chats: self = .agents
        case .mail: self = .inbox
        case .settings: self = .settings
        }
    }

    var title: String {
        switch self {
        case .assistant: "Persona"
        case .agents: "Agents"
        case .todos: String(localized: "To-dos")
        case .messages: String(localized: "Messages [Quote Views 4da199]")
        case .inbox: String(localized: "Inbox")
        case .knowledge: String(localized: "Knowledge")
        case .machines: String(localized: "Machines")
        case .settings: String(localized: "Settings")
        }
    }

    var icon: String {
        switch self {
        case .assistant: Theme.Icon.ai
        case .agents: Theme.Icon.agents
        case .todos: Theme.Icon.todo
        case .messages: Theme.Icon.feishuChats
        case .inbox: Theme.Icon.inbox
        case .knowledge: Theme.Icon.knowledge
        case .machines: Theme.Icon.machines
        case .settings: Theme.Icon.settings
        }
    }
}

/// The window: unpaired, the welcome page (pairing, Demo Mode); paired, a sidebar with the four sections beside the
/// section's page (the iOS pages, `horizontalSizeClass` regular).
struct MacRootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("MacRootView")
        #endif
        Group {
            if let ref = model.pairingEngineMachine {
                PairingEngineView(ref: ref)
            } else if model.isPaired, model.showsChecklist {
                // Just paired: what the main conversation still needs, before the app (#4195).
                OnboardingChecklistView()
            } else if model.isPaired {
                MacShell()
            } else {
                WelcomeView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(minWidth: Theme.Size.macWindowMinWidth, minHeight: Theme.Size.macWindowMinHeight)
        .task(id: model.checklistPending && model.hubFeatures.contains(AssistantReadiness.feature)) {
            if model.checklistPending { await model.refreshReadiness() }
        }
        .extensionSystemPresentation()
        .macLook(model)
        .loadsChannels() // the names under a message that came in from a channel (「来自 微信」)
        .overlay(alignment: .top) { ToastOverlay() }
        .alert("Claude Wants to Run on This Mac", isPresented: Binding(
            get: { model.pendingConfirmation != nil },
            // Gone without an answer: Deny for the one this alert showed. Its own button may have answered it already,
            // and the next one may be on screen by the time this comes: that one is not touched (#4456).
            set: { [shown = model.pendingConfirmation?.id] in if !$0, let shown { model.phoneConfirmations.answer(shown, false) } }
        ), presenting: model.pendingConfirmation) { c in
            Button("Allow [Notification Actions ce7ef2]") { model.phoneConfirmations.answer(c.id, true) }
            Button("Deny", role: .cancel) { model.phoneConfirmations.answer(c.id, false) }
        } message: { c in
            Text(c.summary)
        }
        .sendRefusalAlert(model)
        .overlay { if model.isPaired { MacQuickOpenLayer() } }
        .background {
            // ⌘K's Files: the viewer a file in the conversation opens. On a view of its own: a second sheet on the
            // root next to New Agent's crashed the window's layout when a chat opened.
            Color.clear.sheet(item: Binding(get: { MacShellState.shared.file }, set: { MacShellState.shared.file = $0 })) { ref in
                FileViewer(ref: ref, key: MacShellState.shared.fileKey)
                    .environment(model)
            }
        }
        .sheet(isPresented: Binding(get: { MacShellState.shared.newAgent }, set: { MacShellState.shared.newAgent = $0 })) {
            // New Agent (⌘N, the menu bar, ⌘K): the iOS form in a window sheet; the new agent opens in Agents.
            NavigationStack {
                NewChatView { session in
                    MacShellState.shared.newAgent = false
                    MacShellState.shared.open(session.key, model)
                }
            }
            .macSheet()
            .environment(model)
        }
        // Add Computer / Confirm Computer from anywhere (the readiness card, the Agents list, the menu bar's File › Add Computer…).
        .addComputerPresentation()
        #if DEBUG
        .preferredColorScheme(MacSnapshot.colorScheme)
        #endif
    }
}

extension View {
    /// What RootView sets on the whole tree on iOS: the spec's tint, font design and theme, the text size, and a
    /// rebuild when the style, color or persona changes (tokens are static). The text size rebuilds it too on the
    /// Mac: the fonts read `Theme.Font.scale`, which SwiftUI does not watch (#833).
    func macLook(_ model: AppModel) -> some View {
        id(model.look)
            .id(model.textSize)
            .tint(Theme.accent)
            .fontDesign(model.style.fontDesign)
            .dynamicTypeSize(model.textSize.applied(to: .large))
            .buiTheme(.persona())
            .scrollIndicators(.automatic)
            // Every iOS Form in the window grouped (System Settings' look; a sheet is its own window and needs
            // `macSheet()`, 2026-11-02): the Mac's default two-column
            // form put the iPhone's section rows (pickers, editors) in the label column and cut them off.
            .formStyle(.grouped)
    }
}

/// The sections column and the section's page side by side. Not a `NavigationSplitView`: the pages are split views
/// themselves (Agents, Inbox, Settings in regular width; Persona lays its list beside it, `MacListBeside`), and a split nested in another one's detail column laid its
/// own detail out as if the outer sidebar were still in front of it (the chat sat a sidebar's width to the right,
/// its composer cut off). The page's split is the window's; this column sits at its leading edge.
private struct MacShell: View {
    @Environment(AppModel.self) private var model
    private var shell: MacShellState { .shared }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("MacShell")
        #endif
        HStack(spacing: 0) {
            // View › Hide Sidebar folds it away; ⌘1…6 still move between the sections.
            // The divider runs up into the title bar's row too: stopping under it, the page's white showed past the
            // column's edge there, a step the user saw as a white bump (#830).
            if !sidebarHidden { sections; Divider().ignoresSafeArea(.container, edges: .top) }
            page
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                // The section's title in the title bar's row, at this column's edge (not over the sections column):
                // the row was left empty when the system's title went (#830). Persona's page puts its own header there.
                // A pushed page that draws its own title there (`pageTitle`, #855) takes the row instead.
                // The sections' pages put it there themselves (`MacPageRoot`).
                .overlayPreferenceValue(MacTitleRowTaken.self, alignment: .topLeading) { taken in
                    if let p = pluginPlace, !taken {
                        MacPageTitle(title: p.title, leading: sidebarHidden ? Theme.Size.macTitleAfterControls : Theme.Space.l)
                    }
                }
                // Pages that draw their own header in that row (Persona's) start it here.
                .environment(\.macTitleRowLeading, sidebarHidden ? Theme.Size.macTitleAfterControls : 0)
                // The connection state lives at the sidebar's foot; folded away, it comes to the page's (only while
                // the hub is out of reach: Knowledge and Machines have no title spot of their own that says so).
                .safeAreaInset(edge: .bottom, spacing: 0) {
                    if sidebarHidden { MacConnectionFooter().background(Theme.systemSidebar) }
                }
        }
        // No toolbar band across the window: the sections column reaches up behind the traffic lights, and the bar's
        // backdrop and hairline cut it off at the toolbar (2026-11-02).
        .toolbarBackgroundVisibility(.hidden, for: .windowToolbar)
        // The plugins' sidebar entries and cards (docs/extensions/extensions.md「UI slots」): with a hub that has them.
        .task(id: ExtensionSlots.source(model)) {
            let hub = ExtensionSlots.source(model).hub
            await ExtensionSlots.shared.reload(for: hub)
            // Which inbox sources a plugin draws (placement inbox_item): known before an item is opened.
            await InboxItemSlots.shared.reload(for: hub)
        }
        .extensionThemes()
        // A page whose plugin took it away (disabled, uninstalled) goes back to the section.
        .onChange(of: ExtensionSlots.shared.pages) { if shell.pluginPage != nil, pluginPlace == nil { shell.pluginPage = nil } }
        // No page title in the bar: the system puts it after the items beside the traffic lights, over this column, and
        // a long one ran across the column's edge on a white backing (2026-11-02 user: the text all spills out). The selected
        // section and the selected row say where you are; the window keeps its title for the Window menu.
        .toolbar(removing: .title)
        // Next to the traffic lights, over the sections column: shows or hides that column (View › Show/Hide Sidebar,
        // ⌃⌘S). The pages' lists have no button of their own: one there sat over this column and hid the list.
        .toolbar {
            ToolbarItemGroup(placement: .navigation) {
                Button { withAnimation(Theme.Motion.layout) { sidebarHidden.toggle() } } label: {
                    Label(sidebarHidden ? String(localized: "Show Sidebar") : String(localized: "Hide Sidebar"), systemImage: Theme.Icon.sidebar)
                }
                .help(sidebarHidden ? String(localized: "Show Sidebar") : String(localized: "Hide Sidebar"))
                // The Agents list's menu (filter, grouping, folding) right after it, in this group. As the list's own
                // `.navigation` item it jumped in front of this button whenever a page in the detail column brought an
                // item of its own (a space's ⋯, a machine's ⓘ): the window's toolbar is rebuilt then, list items first.
                // Only while Agents shows (with the pages' own items, a turn after the page, `MacPageStack`).
                if pluginPlace == nil, (chrome.toolbarSection ?? shown) == .agents, let actions = shell.agentsList {
                    AgentsListMenu(actions: actions)
                }
            }
            // A plain glyph beside the traffic lights: in the system's glass circle it stood out of the sections column as
            // a white bump (2026-11-03 user).
            .sharedBackgroundVisibility(.hidden)
            // What the title's room did: the pages' actions stay at the trailing end, by the search field (without
            // it they lined up at the leading edge, over this column).
            ToolbarSpacer(.flexible)
        }
        // Something moved the tab (a notification, ⌘K, a link): its section comes to the front.
        .onChange(of: model.selectedTab) { shell.section = nil; shell.pluginPage = nil }
        .onChange(of: model.openChat) { if model.openChat != nil { shell.section = nil; shell.pluginPage = nil } }
        .onChange(of: TodoStore.shared.isSupported) { _, supported in
            // An old hub has no todos: a window still on the todos section (or remembering it) goes back to the tab's.
            if !supported, shell.section == .todos {
                shell.section = nil
                MacSection.remember(nil)
            }
        }
        .onChange(of: LarkHomePreference.shared.shows) { _, shows in
            // Show Messages on Home switched off (or the hub lost its Feishu chats): the window leaves Messages as the iPhone leaves its tab.
            if !shows, shell.section == .messages {
                shell.section = nil
                MacSection.remember(nil)
            }
        }
        .onChange(of: model.assistantCurrent) { shell.section = nil; shell.pluginPage = nil }
        .onChange(of: sidebarHidden, initial: true) { _, hidden in if chrome.sidebarHidden != hidden { chrome.sidebarHidden = hidden } }
        .onAppear { chrome.openWindow = openWindow }
        .onChange(of: scenePhase, initial: true) { _, phase in if chrome.scenePhase != phase { chrome.scenePhase = phase } }
    }

    @StoredValue(MacSection.sidebarHiddenKey) private var sidebarHidden = false
    @Environment(\.openWindow) private var openWindow
    @Environment(\.scenePhase) private var scenePhase
    /// What the kept pages get from the shell (`MacPageStack`).
    @State private var chrome = MacPageChrome()

    /// The section on screen: the one picked in the column, else the tab's.
    private var shown: MacSection {
        let s = MacSection.shown(override: shell.section, tab: model.selectedTab)
        // A remembered Messages while its switch is off (or before the hub says it has Feishu chats): the tab's section.
        return s == .messages && !LarkHomePreference.shared.shows ? MacSection(model.selectedTab) : s
    }

    /// The sections, with the rows the Persona page's own list draws (`AssistantSidebar`): the same icon box, type and
    /// rounded selection (2026-11-02 user: the system's source list beside it looked unstyled). Flush with the window's
    /// edges, up behind the traffic lights; inset from them it looked like a card on a backing board (same day).
    private var sections: some View {
        return VStack(spacing: Theme.Space.xxs) {
            ForEach(MacSection.visibleNow) { section in
                // The plugins' pages go with Machines and Settings, right before Settings (docs/extensions/extensions.md「UI slots」).
                if section == .settings { pluginRows }
                MacSidebarRow(icon: section.icon, title: section.title, selected: section == shown && shell.pluginPage == nil,
                              badge: badge(section), identifier: "mac.section.\(section.rawValue)") { shell.go(section, model) }
            }
            Spacer(minLength: 0)
            // The subscription accounts' windows (docs/server/usage-windows.md), as the web's sidebar ends; nothing without any.
            MacUsageFooter()
            // The sidebar's foot: while the hub is out of reach, Waiting for Network / Connecting… (a click explains), as the
            // iPhone's title spot does (docs/apple/mac.md「Main window」).
            MacConnectionFooter()
        }
        // Below the traffic lights at a fixed height: the title bar is taller on pages with toolbar items, and rows
        // that followed it jumped up and down between sections.
        .padding(.top, Theme.Size.macToolbarHeight)
        .padding(.horizontal, Theme.Space.s)
        .padding(.bottom, Theme.Space.s)
        .frame(width: Theme.Size.macSidebarIdeal)
        .frame(maxHeight: .infinity, alignment: .top)
        .ignoresSafeArea(.container, edges: .top)
        .background(Theme.systemSidebar)
        .transition(.move(edge: .leading))
    }

    /// The plugins' main-navigation entries (nav_page): icon, title on one line, the plugin's badge.
    private var pluginRows: some View {
        ForEach(ExtensionSlots.shared.pages) { p in
            let key = ExtensionSlots.key(p)
            MacSidebarRow(icon: ExtensionSlots.icon(p.icon), title: p.title, selected: shell.pluginPage == key,
                          badge: p.badge.map { Int($0.text).map(MacSidebarRow.Badge.count) ?? .word($0.text) },
                          identifier: "mac.plugin.\(key)") { shell.pluginPage = key }
        }
    }

    /// The plugin page picked in the sidebar, while its plugin still has it.
    private var pluginPlace: ExtensionUIPlace? {
        shell.pluginPage.flatMap { k in ExtensionSlots.shared.pages.first { ExtensionSlots.key($0) == k } }
    }

    /// The iOS tab bar's badges (`RootView`).
    private func badge(_ section: MacSection) -> MacSidebarRow.Badge? {
        let n: Int
        switch section {
        case .knowledge, .machines, .messages: return nil // Messages: the iPhone's tab has no badge either
        case .todos: n = TodoStore.shared.badgeCount
        case .assistant: n = model.badges.assistant
        case .agents: n = model.badges.agents
        case .inbox: n = InboxStore.shared.unreadCount
        case .settings: return model.isDemo ? .word(String(localized: "Demo", comment: "Sidebar badge: demo mode is on")) : nil
        }
        return n > 0 ? .count(n) : nil
    }

    /// The sections' pages stay under a plugin's page, all behind it: coming back from it builds none of them again.
    private var page: some View {
        // Over them, not beside them in a stack: the sections' pages are laid out as before, the plugin's on top.
        sectionPage.overlay {
            if let p = pluginPlace {
                NavigationStack {
                    ExtensionPageView(plugin: p.owner, ui: p.id, title: p.title)
                        .environment(\.macBelowTitleRow, true)
                }
                .id(ExtensionSlots.key(p))
                .background(Theme.pageBackground)
            }
        }
    }

    /// Kept in the window once shown (#390, `MacPageStack`).
    private var sectionPage: some View {
        MacPageStack(shown: pluginPlace == nil ? shown : nil, chrome: chrome) { section in
            AnyView(sectionView(section).environment(model).macLook(model))
        }
        // Up behind the title bar: each page's hosting view takes the window's title bar as its own safe area, as the
        // page did in the window's tree (its title in that row, its content below it).
        .ignoresSafeArea(.container, edges: .top)
    }

    @ViewBuilder private func sectionView(_ section: MacSection) -> some View {
        switch section {
        case .assistant: AssistantView() // its own headers are the title bar's row (#830, `AssistantView`)
        case .agents: ChatsView()
        case .todos: MacTodosPage()
        case .messages: MacLarkMessagesPage() // the chats beside the open one (#3162)
        case .inbox: MacInboxPage()
        case .knowledge: MacKnowledgePage() // the list and the open entry side by side (#2318)
        case .machines: MacMachinesPage()
        case .settings: SettingsView()
        }
    }
}

extension EnvironmentValues {
    /// Where a page's own header in the title bar's row may start (#830): past the traffic lights and the toolbar's
    /// buttons when the sections column is hidden and the page is at the window's edge, else 0.
    @Entry var macTitleRowLeading: CGFloat = 0
}

/// The sidebar's Machines: the machines page, pushing a machine's page; its agents open their chats here too
/// (SessionKey), as on the phone.
struct MacMachinesPage: View {
    @State private var path = NavigationPath()

    var body: some View {
        NavigationStack(path: $path) {
            MachinesView()
                .environment(\.macBelowTitleRow, true) // below the section's title, not scrolling through it (#1243)
                .navigationDestination(for: MachineDetailRoute.self) { r in MachineDetailView(ref: r.ref) }
                .navigationDestination(for: SessionKey.self) { key in ChatView(key: key).id(key) }
        }
        .environment(\.macSectionStack, true) // a machine's page: its ‹ and name in the title row (#2318)
        #if DEBUG
        // `-macSection machines -openMonitor <machine id>` (or `-openMachineDetail <id>`): that machine's page.
        .onAppear {
            guard path.isEmpty else { return }
            let d = UserDefaults.standard
            if let m = d.string(forKey: "openMonitor"), m != "all" { path.append(MachineDetailRoute(id: m)) }
            else if let m = d.string(forKey: "openMachineDetail") { path.append(MachineDetailRoute(id: m)) }
        }
        #endif
    }
}

/// The connection state at the sidebar's foot; nothing while connected.
struct MacConnectionFooter: View {
    @Environment(AppModel.self) private var model
    @State private var details = false

    var body: some View {
        let phase = model.connection.shown
        if phase != .online {
            Button { details = true } label: {
                ConnectionLabel(phase: phase, compact: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, Theme.Space.l)
                    .padding(.vertical, Theme.Space.s)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .help("View connection details")
            .accessibilityLabel("\(phase.title), view connection details")
            .connectionDetails($details)
        }
    }
}
