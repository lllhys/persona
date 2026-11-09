import SwiftUI

/// The sidebar's Messages (#3162): the iPhone's Messages tab (Settings › Lark › Show Messages on Home, #392 / #530) laid out as the Mac's
/// list pages are (docs/apple/mac-design.md): the Feishu chats in a column of their own, the open chat beside them. Not a
/// second Feishu client: the same list model (`LarkConversationsModel`), rows (`LarkConversationRow`), account chips
/// and pinned shelf as the iPhone's page, and the inbox's chat (`LarkChatView`) on the right. The list's width is
/// dragged and remembered (`MacListBeside`); which chat is open is the page's own state.
struct MacLarkMessagesPage: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let config = model.config {
            MacLarkAccountsPage(config: config).id(config)
        }
    }
}

/// With more than one Feishu account signed in, chips over the list pick whose chats it shows (docs/extensions/feishu.md「多账号」).
private struct MacLarkAccountsPage: View {
    @State private var choice: LarkAccountChoice

    init(config: ServerConfig) {
        _choice = State(initialValue: LarkAccountChoice(deps: .live(config)))
    }

    var body: some View {
        MacLarkMessagesContent(config: choice.config, choice: choice).id(choice.requestAccount)
    }
}

private struct MacLarkMessagesContent: View {
    @Environment(AppModel.self) private var model
    @State private var store: LarkConversationsModel
    @State private var open: LarkConversation?
    @State private var query = ""
    @State private var pins = LarkPinnedInteraction()
    @State private var canExpandPins = false
    /// The end of the list is on screen: the next page loads, and keeps loading while it stays there.
    @State private var endVisible = false
    /// Pages a search has asked for since the query last changed (`searchPageLimit`).
    @State private var searchPages = 0
    var choice: LarkAccountChoice

    /// The iPhone page's limits (`LarkConversationsView`, #2823): a search reads on through this many pages by itself,
    /// events refresh at most once a second, the next page is asked for this many rows before the end.
    private static let searchPageLimit = 8
    private static let refreshGap: Duration = .seconds(1)
    private static let prefetchRows = 10

    init(config: ServerConfig, choice: LarkAccountChoice) {
        self.choice = choice
        _store = State(initialValue: LarkConversationsModel(deps: .live(config, account: choice.requestAccount,
                                                                          accountName: choice.selectedName)))
    }
    private var display: LarkMessageDisplay { model.hub?.larkDisplay.policy ?? .hidden }

    private var filtered: [LarkConversation] {
        LarkConversationsModel.listed(store.conversations, pinned: store.pinned, query: query, more: store.nextCursor != nil)
    }

    var body: some View {
        MacListBeside(listShown: true, widthKey: "mac.messages.listWidth", ideal: Theme.Size.macMessagesListIdeal) {
            list
        } page: {
            MacLarkChatColumn(conversation: open)
        }
        .background(Theme.pageBackground)
        .onChange(of: store.accountID) { _, _ in open = nil; pins = LarkPinnedInteraction() }
        .onChange(of: query) { _, _ in
            pins = LarkPinnedInteraction()
            searchPages = 0
            if endVisible { Task { await loadAtEnd() } }
        }
        // A page held for a moving list lands when it rests: the end may still be on screen and want the next one.
        .onChange(of: store.nextCursor) { _, cursor in if cursor != nil, endVisible { Task { await loadAtEnd() } } }
        .task {
            // Read once; a change comes as a settings event (AppModel).
            if let link = model.hub, link.larkDisplay.snapshot == nil { await link.larkDisplay.refresh(using: link.api) }
        }
        .tabActiveTask(id: model.connection.shown == .online) {
            let changes = LarkSync.shared.listChanges()
            async let accounts: Void = choice.load()
            await store.refresh()
            await accounts
            #if DEBUG
            // `-openMacLarkChat <name or id>`: that chat open beside the list (screenshots).
            if open == nil, let q = UserDefaults.standard.string(forKey: "openMacLarkChat") {
                open = store.conversations.first { $0.id == q || $0.name.contains(q) }
            }
            #endif
            for await change in changes {
                guard !Task.isCancelled else { return }
                if change.everything {
                    async let accounts: Void = choice.load()
                    await store.refresh()
                    await accounts
                } else {
                    // Most events are new messages: page 1 alone, of this account's chats only.
                    await store.refreshHead(change.feeds(of: choice.requestAccount))
                }
                try? await Task.sleep(for: Self.refreshGap)
            }
        }
    }

    private var list: some View {
        List {
            if query.isEmpty, choice.accounts.count > 1 {
                LarkAccountChips(choice: choice)
                    .listRowInsets(EdgeInsets(top: Theme.Space.s, leading: 0, bottom: Theme.Space.s, trailing: 0))
                    .listRowSeparator(.hidden)
                    .listRowBackground(Theme.pageBackground)
            }
            if query.isEmpty, !store.pinned.isEmpty {
                // Expands with its own Expand / Collapse button (no pull-to-expand on the Mac).
                LarkPinnedShelf(conversations: store.pinned, interaction: $pins, expandable: $canExpandPins) { open = $0 }
                    .listRowInsets(EdgeInsets(top: 0, leading: Theme.Space.l, bottom: 0, trailing: Theme.Space.l))
                    .listRowSeparator(.hidden)
                    .listRowBackground(Theme.pageBackground)
            }
            if !store.conversations.isEmpty {
                if store.failed {
                    Button { Task { await store.refresh() } } label: {
                        Label(store.needsUpgrade ? String(localized: "Update the hub to sync pinned chats · Check Again") : String(localized: "Not updated · Retry"), systemImage: Theme.Icon.retry)
                            .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                    .buttonStyle(.plain)
                    .listRowSeparator(.hidden)
                    .listRowBackground(Theme.pageBackground)
                }
                // Once per update: with a query it filters every loaded chat (`listed`).
                let rows = filtered
                let prefetch = query.isEmpty ? rows.dropLast(Self.prefetchRows).last?.id : nil
                ForEach(rows) { conversation in
                    row(conversation)
                        .onScrollVisibilityChange(threshold: 0.01) { visible in
                            if visible, conversation.id == prefetch { Task { await store.more() } }
                        }
                }
                if rows.isEmpty, !query.isEmpty, !searchingOn {
                    EmptyStateView(symbol: Theme.Icon.search, title: String(localized: "No Results for “\(query)”"),
                                   message: store.nextCursor == nil ? String(localized: "You can search chat names and recent messages. Try another word.")
                                                                    : String(localized: "Searched the latest \(store.conversations.count) chats."))
                        .listRowSeparator(.hidden)
                        .listRowBackground(Theme.pageBackground)
                }
                if store.nextCursor != nil {
                    Group {
                        if store.pageFailed {
                            Button("Retry [Lark Conversations View 8924c7]") { Task { await store.more() } }.lineLimit(1)
                        } else if !query.isEmpty, searchPages >= Self.searchPageLimit {
                            Button("Search Older Chats") { searchPages = 0; Task { await loadAtEnd() } }.lineLimit(1)
                        } else {
                            VStack(spacing: Theme.Space.xs) {
                                ProgressView().controlSize(.small)
                                if !query.isEmpty {
                                    Text("Searched \(store.conversations.count) chats, looking further back")
                                        .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                                }
                            }
                            .padding(.vertical, Theme.Space.s)
                            .onScrollVisibilityChange(threshold: 0.01) { visible in
                                endVisible = visible
                                if visible { Task { await loadAtEnd() } }
                            }
                            .onDisappear { endVisible = false }
                        }
                    }
                    .frame(maxWidth: .infinity)
                    .listRowSeparator(.hidden)
                    .listRowBackground(Theme.pageBackground)
                }
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        // One empty state for the page, over the list (docs/apple/mac-design.md「空状态」); the chat column stays empty.
        .overlay { if store.conversations.isEmpty { empty } }
        .accessibilityIdentifier("feishu-conversations")
        .modifier(MacBelowTitleRow()) // under Messages in the title row (#1243)
        .background(Theme.pageBackground.ignoresSafeArea())
        // In the window's toolbar, as the iPhone page's: the open chat's Pin Messages / Have Persona Summarize join it there (in the
        // list column's title row they ran over it), as Inbox's buttons do.
        .pageToolbar {
            MacToolbarMiddle() // the buttons by the search field, not over the chat's title (#830)
            ToolbarItem(placement: .primaryAction) {
                Button { openConnectionSettings() } label: { Label("Lark Settings", systemImage: Theme.Icon.settings) }
                    .help("Lark Settings")
                    .accessibilityIdentifier("feishu-settings")
            }
        }
        .pageSearchable(text: $query, placement: .toolbar, prompt: String(localized: "lark.searchChats", defaultValue: "Search chats", comment: "Search field over the Lark chats list"))
    }

    /// A chat: opens beside the list, the open one in the shell's selected shade (as Knowledge's rows, `KBEntryLink`).
    private func row(_ conversation: LarkConversation) -> some View {
        let selected = open?.id == conversation.id
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
        return Button { open = conversation } label: {
            LarkConversationRow(conversation: conversation, display: display)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .macListRow(selected: selected, in: shape, outset: Theme.Space.s)
        .macHoverShade(in: shape, outset: Theme.Space.s)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityIdentifier("feishu-chat-" + conversation.id)
        .listRowInsets(EdgeInsets(top: 0, leading: Theme.Space.l, bottom: 0, trailing: Theme.Space.l))
        .listRowSeparator(.hidden)
    }

    @ViewBuilder private var empty: some View {
        if store.loading {
            ProgressView()
        } else if store.status?.configured == false {
            EmptyStateView(symbol: Theme.Icon.feishuChats, title: String(localized: "Lark isn't connected yet"), message: String(localized: "Connect a Lark account on the hub and your chats will show up here."),
                           action: (String(localized: "Connection Settings"), { openConnectionSettings() }))
        } else if store.needsUpgrade {
            EmptyStateView(symbol: Theme.Icon.feishuChats, title: String(localized: "Hub update needed"), message: String(localized: "After updating, pinned chats show up and your current Lark account syncs."),
                           action: (String(localized: "Check Again [Lark Chat View c25fb8]"), { Task { await store.refresh() } }))
        } else if store.failed {
            EmptyStateView(symbol: Theme.Icon.feishuChats, title: String(localized: "Couldn't load chats"), message: String(localized: "Check the Lark connection and try again later."),
                           action: (String(localized: "Retry [Components b8784c]"), { Task { await store.refresh() } }))
        } else {
            EmptyStateView(symbol: Theme.Icon.feishuChats, title: String(localized: "No chats yet"), message: String(localized: "Your Lark direct messages and group chats show up here."))
        }
    }

    /// A search with too little to show is still reading on through older pages.
    private var searchingOn: Bool {
        store.nextCursor != nil && !store.pageFailed && searchPages < Self.searchPageLimit
    }

    /// Pages while the end of the list is on screen; a search counts its pages and stops at `searchPageLimit`.
    private func loadAtEnd() async {
        while endVisible, let cursor = store.nextCursor, !store.pageFailed, !Task.isCancelled {
            let searching = !query.isEmpty
            if searching, searchPages >= Self.searchPageLimit { return }
            await store.more()
            // Nothing new yet (another request had it): the cursor's change asks again.
            if store.nextCursor == cursor { return }
            if searching { searchPages += 1 }
        }
    }

    private func openConnectionSettings() {
        model.settingsPath = NavigationPath([SettingsPage.feishu])
        MacShellState.shared.go(.settings, model)
    }
}

/// The open chat, or a quiet line while none is (docs/apple/mac-design.md「空状态」). Its name in the column's part of the
/// title row; people and other chats it opens push in the column's own stack (‹ in the title row), Feishu documents
/// and web links read in the panel at the trailing edge, as in Inbox (`MacInboxItemDetail`).
private struct MacLarkChatColumn: View {
    let conversation: LarkConversation?
    @State private var link: LinkTarget?
    @State private var path = NavigationPath()

    var body: some View {
        NavigationStack(path: $path) {
            Group {
                if let conversation {
                    LarkChatView(conversation: conversation, messageComposer: true)
                        .modifier(MacBelowTitleRow())
                        .background(Theme.pageBackground.ignoresSafeArea())
                        // Over the part kept below the row, not inside it: clipped there, the title did not show.
                        .pageTitle(conversation.name, subtitle: subtitle(conversation))
                } else {
                    SplitPlaceholder(symbol: Theme.Icon.feishuChats, title: String(localized: "Select a Chat"), message: String(localized: "Select a chat from the list on the left to open it here."))
                        .modifier(MacBelowTitleRow())
                }
            }
            .feishuRoutes()
        }
        // Another chat starts its own stack: what the last one pushed goes with it.
        .id(conversation?.id)
        .environment(\.macSectionStack, true)
        .environment(\.feishuOpen) { route in
            if case .doc(let url, _, let manager) = route, let u = URL(string: url) { link = LinkTarget(url: u, manager: manager) } else { path.append(route) }
        }
        .macOpensLinksInPanel($link)
        .macLinkPanel($link, wide: true)
        // At the column's own edge, not past the traffic lights (the list is at the window's edge, not this).
        .environment(\.macTitleRowLeading, 0)
        // The chat titles its part of the row; the section's name over the list stays.
        .transformPreference(MacTitleRowTaken.self) { $0 = false }
    }

    private func subtitle(_ c: LarkConversation) -> String {
        [c.isTopic == true ? String(localized: "Lark Topic Group") : c.isGroup ? String(localized: "Lark Group [Mac Inbox Detail 98c8c6]") : String(localized: "Lark [Inbox Event Views 7e4419]"), c.accountName].compactMap { $0 }.joined(separator: " · ")
    }
}
