import AppKit
import SwiftUI
import WebKit

// Links open inside the app (CLAUDE.md). On the iPhone that is a card rising from the bottom; on the Mac it is a panel
// sliding in at the detail column's trailing edge, beside what it came from (docs/apple/mac.md「和 iOS 的对应」): the page
// stays usable, the panel's width is dragged and remembered, ⌘W closes it, 「在浏览器打开」 hands it to the default
// browser, and 「在单独窗口打开」 moves it into a window of its own. Feishu documents and company-feed posts open in the
// native views (FeishuRouteView) while the hub has them (`LarkFeatures`), the rest in a WKWebView from the shared pool
// (`WebViewPool`, preloaded, reused).

extension View {
    /// Shows `link` in a panel at the trailing edge. `key` is the conversation 「AI 修改」 / 「作为上下文」 go to; without
    /// one (Inbox, Knowledge) they go to Persona' main conversation. `wide`: the panel starts as wide as the page beside it
    /// allows (Inbox, #2113), its dragged width remembered apart from the others'.
    func macLinkPanel(_ link: Binding<LinkTarget?>, key: SessionKey? = nil, wide: Bool = false) -> some View {
        modifier(MacLinkPanelHost(link: link, key: key, wide: wide))
    }

    /// Links followed inside this view (`openURL`) open in the panel; the Feishu ones in the native reader there, read
    /// through `manager` (`LinkTarget.manager`).
    func macOpensLinksInPanel(_ link: Binding<LinkTarget?>, manager: String? = nil) -> some View {
        modifier(MacPanelLinks(link: link, manager: manager))
    }
}

/// Web links open in the panel; with ⌥ held, in a window of their own (`MacLinkWindow`, #2113), as a browser's
/// ⌘-click opens a tab.
private struct MacPanelLinks: ViewModifier {
    @Binding var link: LinkTarget?
    var manager: String?
    @Environment(\.openWindow) private var openWindow

    func body(content: Content) -> some View {
        content
            .environment(\.openURL, OpenURLAction { url in
                guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https" else { return LinkPolicy.system(url) }
                open(url)
                return .handled
            })
            .environment(\.openLink) { open($0) }
    }

    private func open(_ url: URL) {
        let target = LinkTarget(url: url, manager: manager)
        if NSEvent.modifierFlags.contains(.option) { openWindow(id: MacLinkWindow.id, value: MacLinkWindow.Value(target)) } else { link = target }
    }
}

/// The page with the panel beside it. The panel is a sibling in an HStack (not an overlay): the page narrows instead
/// of being covered, so what the link came from stays readable.
private struct MacLinkPanelHost: ViewModifier {
    @Binding var link: LinkTarget?
    var key: SessionKey?
    var wide = false
    @AppStorage("mac.linkPanel.width") private var narrowWidth: Double = Theme.Size.macLinkPanelIdeal
    /// Past any window's width until dragged: `clamp` makes it the most the page beside allows.
    @AppStorage("mac.linkPanel.wideWidth") private var wideWidth: Double = .greatestFiniteMagnitude
    private var width: Double {
        get { wide ? wideWidth : narrowWidth }
        nonmutating set { if wide { wideWidth = newValue } else { narrowWidth = newValue } }
    }
    private var pageMin: CGFloat { wide ? Theme.Size.macLinkPanelWidePageMin : Theme.Size.macLinkPanelPageMin }
    /// The width while a drag is going (written to `width` when it ends, so the store is not hit every frame).
    @State private var dragging: Double?

    func body(content: Content) -> some View {
        GeometryReader { geo in
            // Too narrow for both side by side: the panel covers the column, as a page pushed on it would.
            let beside = geo.size.width >= Theme.Size.macLinkPanelMin + pageMin
            HStack(spacing: 0) {
                content
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                if let link, beside {
                    let w = clamp(dragging ?? width, in: geo.size.width)
                    MacPanelResizeHandle { delta in
                        dragging = clamp((dragging ?? Double(w)) - delta, in: geo.size.width)
                    } ended: {
                        if let dragging { width = dragging }
                        dragging = nil
                    }
                    panel(link)
                        .frame(width: w)
                }
            }
            .overlay {
                if let link, !beside { panel(link) }
            }
        }
        .animation(Theme.Motion.layout, value: link?.id)
    }

    private func panel(_ link: LinkTarget) -> some View {
        MacLinkPanel(target: link, key: key) { self.link = nil }
            .id(link.id)
            .transition(.move(edge: .trailing).combined(with: .opacity))
    }

    /// At least the panel's smallest, and never so wide the page goes under its smallest.
    private func clamp(_ w: Double, in total: CGFloat) -> CGFloat {
        let most = max(Theme.Size.macLinkPanelMin, total - pageMin)
        return min(max(w, Theme.Size.macLinkPanelMin), most)
    }
}

/// A divider that can be dragged sideways, with the resize cursor over it (as the split views' dividers).
struct MacPanelResizeHandle: View {
    var changed: (CGFloat) -> Void
    var ended: () -> Void
    @State private var last: CGFloat = 0

    var body: some View {
        Divider()
            .overlay {
                Color.clear
                    .frame(width: Theme.Size.macResizeHitWidth)
                    .contentShape(.rect)
                    .onHover { inside in if inside { NSCursor.resizeLeftRight.push() } else { NSCursor.pop() } }
                    .gesture(
                        DragGesture(minimumDistance: 1, coordinateSpace: .global)
                            .onChanged { v in
                                changed(v.translation.width - last)
                                last = v.translation.width
                            }
                            .onEnded { _ in last = 0; ended() }
                    )
            }
            .accessibilityHidden(true)
    }
}

/// The panel: a header (title, host, back / forward / reload, actions, close) over the page.
struct MacLinkPanel: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openWindow) private var openWindow
    let target: LinkTarget
    var key: SessionKey?
    /// Closes the panel; nil in a window of its own (the window's close button does it).
    var close: (() -> Void)?
    @State private var title = ""
    @State private var editing: ContextRef?
    @State private var instruction = ""

    /// Feishu links with a native view (documents, company-feed posts), while the hub has them (`LarkFeatures`).
    private var feishuRoute: FeishuRoute? { FeishuLinks.route(target.url, lark: model.larkFeatures, manager: target.manager) }
    private var isFeishuDoc: Bool { if case .doc = feishuRoute { true } else { false } }
    /// Where 「AI 修改」 and 「作为上下文」 go: the conversation the link came from, else Persona' main one.
    private var destination: SessionKey? { key ?? model.assistantMain ?? model.assistantCurrent }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            if let feishuRoute {
                NavigationStack {
                    FeishuRouteView(route: feishuRoute)
                        .feishuRoutes()
                        .environment(\.docPick) { text, kind in pick(text, kind: kind) }
                }
            } else {
                PooledWebView(url: target.url, title: $title) { selected, kind in pick(selected, kind: kind) }
            }
        }
        .background(Theme.pageBackground)
        .alert("Have an Agent Edit This", isPresented: Binding(get: { editing != nil }, set: { if !$0 { editing = nil } })) {
            TextField(String(localized: "How should it change?"), text: $instruction)
            Button("Cancel", role: .cancel) { instruction = "" }
            Button("Send") { sendEdit() }
        } message: {
            Text(editing.map { String($0.text.prefix(120)) } ?? "")
        }
        .accessibilityIdentifier("mac.linkPanel")
    }

    private var shownTitle: String {
        title.isEmpty ? (isFeishuDoc ? String(localized: "Lark Doc") : feishuRoute != nil ? String(localized: "Moments") : target.url.host ?? String(localized: "Web Pages")) : title
    }

    private var header: some View {
        HStack(spacing: Theme.Space.s) {
            if feishuRoute == nil {
                ControlGroup {
                    Button { web?.goBack() } label: { Image(systemName: Theme.Icon.back) }
                        .help("Back [Mac Link Panel 2d1d8c]")
                    Button { web?.goForward() } label: { Image(systemName: Theme.Icon.forward) }
                        .help("Forward")
                }
                .controlGroupStyle(.navigation)
                .fixedSize()
            }
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(shownTitle).font(Theme.Font.secondaryStrong).foregroundStyle(.primary).lineLimit(1)
                Text(target.url.host ?? target.url.absoluteString).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            .help(target.url.absoluteString)
            Spacer(minLength: Theme.Space.sm)
            if isFeishuDoc {
                Button { pick("", kind: "context") } label: { Image(systemName: Theme.Icon.addContext) }
                    .help("Add this document as context")
                    .accessibilityLabel("Add this document as context")
            } else if feishuRoute == nil {
                Button { web?.reload() } label: { Image(systemName: Theme.Icon.retry) }
                    .help("Reload [Mac Link Panel 35588d]")
            }
            Menu {
                Button { NSWorkspace.shared.open(target.url) } label: { Label("Open in Browser", systemImage: Theme.Icon.safari) }
                if close != nil {
                    Button {
                        openWindow(id: MacLinkWindow.id, value: MacLinkWindow.Value(target))
                        close?()
                    } label: { Label("Open in Separate Window", systemImage: Theme.Icon.newWindow) }
                }
                Divider()
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(target.url.absoluteString, forType: .string)
                    model.toast = Toast(String(localized: "Link copied"), style: .success)
                } label: { Label("Copy Link", systemImage: Theme.Icon.link) }
                ShareLink(item: target.url) { Label("Share", systemImage: Theme.Icon.share) }
            } label: { Image(systemName: Theme.Icon.moreMenu) }
            .menuIndicator(.hidden)
            .fixedSize()
            .help("More")
            if let close {
                Button(action: close) { Image(systemName: Theme.Icon.clear).symbolRenderingMode(.hierarchical) }
                    .keyboardShortcut("w", modifiers: .command)
                    .help("Close (⌘W)")
                    .accessibilityLabel("Close")
            }
        }
        .buttonStyle(.borderless)
        .foregroundStyle(.secondary)
        .labelStyle(.iconOnly)
        .font(Theme.Font.heading)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
    }

    /// The pooled view for this link (back / forward / reload act on it).
    private var web: WKWebView? { WebViewPool.shared.existing(for: target.url) }

    private func pick(_ text: String, kind: String) {
        let ref = ContextRef(kind: kind, title: title.isEmpty ? (target.url.host ?? "") : title, url: target.url.absoluteString,
                             text: text.isEmpty ? "（整篇）" : text) // i18n-ignore: sentinel in the context XML, compared in QuoteViews
        if kind == "edit" {
            instruction = ""
            editing = ref
        } else if let destination {
            model.composerContexts[destination, default: []].append(ref)
            model.toast = Toast(key == nil ? String(localized: "Added as context. It will be sent with your next message to Persona.") : String(localized: "Added as context. It'll be sent with your next message."), style: .success)
        } else {
            Task { await model.askPersona(ref.xml + "\n\n先记下这段，我接着说。") } // i18n-ignore: sent to the assistant
        }
    }

    private func sendEdit() {
        guard let ref = editing else { return }
        let ask = instruction.trimmingCharacters(in: .whitespacesAndNewlines)
        let text = ref.xml + "\n\n" + (ask.isEmpty ? "按上面选中的内容修改。" : "按要求修改上面选中的内容：" + ask) // i18n-ignore: instruction sent to the agent
        if let key { model.sendNow(key, text: text) } else { Task { await model.askPersona(text) } }
        instruction = ""
        editing = nil
    }
}

/// A link in a window of its own (dragged out of the panel with 「在单独窗口打开」): the same panel, closed with the
/// window. Its 「AI 修改」 / 「作为上下文」 go to Persona.
struct MacLinkWindow: View {
    static let id = "link"

    /// What the window opens: the link and the manager its native view reads through (`LinkTarget.manager`).
    nonisolated struct Value: Codable, Hashable, Sendable {
        var url: URL
        var manager: String?

        @MainActor init(_ target: LinkTarget) {
            url = target.url
            manager = target.manager
        }

        init(from decoder: any Decoder) throws {
            // A window restored from a version before managers holds the bare URL.
            if let url = try? decoder.singleValueContainer().decode(URL.self) {
                self.url = url
                manager = nil
                return
            }
            let c = try decoder.container(keyedBy: CodingKeys.self)
            url = try c.decode(URL.self, forKey: .url)
            manager = try c.decodeIfPresent(String.self, forKey: .manager)
        }
    }

    @Environment(AppModel.self) private var model
    var value: Value?

    var body: some View {
        Group {
            if let value {
                MacLinkPanel(target: LinkTarget(url: value.url, manager: value.manager), key: nil, close: nil)
            } else {
                EmptyStateView(symbol: Theme.Icon.link, title: String(localized: "No Link"), message: String(localized: "Open a link from a chat or the Inbox."))
            }
        }
        .macLook(model)
        .frame(minWidth: Theme.Size.macLinkPanelMin, minHeight: Theme.Size.macSheetMinHeight)
    }
}
