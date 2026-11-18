import CoreGraphics
import Testing
@testable import Persona

/// A held mic's rules (#9619): only a slide left decides; there is no lock (a tap keeps dictation on).
struct DictationHoldTests {
    @Test func aTouchCountsAsAHoldOnlyAfterATapsLength() {
        // A slow tap (0.2–0.3 s) is still a tap: Telegram's 0.19 s took it for a hold that heard nothing.
        #expect(Theme.Hold.delay == .milliseconds(300))
    }

    @Test func whileHeldOnlyAFarSlideLeftDecides() {
        #expect(DictationHold.whileHeld(CGSize(width: -149, height: 0)) == nil)
        #expect(DictationHold.whileHeld(CGSize(width: -151, height: 0)) == .cancel)
        // Up no longer locks: the finger going up is where it started.
        #expect(DictationHold.whileHeld(CGSize(width: 0, height: -400)) == nil)
    }

    @Test func rightUpAndDownAreWhereTheFingerStarted() {
        #expect(DictationHold.whileHeld(CGSize(width: 400, height: 400)) == nil)
        #expect(DictationHold.onRelease(CGSize(width: 300, height: 300), velocity: CGSize(width: 2000, height: 2000)) == .keep)
        #expect(DictationHold.onRelease(CGSize(width: 0, height: -300), velocity: CGSize(width: 0, height: -2000)) == .keep)
        #expect(DictationHold.labelShift(CGSize(width: 50, height: 0)) == 0)
    }

    @Test func lettingGoAShorterSlideOrAFlickLeftCancels() {
        #expect(DictationHold.onRelease(CGSize(width: -101, height: 0), velocity: .zero) == .cancel)
        #expect(DictationHold.onRelease(CGSize(width: -99, height: 0), velocity: .zero) == .keep)
        #expect(DictationHold.onRelease(CGSize(width: -20, height: 0), velocity: CGSize(width: -401, height: 0)) == .cancel)
        #expect(DictationHold.onRelease(CGSize(width: -20, height: 0), velocity: CGSize(width: -399, height: 0)) == .keep)
        // A slide left that also went up still cancels: only left counts.
        #expect(DictationHold.onRelease(CGSize(width: -101, height: -120), velocity: .zero) == .cancel)
    }

    @Test func theLabelFollowsAtHalfThePaceAfterADeadZone() {
        #expect(DictationHold.labelShift(CGSize(width: -8, height: 0)) == 0)
        #expect(DictationHold.labelShift(CGSize(width: -48, height: 0)) == 20)
    }

    @Test func theLabelIsGoneByTheDrop() {
        #expect(DictationHold.labelOpacity(.zero) == 1)
        #expect(DictationHold.labelOpacity(CGSize(width: -120, height: 0)) == 1)
        #expect(DictationHold.labelOpacity(CGSize(width: -140, height: 0)) == 0.5)
        #expect(DictationHold.labelOpacity(CGSize(width: -150, height: 0)) == 0)
    }

    @Test func theCircleIsBounded() {
        #expect(DictationHold.circleScale(.zero) == 1)
        #expect(DictationHold.circleScale(CGSize(width: -150, height: 0)) == 0.5)
        #expect(DictationHold.circleScale(CGSize(width: -1000, height: 0)) == 0.4)
        #expect(DictationHold.circleScale(CGSize(width: 0, height: -300)) == 1)
    }

    @Test func whatWasHeardGoesAfterTheDraft() {
        #expect(DictationHold.joined("", "hello") == "hello")
        #expect(DictationHold.joined("Hi", "there") == "Hi there")
        #expect(DictationHold.joined("Hi ", "there") == "Hi there")
        #expect(DictationHold.joined("Hi", "") == "Hi")
    }
}
