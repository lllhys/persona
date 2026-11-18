import Testing
@testable import Persona

struct ComposerGrowthTests {
    @Test func phoneAndDesktopCapsAreIndependent() {
        #expect(ComposerGrowth.maximumLines == 6)
        #expect(Theme.Shell.composerMaximumLines == 8)
    }

    @Test func expandStartsAtTheLimitAndDisappearsBelowIt() {
        for limit in 3...8 {
            #expect(!ComposerGrowth.offersExpansion(textHeight: Double(limit - 1) * 24, lineHeight: 24, maximumLines: limit))
            #expect(ComposerGrowth.offersExpansion(textHeight: Double(limit) * 24, lineHeight: 24, maximumLines: limit))
            #expect(ComposerGrowth.offersExpansion(textHeight: 1000, lineHeight: 24, maximumLines: limit))
        }
        #expect(!ComposerGrowth.offersExpansion(textHeight: 100, lineHeight: 0, maximumLines: 3))
    }

    @Test func expandButtonShowsWhenFocusedMultilineOrAtLimit() {
        #expect(!ComposerGrowth.showsExpandButton(text: "", focused: false, textHeight: 24, lineHeight: 24, maximumLines: 3))
        #expect(ComposerGrowth.showsExpandButton(text: "", focused: true, textHeight: 24, lineHeight: 24, maximumLines: 3))
        #expect(ComposerGrowth.showsExpandButton(text: "short", focused: true, textHeight: 24, lineHeight: 24, maximumLines: 3))
        #expect(!ComposerGrowth.showsExpandButton(text: "short", focused: false, textHeight: 24, lineHeight: 24, maximumLines: 3))
        #expect(ComposerGrowth.showsExpandButton(text: "line 1\nline 2", focused: false, textHeight: 48, lineHeight: 24, maximumLines: 5))
        #expect(ComposerGrowth.showsExpandButton(text: "long single line", focused: false, textHeight: 72, lineHeight: 24, maximumLines: 3))
    }
}
