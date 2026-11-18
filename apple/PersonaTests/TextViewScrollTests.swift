import Testing
import UIKit
@testable import Persona

/// A field that grew taller must not stay scrolled down a line, showing only its last line (#909).
@MainActor struct TextViewScrollTests {
    @Test func layoutRefreshesLeaveAManuallyScrolledDraftAtTheFirstLine() async {
        let view = draftView()
        view.selectedRange = NSRange(location: (view.text as NSString).length, length: 0)
        view.setContentOffset(.zero, animated: false)
        for _ in 0..<20 {
            TextViewScroll.scheduleSettling(view)
            await nextMainQueueTurn()
            #expect(view.contentOffset.y == 0)
        }
        #expect(view.caretReveals == 0)
    }

    @Test func typingCanRevealTheCaretAfterLayout() async {
        let view = draftView()
        view.selectedRange = NSRange(location: (view.text as NSString).length, length: 0)
        view.setContentOffset(.zero, animated: false)
        TextViewScroll.scheduleSettling(view, revealCaret: true)
        await nextMainQueueTurn()
        #expect(view.caretReveals == 1)
        #expect(view.contentOffset.y > 0)
    }

    @Test func aManualScrollCancelsAlreadyQueuedCaretFollowing() async {
        let view = draftView()
        view.selectedRange = NSRange(location: (view.text as NSString).length, length: 0)
        view.setContentOffset(CGPoint(x: 0, y: 40), animated: false)
        TextViewScroll.scheduleSettling(view, revealCaret: true)
        view.setContentOffset(.zero, animated: false)
        await nextMainQueueTurn()
        #expect(view.contentOffset.y == 0)
        #expect(view.caretReveals == 0)
    }

    @Test func aLaterSelectionCancelsStaleCaretWork() async {
        let view = draftView()
        view.selectedRange = NSRange(location: (view.text as NSString).length, length: 0)
        TextViewScroll.scheduleSettling(view, revealCaret: true)
        view.selectedRange = NSRange(location: 0, length: 0)
        await nextMainQueueTurn()
        #expect(view.caretReveals == 0)
    }

    private func draftView() -> DraftView {
        let view = DraftView(frame: CGRect(x: 0, y: 0, width: 200, height: 80))
        view.font = .systemFont(ofSize: 17)
        view.text = (1...20).map { "Draft line \($0)" }.joined(separator: "\n")
        view.layoutIfNeeded()
        return view
    }

    private func nextMainQueueTurn() async {
        await withCheckedContinuation { continuation in
            DispatchQueue.main.async { continuation.resume() }
        }
    }

    private final class DraftView: UITextView {
        var caretReveals = 0
        override func scrollRangeToVisible(_ range: NSRange) {
            caretReveals += 1
            super.scrollRangeToVisible(range)
        }
    }

    @Test func anOffsetWithinTheContentIsKept() {
        // Eight lines shown of ten, scrolled to the end: that is where the caret is.
        #expect(TextViewScroll.settled(offset: 48, content: 240, height: 192, insetTop: 0, insetBottom: 0) == 48)
        #expect(TextViewScroll.settled(offset: 0, content: 240, height: 192, insetTop: 0, insetBottom: 0) == 0)
    }

    @Test func anOffsetPastTheEndComesBack() {
        // Two lines in a field two lines tall, scrolled down one: back to the top.
        #expect(TextViewScroll.settled(offset: 24, content: 49, height: 49, insetTop: 0, insetBottom: 0) == 0)
        // Content shorter than the view: the top, insets included.
        #expect(TextViewScroll.settled(offset: 24, content: 30, height: 49, insetTop: 4, insetBottom: 4) == -4)
        // Past the end of content that does scroll: the end.
        #expect(TextViewScroll.settled(offset: 100, content: 240, height: 192, insetTop: 0, insetBottom: 0) == 48)
    }

    @Test func aTextViewLeftScrolledDownShowsItsFirstLineAgain() {
        // Three lines in a view tall enough for all of them, left scrolled down a line (the field on the phone in #909).
        let view = UITextView(frame: CGRect(x: 0, y: 0, width: 200, height: 30))
        view.font = .systemFont(ofSize: 17)
        view.text = "第一行\n第二行\n第三行"
        view.frame.size.height = view.sizeThatFits(CGSize(width: 200, height: CGFloat.greatestFiniteMagnitude)).height
        view.layoutIfNeeded()
        view.contentOffset.y = 20
        TextViewScroll.settle(view)
        #expect(view.contentOffset.y == -view.adjustedContentInset.top)
    }

    @Test func aTextViewScrolledWithinItsContentIsLeftAlone() {
        // Eight lines shown of many, scrolled to the caret: nothing to fix.
        let view = UITextView(frame: CGRect(x: 0, y: 0, width: 200, height: 60))
        view.font = .systemFont(ofSize: 17)
        view.text = (1...10).map { "第\($0)行" }.joined(separator: "\n")
        view.layoutIfNeeded()
        view.contentOffset.y = 40
        TextViewScroll.settle(view)
        #expect(view.contentOffset.y == 40)
    }
}
