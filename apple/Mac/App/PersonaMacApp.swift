import MarkdownUI
import AppKit
import SwiftUI
import UserNotifications

/// The macOS app (docs/apple/mac.md): one AppModel, as on iOS, behind a Mac shell. A window with a sidebar (the iPhone's
/// four tabs) beside the same pages, and Settings also in its own window (⌘,).
@main
struct PersonaMacApp: App {
    @NSApplicationDelegateAdaptor(MacAppDelegate.self) private var delegate
    @State private var model = AppModel.shared
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup("Persona", id: "main") {
            MacRootView()
                .environment(model)
                .markdownInlineImageProvider(MathInlineImages()).markdownImageProvider(WebMarkdownImages()) // inline formulas (MathMarkdown)
                .markdownCodeSyntaxHighlighter(PersonaCodeHighlighter()) // fences colored by language, as on the web (#2684)
                .onOpenURL { url in model.openURL(url) }
                // A persona:// link goes to the window that is open: without this every link opened another main window.
                .handlesExternalEvents(preferring: ["*"], allowing: ["*"])
        }
        .handlesExternalEvents(matching: ["*"])
        .defaultSize(width: Theme.Size.macWindowWidth, height: Theme.Size.macWindowHeight)
        .windowToolbarStyle(.unified)
        .commands {
            MacCommands(model: model)
            MacAgentMenu(model: model)
            MacInboxMenu(model: model)
            MacViewMenu(model: model)
        }
        .onChange(of: phase) { _, new in
            // A Mac app keeps running in the background: the socket stays open (the hub reaches it that way), only
            // coming back to the front checks the connection and refreshes, as the iPhone does.
            if new == .active {
                model.live.ensureConnected()
                Task { await model.refresh() }
            }
        }

        // An Agent's terminal on its own (Agent › Open Terminal in New Window, MacTerminalWindow).
        WindowGroup(String(localized: "Terminal"), id: MacTerminalWindow.id, for: SessionKey.self) { $key in
            MacTerminalWindow(key: key).environment(model)
                .markdownInlineImageProvider(MathInlineImages()).markdownImageProvider(WebMarkdownImages()) // inline formulas (MathMarkdown)
                .markdownCodeSyntaxHighlighter(PersonaCodeHighlighter()) // fences colored by language, as on the web (#2684)
        }
        .defaultSize(width: Theme.Size.macWindowHeight, height: Theme.Size.macWindowMinHeight)

        // A conversation on its own (「在新窗口打开」, MacChatWindow).
        WindowGroup(String(localized: "Chat"), id: MacChatWindow.id, for: SessionKey.self) { $key in
            MacChatWindow(key: key).environment(model)
                .markdownInlineImageProvider(MathInlineImages()).markdownImageProvider(WebMarkdownImages()) // inline formulas (MathMarkdown)
                .markdownCodeSyntaxHighlighter(PersonaCodeHighlighter()) // fences colored by language, as on the web (#2684)
        }
        .defaultSize(width: Theme.Size.macWindowHeight, height: Theme.Size.macWindowHeight)

        // A link dragged out of the panel, an inbox item opened on its own (Mac/Link, Mac/Inbox).
        WindowGroup(String(localized: "Web Pages"), id: MacLinkWindow.id, for: MacLinkWindow.Value.self) { $value in
            MacLinkWindow(value: value).environment(model)
                .markdownInlineImageProvider(MathInlineImages()).markdownImageProvider(WebMarkdownImages()) // inline formulas (MathMarkdown)
                .markdownCodeSyntaxHighlighter(PersonaCodeHighlighter()) // fences colored by language, as on the web (#2684)
        }
        .defaultSize(width: Theme.Size.macLinkPanelIdeal * 2, height: Theme.Size.macWindowHeight)
        WindowGroup(String(localized: "Inbox"), id: MacInboxItemWindow.id, for: String.self) { $id in
            MacInboxItemWindow(itemID: id).environment(model)
                .markdownInlineImageProvider(MathInlineImages()).markdownImageProvider(WebMarkdownImages()) // inline formulas (MathMarkdown)
                .markdownCodeSyntaxHighlighter(PersonaCodeHighlighter()) // fences colored by language, as on the web (#2684)
        }
        .defaultSize(width: Theme.Size.macWindowHeight, height: Theme.Size.macWindowHeight)

        MenuBarExtra {
            MacMenuBarMenu().environment(model)
                .markdownInlineImageProvider(MathInlineImages()).markdownImageProvider(WebMarkdownImages()) // inline formulas (MathMarkdown)
                .markdownCodeSyntaxHighlighter(PersonaCodeHighlighter()) // fences colored by language, as on the web (#2684)
        } label: {
            MacMenuBarLabel().environment(model)
                .markdownInlineImageProvider(MathInlineImages()).markdownImageProvider(WebMarkdownImages()) // inline formulas (MathMarkdown)
                .markdownCodeSyntaxHighlighter(PersonaCodeHighlighter()) // fences colored by language, as on the web (#2684)
        }
        .menuBarExtraStyle(.menu)
    }
}

extension AppModel {
    /// `persona://pair?…` (a link from `personad hub pair`, or pasted): pair with that hub.
    /// A `persona://` link from outside (MacOpenURL): open a chat, pair, or hand a computer's link to Persona Link.
    func openURL(_ url: URL) {
        #if DEBUG
        if UserDefaults.standard.bool(forKey: "macWindowLog") { // how many main windows a link leaves (docs/apple/mac.md)
            DispatchQueue.main.asyncAfter(deadline: .now() + 1) {
                let mains = NSApp.windows.filter { $0.identifier?.rawValue.hasPrefix("main") == true && $0.isVisible }
                NSLog("macWindowLog: %d main window(s) after %@", mains.count, url.absoluteString)
            }
        }
        #endif
        if let link = ExtensionSystemLink(url: url) { openExtension(link); return }
        switch MacOpenURL.route(url) {
        case .session(let key): MacShellState.shared.open(resolve(path: key.path) ?? key, self)
        case .pair: openPairing(url)
        case .computerLink: openComputerLink(url)
        case .sentRecords: openSentRecords()
        case .app, .ignore: break
        }
    }

    func openPairing(_ url: URL) {
        guard let cfg = ServerConfig(pairURL: url.absoluteString) else { return }
        guard MacPairing.accepts(isPaired: isPaired, isDemo: isDemo) else {
            let host = config?.url.host() ?? String(localized: "a hub")
            toast = Toast(String(localized: "Already connected to \(host). To switch hubs, disconnect in Settings › Server first."), style: .error)
            return
        }
        Task {
            do {
                try await pair(cfg)
                let host = cfg.url.host() ?? String(localized: "Server")
                toast = Toast(String(localized: "Connected to \(host)"), style: .success)
            }
            catch { showPairing(error) }
        }
    }
}

/// The app's menus: the sections on ⌘1…, as Mac apps with a sidebar do. What acts on the main window brings it up
/// first (closed: opened again, minimized: restored), or the key press seems to do nothing from a chat window.
struct MacCommands: Commands {
    let model: AppModel

    var body: some Commands {
        // File: New Agent Chat ⌘N, New Persona Chat ⇧⌘N (docs/apple/mac.md「菜单和快捷键」). Not while unpaired: nothing to start on.
        CommandGroup(replacing: .newItem) {
            Button("New Agent Chat…") {
                MacShellState.shared.showMain() // the sheet is the main window's
                MacShellState.shared.newAgent = true
            }
            .keyboardShortcut("n", modifiers: .command)
            .disabled(!model.isPaired)
            Button("New Persona Chat") {
                MacShellState.shared.willOpen()
                model.selectedTab = .assistant
                model.assistantCurrent = nil
                MacShellState.shared.showMain()
            }
            .keyboardShortcut("n", modifiers: [.command, .shift])
            .disabled(!model.isPaired)
            // Add Computer (#4196): only a hub with machines_add takes it.
            if model.canAddComputer {
                Divider()
                Button("Add Computer…") { model.openAddComputer() }
                    .disabled(!model.isPaired)
            }
        }
        // Go: Quick Open ⌘K.
        CommandGroup(after: .textEditing) {
            Button("Find…") { MacFind.find() }
                .keyboardShortcut("f", modifiers: .command)
            Button("Find Next") { MacFind.findNext(forward: true) }
                .keyboardShortcut("g", modifiers: .command)
            Button("Find Previous") { MacFind.findNext(forward: false) }
                .keyboardShortcut("g", modifiers: [.command, .shift])
        }
        CommandMenu(String(localized: "Go [New Chat View fa9607]")) {
            Button("Quick Open…") {
                // The panel floats over the main window. It closes only where it can be seen (the main window key);
                // from a chat window, or with the main window closed or minimized, it is brought up open.
                let shell = MacShellState.shared
                let mainKey = NSApp.keyWindow?.identifier?.rawValue.hasPrefix("main") == true
                if shell.quickOpen && mainKey {
                    shell.quickOpen = false
                } else {
                    shell.showMain()
                    shell.quickOpen = true
                }
            }
            .keyboardShortcut("k", modifiers: .command)
            .disabled(!model.isPaired)
        }
        // Persona › Settings… ⌘,: the Settings section of the main window (#1222). A Settings window of its own held the same
        // pages as the section: two ways to the same place, and one of them a window on its own.
        CommandGroup(replacing: .appSettings) {
            Button("Set Up…") {
                MacShellState.shared.go(.settings, model)
                MacShellState.shared.showMain()
            }
            .keyboardShortcut(",", modifiers: .command)
            .disabled(!model.isPaired)
        }
        CommandGroup(after: .sidebar) {
            ForEach(Array(MacSection.shortcutOrder.enumerated()), id: \.element) { i, section in
                if MacSection.visibleNow.contains(section) {
                    Button(section.title) {
                        MacShellState.shared.go(section, model)
                        MacShellState.shared.showMain()
                    }
                    .keyboardShortcut(KeyEquivalent(Character("\(i + 1)")), modifiers: .command)
                }
            }
            Divider()
        }
    }
}

final class MacAppDelegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    #if DEBUG
    private var launchArgumentsApplied = false
    #endif

    func applicationWillFinishLaunching(_ notification: Notification) {
        ErrorReports.installExceptionHandler(queue: .shared) // first: a crash while launching is reported too (#5621)
        #if DEBUG
        MacSnapshot.applyAppearanceIfAsked()
        // A snapshot paired with -pairURL (a mock hub): paired before the window is built. Launched in the background
        // (`open -g`) the window kept its first frame, the welcome page, whatever the app did after (#4196).
        if UserDefaults.standard.string(forKey: "macSnapshot") != nil, UserDefaults.standard.string(forKey: "pairURL") != nil {
            MacLaunchArguments.apply()
            launchArgumentsApplied = true
        }
        #endif
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        UNUserNotificationCenter.current().delegate = self
        Task { await ExtensionNotificationActions.preserveAndRegister { NotificationActions.categories } }
        #if DEBUG
        if !launchArgumentsApplied { MacLaunchArguments.apply() }
        #endif
        AppModel.shared.start()
        MacAttentionWatcher.shared.start(AppModel.shared)
        MacMenuBarState.shared.start(AppModel.shared)
        MacShellState.shared.watchKeyWindow()
        #if DEBUG
        MacKeyScript.start()
        #endif
        #if DEBUG
        MacSnapshot.scheduleIfAsked()
        MacScrollLab.scheduleIfAsked()
        #endif
        #if DEBUG || PERF_SIGNPOST
        MacPerfHarness.scheduleIfAsked(AppModel.shared) // `-showPerfRun …`: measure smoothness from inside (#1337)
        #endif
        PushRegistrar.shared.registerIfPossible()
    }

    func application(_ application: NSApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        PushRegistrar.shared.didRegister(token: deviceToken)
    }

    /// A build signed without the push entitlement (the unsigned local ones) lands here: the Mac then only has the
    /// notifications it posts itself while it runs.
    func application(_ application: NSApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        PushRegistrar.shared.didFail(error)
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func applicationDockMenu(_ sender: NSApplication) -> NSMenu? { MacDockMenu.shared.menu(AppModel.shared) }

    /// Clicking the Dock icon with no window open brings the main window back.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { sender.windows.first { $0.identifier?.rawValue.hasPrefix("main") == true }?.makeKeyAndOrderFront(nil) }
        return true
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                            withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        let ref = PushRef(notification.request.content.userInfo)
        nonisolated(unsafe) let content = notification.request.content.copy() as! UNNotificationContent
        nonisolated(unsafe) let done = completionHandler
        Task { @Sendable in
            await ExtensionNotificationActions.register(content: content)
            DispatchQueue.main.async {
                let (key, visible) = MainActor.assumeIsolated { (ref?.key(AppModel.shared), AppModel.shared.visibleSession) }
                done(key != nil && key == visible ? [] : [.banner, .sound, .list])
            }
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                            withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let ref = PushRef(info)
        let action = response.actionIdentifier
        let text = (response as? UNTextInputNotificationResponse)?.userText
        let ixID = info["interaction_id"] as? String
        let extensionNotice = info["extension_notification"] != nil
        let pair = PairPush(info) // a pairing receipt (#4199); nil from an older hub: a plain notification
        let noteID = response.notification.request.identifier
        let event = ExtensionNotificationEvent(id: response.notification.request.identifier, info: info,
                                              category: response.notification.request.content.categoryIdentifier,
                                              actionID: action, text: text)
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                if extensionNotice {
                    if let event {
                        Task {
                            if !event.dismissed, event.valid, event.action == nil || event.action?.foreground == true {
                                MacShellState.shared.showMain()
                            }
                            await ExtensionNotificationHandler.handle(event, deps: ExtensionNotificationHandler.live(AppModel.shared))
                            done()
                        }
                    } else { done() }
                    return
                }
                if let pair {
                    if let request = PairReceiptActions.request(action: action, pair) {
                        Task {
                            await PairReceiptActions.run(request, pair, id: noteID)
                            done()
                        }
                        return
                    }
                    if action == UNNotificationDefaultActionIdentifier {
                        let model = AppModel.shared
                        model.openPair(PairReceiptActions.destination(pair, group: model.pushGroup(pair.userInfo)))
                    }
                    done()
                    return
                }
                // The group the push came from (docs/protocol/multi-hub.md「推送」).
                let key = ref?.key(AppModel.shared)
                if let key, let reply = NotificationActions.replyText(action: action, text: text) {
                    Task {
                        await NotificationActions.sendReply(key, text: reply)
                        done()
                    }
                    return
                }
                if let ixID, let resolution = NotificationActions.resolution(action: action, text: text) {
                    Task {
                        do {
                            try await HubStore.client(group: key?.group).resolve(ixID, resolution)
                        } catch {
                            await NotificationActions.reportFailure(id: ixID, title: NotificationActions.failureTitle(action: action), error: error)
                        }
                        done()
                    }
                    return
                }
                if action != UNNotificationDismissActionIdentifier, let key {
                    MacShellState.shared.showMain() // the main window may have been closed
                    MacShellState.shared.open(key, AppModel.shared, latestAnswer: ixID == nil)
                }
                done()
            }
        }
    }

}

#if DEBUG
/// The iOS app's DEBUG launch arguments (`PersonaApp.swift`), read the same way: `-pairURL`, `-tab`, `-openSession`,
/// `-openAssistant`, `-newAssistant`, `-showNewChat`, `-openChatsFresh`, `-openAgentsFresh`, `-showLark`, `-simulateTap`.
/// `-demo.on YES` needs nothing here: AppModel reads it from the defaults.
enum MacLaunchArguments {
    @MainActor static func apply() {
        let d = UserDefaults.standard
        let model = AppModel.shared
        if let s = d.string(forKey: "pairURL"), let cfg = ServerConfig(pairURL: s) {
            let more = ["pairURL2", "pairURL3"].compactMap { d.string(forKey: $0).flatMap(ServerConfig.init(pairURL:)) }
            model.adopt([cfg] + more)
            // Snapshots skip the keychain (Keychain.load), so AppModel.init saw no config and left the outbox on disk:
            // bring it back as a launch with a stored pairing does (it skips messages already there).
            model.restoreOutbox()
        }
        if d.bool(forKey: "openChatsFresh") { ChatCache.removeAll() }
        if d.bool(forKey: "openAgentsFresh") {
            for k in ["dev.machineFilter", HubFilter.key, "dev.grouping", "dev.sort", "dev.compact", "dev.folded"] { d.removeObject(forKey: k) }
        }
        // A tab, a chat or a conversation named on the command line shows that, not the Knowledge / Machines remembered
        // from last time (MacShell's onChange is not there yet to clear it).
        if ["tab", "openSession", "openAssistant", "newAssistant", "showLark"].contains(where: { d.object(forKey: $0) != nil }) {
            MacShellState.shared.section = nil
        }
        switch d.string(forKey: "tab") {
        case "dev": model.selectedTab = .chats
        case "mail": model.selectedTab = .mail
        case "settings": model.selectedTab = .settings
        case "assistant": model.selectedTab = .assistant
        default: break
        }
        // `-openMacExtensionPage <extension>/<ui>` (or the old `-openMacPluginPage`): an extension's page in the sidebar
        // (docs/extensions/extensions.md「UI 插槽」), shown once the hub lists it.
        if let key = d.string(forKey: "openMacExtensionPage") ?? d.string(forKey: "openMacPluginPage") { MacShellState.shared.pluginPage = key }
        // `-openMacSection todos`: the window opens on that sidebar section (the To-dos page, for screenshots).
        if let raw = d.string(forKey: "openMacSection"), let section = MacSection(rawValue: raw) {
            MacShellState.shared.section = section
        }
        if let key = sessionKey(d.string(forKey: "openSession")) {
            if d.bool(forKey: "openFromDoneNotice") {
                model.open(key, latestAnswer: true)
            } else {
                model.selectedTab = .chats
                model.openChat = key
            }
        }
        if d.string(forKey: "showLark") != nil { model.selectedTab = .mail }
        if let key = sessionKey(d.string(forKey: "openAssistant")) {
            model.selectedTab = .assistant
            model.assistantCurrent = key
        }
        if d.bool(forKey: "newAssistant") {
            model.selectedTab = .assistant
            model.assistantCurrent = nil
        }
        model.debugShowNewChat = d.bool(forKey: "showNewChat")
        // `-openQuickOpen <query>` (`""` for none): ⌘K open, that typed in. `-openNewAgent YES`: the New Agent sheet.
        if let q = d.string(forKey: "openQuickOpen") {
            MacShellState.shared.seed = q
            MacShellState.shared.quickOpen = true
        }
        if d.bool(forKey: "openNewAgent") { MacShellState.shared.newAgent = true }
        // `-openEngineStep <machine id>`: pairing's Choose Engine step for that machine, once the hub's machines are in (the
        // step reads the machine when it appears).
        if let id = d.string(forKey: "openEngineStep") {
            Task {
                let ref = model.hubRef(id)
                for _ in 0..<40 where model.machine(ref) == nil { try? await Task.sleep(for: .milliseconds(250)) }
                model.pairingEngineMachine = ref
            }
        }
        // `-macMenuLog YES`: the Agent menu's items and shortcuts as built for the chat on screen (the Answer submenu).
        if d.bool(forKey: "macMenuLog") {
            DispatchQueue.main.asyncAfter(deadline: .now() + 8) {
                // A closed menu is brought up to date only when it opens: tell it so, as opening it would.
                if let m = NSApp.mainMenu?.items.first(where: { $0.title == "Agent" })?.submenu {
                    m.delegate?.menuNeedsUpdate?(m)
                    m.delegate?.menuWillOpen?(m)
                    m.update()
                }
                func walk(_ m: NSMenu, _ depth: Int) {
                    for it in m.items where !it.isSeparatorItem {
                        let mods = it.keyEquivalentModifierMask
                        let k = it.keyEquivalent.isEmpty ? "" : (mods.contains(.option) ? "⌥" : "") + (mods.contains(.shift) ? "⇧" : "")
                            + (mods.contains(.command) ? "⌘" : "") + it.keyEquivalent
                        NSLog("macMenuLog: %@%@ %@%@", String(repeating: "  ", count: depth), it.title, k, it.isEnabled ? "" : " (off)")
                        if let sub = it.submenu { walk(sub, depth + 1) }
                    }
                }
                let shell = MacShellState.shared
                NSLog("macMenuLog: keyWindow %@ chats %@ visible %@ → %@", String(describing: shell.keyWindow),
                      String(describing: shell.chatsByWindow), String(describing: AppModel.shared.visibleSession),
                      String(describing: shell.focusedChat(AppModel.shared)))
                if let k = shell.focusedChat(AppModel.shared) {
                    for ix in AppModel.shared.interactions(for: k) {
                        NSLog("macMenuLog: interaction %@ questions %d choices %d", ix.kind.rawValue, ix.questions?.count ?? -1,
                              MacAgentCommands.choices(ix).count)
                    }
                }
                if let agent = NSApp.mainMenu?.items.first(where: { $0.title == "Agent" })?.submenu { walk(agent, 0) }
            }
        }
        // With `-macWindowLog YES`: how many chat / terminal windows the launch left (two openers, one window each).
        if d.bool(forKey: "macWindowLog") {
            DispatchQueue.main.asyncAfter(deadline: .now() + 4) {
                func count(_ id: String) -> Int {
                    NSApp.windows.filter { $0.isVisible && $0.identifier?.rawValue.hasPrefix(id) == true }.count
                }
                NSLog("macWindowLog: %d chat, %d terminal window(s) at launch", count(MacChatWindow.id), count(MacTerminalWindow.id))
            }
        }
        // `-openTerminalWindow <machine>/<session>`: that Agent's terminal in a window of its own.
        if let key = sessionKey(d.string(forKey: "openTerminalWindow")) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 1) { MacShellState.shared.terminalWindow = key }
        }
        // `-openChatWindow <machine>/<session>`: that conversation in a window of its own.
        if let key = sessionKey(d.string(forKey: "openChatWindow")) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 1) { MacShellState.shared.chatWindow = key }
        }
        // `-macAgentAsk rename|clear|close|delete|inspector|terminal` with `-openSession`: what that Agent menu item does, 3 s in.
        if let key = sessionKey(d.string(forKey: "openSession")), let ask = d.string(forKey: "macAgentAsk") {
            let kinds: [String: MacShellState.ChatRequest.Kind] = ["rename": .rename, "clear": .clear, "close": .close, "delete": .delete,
                                                                      "inspector": .inspector, "terminal": .terminal]
            if let kind = kinds[ask] {
                DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                    // For the main window, as the menu would with it key (`-openChatWindow` opens the same chat in a second one).
                    let main = NSApp.windows.first { $0.identifier?.rawValue.hasPrefix("main") == true }?.windowNumber
                    MacShellState.shared.chatRequest = .init(key: key, kind: kind, window: main)
                }
            }
        }
        // `-macOpenURL <persona://…>`: what handing the app that link does, 2 s in.
        if let u = d.string(forKey: "macOpenURL").flatMap(URL.init(string:)) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 2) { model.openURL(u) }
        }
        // `-macFind YES`: Edit › Find… 3 s in, and who has the keyboard after it (in the log).
        if d.bool(forKey: "macFind") {
            DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                // A locked screen's snapshot run has no key window: use the main one, as ⌘F would.
                guard let w = NSApp.keyWindow ?? NSApp.windows.first(where: { $0.isVisible && $0.canBecomeMain }) else { return }
                let found = MacFind.focusSearch(in: w)
                let r = w.firstResponder
                NSLog("macFind: found %@", found ? "yes" : "no")
                NSLog("macFind: first responder %@ (delegate %@)", String(describing: r.map { type(of: $0) }),
                      String(describing: (r as? NSText)?.delegate.map { type(of: $0) }))
            }
        }
        // `-perfCounts YES -macPerfLog YES`: what hub pushes cost (PerfCount: bodies, main-thread ms), logged every 2 s,
        // for measuring against the mock hub's /mock/churn without a UI test (docs/apple/mac.md「性能」).
        if PerfCount.enabled, d.bool(forKey: "macPerfLog") {
            Timer.scheduledTimer(withTimeInterval: 2, repeats: true) { _ in
                MainActor.assumeIsolated { NSLog("macPerf %@", PerfCount.summary) }
            }
        }
        // `-macSection knowledge|machines|…`: that sidebar section.
        if let s = d.string(forKey: "macSection").flatMap(MacSection.init(rawValue:)) { MacShellState.shared.go(s, model) }
        if let key = sessionKey(d.string(forKey: "simulateTap")) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 4) { model.open(key) }
        }
    }

    /// `<machine>/<session>`, or `group|machine/session` for another group of hubs.
    private static func sessionKey(_ s: String?) -> SessionKey? {
        guard let s else { return nil }
        return AppModel.shared.resolve(path: s)
    }
}
#endif

/// Whether a `persona://pair` link (from the browser, `personad hub pair`, Finder) may pair: not over a real pairing,
/// which a link from anywhere would otherwise replace without a word; Demo Mode makes way for a real hub.
enum MacPairing {
    static func accepts(isPaired: Bool, isDemo: Bool) -> Bool { !isPaired || isDemo }
}
