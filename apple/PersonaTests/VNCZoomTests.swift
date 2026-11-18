import CoreGraphics
import Testing
@testable import Persona

/// Two fingers on the Mac's screen in Mouse Mode as a scroll or a pinch (#6196, VNCTwoFingers), and where a zoomed-in
/// picture goes (VNCZoom): zooming about a point, following the pointer, the phone turning.
struct VNCZoomTests {
    /// A touch of two fingers `spread` apart around (200, 300), moved in `steps` steps to `to` around a center `travel`
    /// points down: what it is after each step.
    private func touch(spread: CGFloat = 80, to: CGFloat, travel: CGFloat, steps: Int = 20) -> [VNCTwoFingers.Meaning] {
        var two = VNCTwoFingers()
        two.began(center: CGPoint(x: 200, y: 300), spread: spread)
        return (1...steps).map { i in
            let k = CGFloat(i) / CGFloat(steps)
            return two.moved(center: CGPoint(x: 200, y: 300 + travel * k), spread: spread + (to - spread) * k)
        }
    }

    @Test func aScrollWhoseFingersDriftApartNeverZooms() {
        // 150 pt down while the fingers open from 80 to 100 pt (25%): a scroll, from its first few points, to the end.
        let seen = touch(to: 100, travel: 150)
        #expect(!seen.contains(.zoom))
        #expect(seen.last == .scroll)
        #expect(seen.firstIndex(of: .scroll) ?? 99 <= 1)
        // Closing as much, the same.
        #expect(!touch(to: 60, travel: -150).contains(.zoom))
    }

    @Test func aPinchZoomsOnceItOpensFifteenPercent() {
        // Opening from 80 to 160 pt in place: undecided, then a pinch at 92 pt (15%) and from there on.
        let seen = touch(to: 160, travel: 0)
        #expect(seen.first == .undecided) // 84 pt: not yet
        #expect(seen.last == .zoom)
        #expect(!seen.contains(.scroll))
        // Pinching in as well.
        #expect(touch(spread: 160, to: 80, travel: 0).last == .zoom)
    }

    @Test func aPinchWithOneFingerStillZooms() {
        // The thumb stays and the index finger goes 60 pt further: the middle moves half as far as the spread opens.
        #expect(touch(to: 140, travel: 30).last == .zoom)
    }

    @Test func fingersCloseTogetherNeedTenPoints() {
        // 30 pt apart: 15% is 4.5 pt, a finger settling; it takes 10.
        var two = VNCTwoFingers()
        two.began(center: .zero, spread: 30)
        #expect(two.moved(center: .zero, spread: 36) == .undecided)
        #expect(two.moved(center: .zero, spread: 41) == .zoom)
    }

    @Test func oneMeaningUntilTheFingersLift() {
        var two = VNCTwoFingers()
        two.began(center: .zero, spread: 80)
        #expect(two.moved(center: CGPoint(x: 0, y: 10), spread: 80) == .scroll)
        #expect(two.moved(center: CGPoint(x: 0, y: 12), spread: 200) == .scroll) // a pinch later on: still the scroll
        two.ended()
        #expect(two.meaning == .scroll) // for the lifting callbacks
        // The next touch, its start not seen: from where it is first moved.
        #expect(two.moved(center: CGPoint(x: 50, y: 50), spread: 80) == .undecided)
        #expect(two.moved(center: CGPoint(x: 50, y: 50), spread: 120) == .zoom)
    }

    @Test func zoomInKeepsTheAnchorWhereItIs() {
        let fitted = CGRect(x: 0, y: 100, width: 400, height: 250)
        let r = VNCZoom.zoomed(fitted, by: 2, at: CGPoint(x: 100, y: 150))
        #expect(r == CGRect(x: -100, y: 50, width: 800, height: 500))
        // The anchor's place on the picture is the same before and after.
        #expect((100 - r.minX) / r.width == (100 - fitted.minX) / fitted.width)
    }

    @Test func aZoomedPictureFollowsThePointerToTheEdges() {
        let size = CGSize(width: 400, height: 600)
        let r = CGRect(x: -200, y: 50, width: 800, height: 500) // as clamped: wider than the view, centered down it
        // Well inside: no move.
        #expect(VNCZoom.following(r, keep: CGPoint(x: 200, y: 250), in: size, margin: 32) == r)
        // Past the right edge's margin by 40: the picture moves 40 to the left.
        #expect(VNCZoom.following(r, keep: CGPoint(x: 408, y: 250), in: size, margin: 32) == r.offsetBy(dx: -40, dy: 0))
        // As far as the picture reaches: never past its own edge.
        #expect(VNCZoom.following(r, keep: CGPoint(x: -500, y: 250), in: size, margin: 32).minX == 0)
        // Shorter than the view: kept in the middle of it whatever the pointer does up and down.
        #expect(VNCZoom.following(r, keep: CGPoint(x: 200, y: -50), in: size, margin: 32).minY == 50)
    }

    @Test func turningKeepsTheZoomAndTheMiddle() throws {
        let picture = CGSize(width: 1600, height: 1000)
        let upright = CGRect(x: 0, y: 0, width: 400, height: 700), side = CGRect(x: 0, y: 0, width: 800, height: 360)
        let fitted = VNCZoom.fit(picture, in: upright) // 400 × 250
        let r = VNCZoom.clamp(VNCZoom.zoomed(fitted, by: 3, at: CGPoint(x: 300, y: 350)), in: upright.size)
        let turned = try #require(VNCZoom.turned(r, picture: picture, from: upright, to: side))
        // Three times the new fit (576 × 360 → 1728 wide).
        #expect(abs(turned.width / VNCZoom.fit(picture, in: side).width - 3) < 0.001)
        // The part that was in the middle is in the middle again.
        let before = CGPoint(x: (upright.midX - r.minX) / r.width, y: (upright.midY - r.minY) / r.height)
        let after = CGPoint(x: (side.midX - turned.minX) / turned.width, y: (side.midY - turned.minY) / turned.height)
        #expect(abs(before.x - after.x) < 0.001 && abs(before.y - after.y) < 0.001)
        // Fitted stays fitted.
        #expect(VNCZoom.turned(fitted, picture: picture, from: upright, to: side) == nil)
    }
}
