import MarkdownUI
import PersonaKit
import Photos
import SwiftUI

/// A message to share as a picture (Share in the long-press bar).
struct ShareMessage: Identifiable {
    var text: String
    var isUser: Bool
    var source: String
    var date: Date?
    var agent = "Claude"
    var id: String { text }
}

/// The message laid out as a card: who said it, where, the words, and a quiet Persona mark — drawn into an image.
/// `width`: the card's, chosen for its content (`ShareCardWidth`, #2216).
struct MessageShareCard: View {
    var message: ShareMessage
    var dark: Bool
    var width = ShareCardWidth.base

    /// Very long answers are cut: a picture of ten screens helps nobody.
    static let maxChars = 3000

    var text: String {
        let t = message.text.trimmingCharacters(in: .whitespacesAndNewlines)
        return t.count > Self.maxChars ? String(t.prefix(Self.maxChars)) + "\n\n…" : t
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.l) {
            HStack(spacing: 10) {
                avatar
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(message.isUser ? String(localized: "Me") : message.agent).font(.system(size: 15, weight: .semibold))
                    if !message.source.isEmpty {
                        Text(message.source).font(.system(size: 12)).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                if let d = message.date, d.isMeaningful {
                    Text(d.formatted(.dateTime.month().day().hour().minute())).font(.system(size: 12)).foregroundStyle(.secondary)
                }
            }
            Markdown(Links.markdown(text))
                .markdownTheme(.card)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 6) {
                ThemedIcon(Theme.Icon.ai).font(.system(size: 11, weight: .semibold))
                Text("Persona").font(.system(size: 12, weight: .semibold))
                Spacer()
            }
            .foregroundStyle(.tertiary)
            .padding(.top, 2)
        }
        .padding(22)
        .background(dark ? Color(white: 0.11) : .white, in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))
        .shadow(color: .black.opacity(dark ? 0.4 : 0.08), radius: 16, y: 6)
        .padding(24)
        // The gradient fills the whole image: the renderer can size it a little taller than the card is laid out
        // (a table measures itself in two passes), which left a transparent band above and below it.
        .frame(width: width)
        .frame(maxHeight: .infinity)
        .background(LinearGradient(colors: dark ? [Color(hex: 0x1C2440), Color(hex: 0x2B1D3F)] : [Color(hex: 0xDCE8FF), Color(hex: 0xF1E4FF)],
                                   startPoint: .topLeading, endPoint: .bottomTrailing))
        .environment(\.colorScheme, dark ? .dark : .light)
    }

    /// Who wrote it: the user, or the agent's own mark (Claude's, OpenAI's for Codex). TRAEX has no public mark and
    /// keeps a terminal on green.
    @ViewBuilder private var avatar: some View {
        if message.isUser {
            ThemedIcon(Theme.Icon.user).font(.system(size: 15, weight: .semibold)).foregroundStyle(.white)
                .frame(width: 32, height: 32).background(Color.gray, in: .circle)
        } else if message.agent == "TRAEX" {
            ThemedIcon(Theme.Icon.terminalFilled).font(.system(size: 15, weight: .semibold)).foregroundStyle(.white)
                .frame(width: 32, height: 32)
                .background(LinearGradient(colors: [Color(hex: 0x2FD38F), Color(hex: 0x12935E)], startPoint: .top, endPoint: .bottom), in: .circle)
        } else {
            Image(message.agent == "Codex" ? Theme.Brand.openAI : Theme.Brand.claude)
                .resizable().scaledToFit().foregroundStyle(.primary)
                .padding(7)
                .frame(width: 32, height: 32)
                .background(Color(.secondarySystemBackground), in: .circle)
        }
    }

    /// Drawn as wide as its tables and code need, or a little wider when it would be a thin strip (`ShareCardWidth`).
    @MainActor static func render(_ message: ShareMessage, dark: Bool) -> UIImage? {
        let card = MessageShareCard(message: message, dark: dark)
        let width = ShareCardWidth.width(natural: ShareCardWidth.natural(card.text), height: size(card, width: ShareCardWidth.base).height)
        let r = ImageRenderer(content: MessageShareCard(message: message, dark: dark, width: width))
        r.proposedSize = ProposedViewSize(width: width, height: nil) // measured at the width it is drawn at
        r.scale = 3
        return r.uiImage
    }

    /// The card's size at `width`, in points.
    @MainActor private static func size(_ card: MessageShareCard, width: CGFloat) -> CGSize {
        let r = ImageRenderer(content: card)
        r.proposedSize = ProposedViewSize(width: width, height: nil)
        r.scale = 1
        return r.uiImage?.size ?? .zero
    }
}

extension MarkdownUI.Theme {
    /// The chat's theme with code blocks that wrap instead of scrolling (an image can't scroll; the renderer
    /// draws no scroll views at all).
    @MainActor static let card = MarkdownUI.Theme.persona
        .text { FontSize(15) }
        .paragraph { config in
            config.label
                .fixedSize(horizontal: false, vertical: true)
                .relativeLineSpacing(.em(0.26))
                .markdownMargin(top: 0, bottom: 10)
        }
        .codeBlock { config in
            config.label
                .relativeLineSpacing(.em(0.2))
                .markdownTextStyle { FontFamilyVariant(.monospaced); FontSize(.em(0.8)) }
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.codeBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                .markdownMargin(top: 4, bottom: 12)
        }
        .table { config in
            config.label
                .markdownTableBorderStyle(.init(color: Color.primary.opacity(0.12)))
                .markdownTableBackgroundStyle(.alternatingRows(Color.clear, Color.primary.opacity(0.03)))
                .markdownMargin(top: 4, bottom: 12)
        }
}

/// How wide a message's card is drawn (#2216). Plain text keeps a phone's width; tables, code blocks and their long lines
/// widen it to their own width, up to `cap`, past which a table wraps its cells instead. A card that would still be a
/// thin strip, taller than `tallest` times its width, widens too, also up to `cap`: about 28 Chinese characters a line
/// at 15 pt, still a comfortable line to read, and filling a phone's screen the words are about three quarters as big.
nonisolated enum ShareCardWidth {
    static let base: CGFloat = 390
    static let cap: CGFloat = 520
    static let tallest: CGFloat = 2.6
    /// Around the words: the 24 pt margin and the 22 pt padding on each side.
    static let chrome: CGFloat = 2 * (24 + 22)

    /// natural: the widest table or code block at its own width (0 for none); height: the card's height at `base`.
    static func width(natural: CGFloat, height: CGFloat) -> CGFloat {
        var width = base
        if natural > 0 { width = max(width, natural + chrome) }
        if height > tallest * base {
            // The words keep about the same area as they widen: height × width of the text ≈ the same.
            let area = height * (base - chrome)
            let fit = (tallest * chrome + (tallest * tallest * chrome * chrome + 4 * tallest * area).squareRoot()) / (2 * tallest)
            width = max(width, fit)
        }
        return min(width.rounded(), cap)
    }

    /// The widest of the text's tables and code blocks drawn at their own width, as the card draws them.
    @MainActor static func natural(_ text: String) -> CGFloat {
        ShareCardBlocks.wide(text).map { block in
            let r = ImageRenderer(content: Markdown(block).markdownTheme(.card).fixedSize())
            r.proposedSize = .unspecified
            r.scale = 1
            return r.uiImage?.size.width ?? 0
        }.max() ?? 0
    }
}

/// The parts of a message that want width: its fenced code blocks and its tables (a row of pipes over a delimiter row),
/// each as the Markdown that draws it alone.
nonisolated enum ShareCardBlocks {
    static func wide(_ text: String) -> [String] {
        let lines = text.components(separatedBy: "\n")
        var out: [String] = [], i = 0
        while i < lines.count {
            let trimmed = lines[i].trimmingCharacters(in: .whitespaces)
            if let fence = MarkdownFence.opening(trimmed) {
                var j = i + 1
                while j < lines.count, !fence.isClosed(by: lines[j].trimmingCharacters(in: .whitespaces)) { j += 1 }
                out.append(lines[i...min(j, lines.count - 1)].joined(separator: "\n"))
                i = j + 1
            } else if trimmed.contains("|"), i + 1 < lines.count, isDelimiter(lines[i + 1]) {
                var j = i + 2
                while j < lines.count, lines[j].contains("|"), !lines[j].trimmingCharacters(in: .whitespaces).isEmpty { j += 1 }
                out.append(lines[i..<j].joined(separator: "\n"))
                i = j
            } else {
                i += 1
            }
        }
        return out
    }

    /// `| --- | :-: |`, `---|---`: a table's second line (with a pipe: a lone `---` is a rule).
    static func isDelimiter(_ line: String) -> Bool {
        let cells = line.trimmingCharacters(in: .whitespaces).split(separator: "|", omittingEmptySubsequences: true)
            .map { $0.trimmingCharacters(in: .whitespaces) }
        guard line.contains("|"), !cells.isEmpty else { return false } // a lone "---" is a rule
        return cells.allSatisfy { c in
            let core = c.drop { $0 == ":" }.reversed().drop { $0 == ":" }
            return !core.isEmpty && core.allSatisfy { $0 == "-" }
        }
    }
}

/// Preview of the card, light or dark, with the image viewer's bars (#4714, `ViewerTopBar` / `ViewerBottomBar`): a round
/// close button and Light / Dark in a glass capsule on top; Share (the image) · Save and Copy in a capsule · Share Text (the
/// plain text) at the bottom. The card scrolls under the bars.
struct MessageShareSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.colorScheme) private var scheme
    var message: ShareMessage
    @State private var dark: Bool?
    @State private var image: UIImage?
    @State private var saving = false

    private var isDark: Bool { dark ?? (scheme == .dark) }

    var body: some View {
        ScrollView {
            Group {
                if let image {
                    Image(uiImage: image)
                        .resizable()
                        .scaledToFit()
                        .clipShape(.rect(cornerRadius: Theme.Radius.card, style: .continuous))
                        .shadow(color: .black.opacity(0.12), radius: 12, y: 4)
                        .accessibilityLabel("Message card")
                } else {
                    ProgressView().frame(maxWidth: .infinity, minHeight: 240)
                }
            }
            .padding(.horizontal, Theme.Space.xl)
            .padding(.vertical, Theme.Space.l)
        }
        .scrollIndicators(.hidden)
        .background(Color(.systemGroupedBackground))
        .safeAreaInset(edge: .top, spacing: 0) { topBar.padding(.vertical, Theme.Space.s) }
        .safeAreaInset(edge: .bottom, spacing: 0) { bottomBar.padding(.vertical, Theme.Space.s) }
        .task(id: isDark) { image = MessageShareCard.render(message, dark: isDark) }
        .presentationDetents([.large])
    }

    private var topBar: some View {
        ViewerTopBar {
            Button { dismiss() } label: { ViewerIcon(symbol: Theme.Icon.close) }
                .viewerRound()
                .accessibilityLabel("Done [Extension System Presentation c0b3fb]")
                .accessibilityIdentifier("share-card-close")
                .keyboardShortcut(.cancelAction)
        } center: {
            Picker("Style", selection: Binding(get: { isDark }, set: { dark = $0 })) {
                Text("Light").tag(false)
                Text("Dark").tag(true)
            }
            .pickerStyle(.segmented)
            .fixedSize()
            .accessibilityIdentifier("share-card-style")
            .padding(Theme.Space.xs)
            .surface(in: Capsule()) // not interactive glass: that would take the segments' taps
        } trailing: {
            ViewerBarSpacer()
        }
    }

    private var bottomBar: some View {
        ViewerBottomBar {
            if let image {
                ShareLink(item: Image(uiImage: image), preview: SharePreview(String(localized: "Message card"), image: Image(uiImage: image))) {
                    ViewerIcon(symbol: Theme.Icon.share)
                }
                .accessibilityLabel("Share")
            } else {
                ViewerIcon(symbol: Theme.Icon.share, enabled: false)
                    .accessibilityLabel("Share")
                    .accessibilityAddTraits(.isButton)
            }
        } actions: {
            Button { Task { await save() } } label: { ViewerIcon(symbol: Theme.Icon.save) }
                .disabled(image == nil || saving)
                .accessibilityLabel(saving ? String(localized: "Saving [Image Viewer a1a172]") : String(localized: "Save"))
            Button { copyImage() } label: { ViewerIcon(symbol: Theme.Icon.copy) }
                .disabled(image == nil)
                .accessibilityLabel("Copy [Readiness Views d37380]")
        } trailing: {
            ShareLink(item: message.text) { ViewerIcon(symbol: Theme.Icon.text) }
                .accessibilityLabel("Share Text")
        }
    }

    private func copyImage() {
        guard let image else { return }
        UIPasteboard.general.image = image
        Haptics.success()
        AppModel.shared.toast = Toast(String(localized: "Image copied"), style: .success)
    }

    private func save() async {
        guard let image else { return }
        saving = true
        defer { saving = false }
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            Haptics.error()
            AppModel.shared.toast = Toast(String(localized: "No access to Photos. In Settings › Persona › Photos, allow “Add Photos Only”."), style: .error)
            return
        }
        do {
            // `@Sendable`: Photos runs the block on its own queue, where main-actor code is a crash (BackgroundCallbackTests).
            try await PHPhotoLibrary.shared().performChanges { @Sendable in PHAssetChangeRequest.creationRequestForAsset(from: image) }
            Haptics.success()
            AppModel.shared.toast = Toast(String(localized: "Saved to Photos"), style: .success)
        } catch {
            Haptics.error()
            if !error.isCancellation { AppModel.shared.toast = Toast(String(localized: "Couldn't save: \(error.localizedDescription) [Image Viewer fcc6eb]"), style: .error) }
        }
    }
}
