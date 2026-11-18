#if os(iOS)
import SwiftUI

/// Export Image's preview (#9468): the picture's pages, small, to scroll through; Include Questions (when it means
/// something; the selecting bar's own setting) and Title and Date; Share, which hands the pages to the system's share
/// sheet: Save Image is there, so the app never asks for Photos. The picture follows the phone's appearance. It is drawn
/// again when a switch changes; while it is drawn the pages wait under a spinner.
struct ChatExportSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.colorScheme) private var scheme
    @Environment(\.displayScale) private var displayScale
    @Environment(AppModel.self) private var model
    let request: ExportRequest
    let pick: MessagePick
    @State private var heading = true
    @State private var pages: [ExportPage] = []
    @State private var working = true
    @State private var failed: String?
    @State private var attempt = 0

    /// Include Questions means something for these messages.
    private var offersQuestions: Bool { pick.offersQuestions }

    private var options: ExportOptions {
        ExportOptions(questions: offersQuestions && pick.include, heading: heading, dark: scheme == .dark)
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(spacing: Theme.Space.s) {
                    ForEach(pages) { page in
                        if pages.count > 1 {
                            Text("Page \(page.id) of \(pages.count)").font(Theme.Font.caption).foregroundStyle(.secondary)
                        }
                        ExportPagePreview(page: page)
                            .accessibilityIdentifier("export.page.\(page.id)")
                    }
                }
                .padding(.horizontal, Theme.Space.xl)
                .padding(.vertical, Theme.Space.l)
                .opacity(working ? 0.4 : 1)
            }
            .overlay {
                if working {
                    ProgressView().accessibilityIdentifier("export.working")
                } else if let failed {
                    VStack(spacing: Theme.Space.s) {
                        Text("Couldn't make the image").font(Theme.Font.bodyStrong)
                        Text(verbatim: failed).font(Theme.Font.caption).foregroundStyle(.secondary).multilineTextAlignment(.center)
                        Button("Try Again") { attempt += 1 }.themedButton()
                    }
                    .padding(Theme.Space.l)
                }
            }
            .themedGroupedPage()
            .safeAreaInset(edge: .bottom, spacing: Theme.Space.flush) { controls }
            .navigationTitle("Export Image")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                SheetClose(id: "export.close") { dismiss() }
            }
        }
        .task(id: Run(options: options, attempt: attempt)) { await render() }
        .accessibilityIdentifier("export.sheet")
    }

    /// What starts a drawing: the choices, and Try Again.
    private struct Run: Hashable {
        var options: ExportOptions
        var attempt: Int
    }

    private var controls: some View {
        VStack(spacing: Theme.Space.m) {
            VStack(spacing: Theme.Space.flush) {
                if offersQuestions {
                    Toggle(isOn: Binding(get: { pick.include }, set: { on in
                        if !pick.setInclude(on) {
                            Haptics.error()
                            model.toast = Toast(String(localized: "Up to \(ForwardMessage.most) messages at a time"), style: .error)
                        }
                    })) {
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text("Include Questions")
                            Text("Each answer brings the question before it").font(Theme.Font.caption).foregroundStyle(.secondary)
                        }
                    }
                    .accessibilityIdentifier("export.questions")
                    .padding(.horizontal, Theme.Space.l).padding(.vertical, Theme.Space.m)
                    Divider()
                }
                Toggle("Title and Date", isOn: $heading)
                    .accessibilityIdentifier("export.heading")
                    .padding(.horizontal, Theme.Space.l).padding(.vertical, Theme.Space.m)
            }
            .surface(in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
            ShareLink(items: pages.map(\.file)) {
                Label { Text("Share").lineLimit(1) } icon: { ThemedIcon(Theme.Icon.share) }
                    .frame(maxWidth: .infinity)
            }
            .themedButton(prominent: true)
            .disabled(working || pages.isEmpty)
            .accessibilityIdentifier("export.share")
        }
        .padding(.horizontal, Theme.Space.page)
        .padding(.vertical, Theme.Space.s)
        .background(Theme.groupedPage)
    }

    private func render() async {
        working = true
        failed = nil
        do {
            let drawn = try await ChatExportRenderer.render(request, options: options, scale: ChatExportRenderer.scale(displayScale))
            pages = drawn
            working = false
        } catch is CancellationError {
            // A switch changed: the next drawing says what shows.
        } catch {
            guard !Task.isCancelled else { return }
            failed = error.shown
            working = false
        }
    }
}

/// A page's small copy, read from its file when it comes into view (a long picture's pages are never all held).
private struct ExportPagePreview: View {
    let page: ExportPage
    @Environment(\.displayScale) private var displayScale
    @State private var image: UIImage?
    @State private var width: CGFloat = 0

    var body: some View {
        let aspect = page.pixels.height > 0 ? page.pixels.width / page.pixels.height : 1
        Color.clear
            .aspectRatio(aspect, contentMode: .fit)
            .overlay {
                if let image { Image(uiImage: image).resizable().scaledToFit() }
            }
            .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Theme.cardStroke, lineWidth: Theme.Size.hairline))
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
            .task(id: width) {
                guard width > 0 else { return }
                let file = page.file, pixels = page.pixels, target = width * displayScale
                image = await Task.detached(priority: .userInitiated) {
                    ExportSlicer.thumbnail(file, pixels: pixels, width: target)
                }.value
            }
            .accessibilityElement()
            .accessibilityLabel("Page \(page.id)")
            .accessibilityAddTraits(.isImage)
    }
}
#endif
