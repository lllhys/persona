import SwiftUI

/// An ⓘ beside a title that says, in a sentence or two, what the thing it names holds (#6458; docs/design/DESIGN_SYSTEM.md
/// 3.8 "Info tips"). The words stay out of the list until asked for: a tap shows them in a small popover from the ⓘ (a
/// popover on the iPhone too, not a sheet), a tap elsewhere puts them away. A finger's square to tap; VoiceOver reads it as
/// "About <name>" with the words as its hint, so a header that speaks as one element can say them itself.
struct InfoTip: View {
    var name: String
    var text: String
    @State private var shown = false

    var body: some View {
        Button { shown.toggle() } label: {
            ThemedIcon(Theme.Icon.info)
                .font(Theme.Font.label)
                .foregroundStyle(.secondary)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(String(localized: "About \(name)"))
        .accessibilityHint(text)
        .accessibilityIdentifier("infoTip")
        .popover(isPresented: $shown) {
            Text(text)
                .accessibilityIdentifier("infoTip.text")
                .font(Theme.Font.secondary)
                .foregroundStyle(.primary)
                .fixedSize(horizontal: false, vertical: true)
                .scaledFrame(maxWidth: Theme.Size.box260, alignment: .leading)
                .padding(Theme.Space.m)
                .presentationCompactAdaptation(.popover)
        }
    }
}
