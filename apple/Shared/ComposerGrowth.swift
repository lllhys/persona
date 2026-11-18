import Foundation

/// The iOS draft grows through six complete lines, independently of keyboard height; longer drafts scroll.
nonisolated enum ComposerGrowth {
    static var maximumLines: Int { Theme.Shell.composerPhoneMaximumLines }

    /// Measure at the unreserved width: adding the button must not change the decision that added it.
    static func offersExpansion(textHeight: CGFloat, lineHeight: CGFloat, maximumLines: Int) -> Bool {
        lineHeight > 0 && textHeight > lineHeight * (CGFloat(maximumLines) - Theme.Shell.composerLineTolerance)
    }

    /// Offers the full-page editor whenever the composer is focused for typing, has a multiline draft, or reaches the
    /// viewport's line limit.
    static func showsExpandButton(text: String, focused: Bool, textHeight: CGFloat, lineHeight: CGFloat, maximumLines: Int) -> Bool {
        if focused { return true }
        guard !text.isEmpty else { return false }
        if text.contains("\n") { return true }
        return offersExpansion(textHeight: textHeight, lineHeight: lineHeight, maximumLines: maximumLines)
    }
}
