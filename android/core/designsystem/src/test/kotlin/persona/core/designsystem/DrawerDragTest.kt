package persona.core.designsystem

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The iPhone release rule and the drawer's velocity share, without a device.
 * `SwipeRelease.goes` is `apple/Persona/Views/Shell/ShellPager.swift:193-196`. The share is
 * `AssistantSidebar.swift:119-121` (`velocity / way`), not the pager's negated share (`ShellPager.swift:157`).
 */
class DrawerDragTest {
    @Test fun flickOver300GoesThatWayAndUnder300UsesTheProjection() {
        // Exactly 300 is not a flick (`abs(velocity) > 300`).
        assertFalse(DrawerDrag.settlesOpen(0f, 300f, 300f))
        assertTrue(DrawerDrag.settlesOpen(0.5f, 300f, 300f))
        assertTrue(DrawerDrag.settlesOpen(0f, 301f, 300f))
        assertFalse(DrawerDrag.settlesOpen(0.9f, -301f, 300f))
        assertFalse(DrawerDrag.settlesOpen(0f, 299f, 300f))
        // Width 300: 0.4 + 100 * 0.2 / 300 = 0.466 close; 0.4 + 200 * 0.2 / 300 = 0.533 open.
        assertFalse(DrawerDrag.settlesOpen(0.4f, 100f, 300f))
        assertTrue(DrawerDrag.settlesOpen(0.4f, 200f, 300f))
        assertFalse(DrawerDrag.settlesOpen(0.6f, -200f, 300f))
        assertTrue(DrawerDrag.settlesOpen(0.6f, -100f, 300f))
    }

    @Test fun shareIsVelocityOverTheRemainingDistanceAndClampsTheOtherWay() {
        // width 300, progress 0, target 1, velocity 300: share 1, and Compose progress velocity is 1 per second.
        assertEquals(1f, DrawerDrag.velocityShare(0f, 1f, 300f, 300f), 0.0001f)
        assertEquals(1f, DrawerDrag.progressPerSecond(0f, 1f, 300f, 300f), 0.0001f)
        // Closing: way is negative, finger velocity is negative, share stays positive, progress velocity is negative.
        assertEquals(1.25f, DrawerDrag.velocityShare(0.8f, 0f, -300f, 300f), 0.0001f)
        assertEquals(-1f, DrawerDrag.progressPerSecond(0.8f, 0f, -300f, 300f), 0.0001f)
        // Finger heading the other way: share 0 (the pager's negation would do this for a finger going with the page).
        assertEquals(0f, DrawerDrag.velocityShare(0.4f, 1f, -100f, 300f), 0f)
        assertEquals(0f, DrawerDrag.progressPerSecond(0.4f, 1f, -100f, 300f), 0f)
        // abs(way) <= 1 contributes nothing. way = 0.01 * 100 = 1.
        assertEquals(0f, DrawerDrag.velocityShare(0f, 0.01f, 500f, 100f), 0f)
        assertEquals(0.5f, DrawerDrag.velocityShare(0f, 0.02f, 1f, 100f), 0.0001f)
    }

    @Test fun trackingClampsAndTakeRequiresAStrictlyHorizontalMove() {
        assertEquals(0.5f, DrawerDrag.tracked(0f, 150f, 300f), 0.0001f)
        assertEquals(0f, DrawerDrag.tracked(0.2f, -500f, 300f), 0f)
        assertEquals(1f, DrawerDrag.tracked(0.8f, 500f, 300f), 0f)
        // Equal |dx| and |dy| is not within 45° (`abs(dx) > abs(dy)`, AssistantSidebar.swift:148).
        assertFalse(DrawerDrag.takes(10f, 10f, isOpen = false))
        assertFalse(DrawerDrag.takes(10f, 10f, isOpen = true))
        assertTrue(DrawerDrag.takes(10f, 9f, isOpen = false))
        assertFalse(DrawerDrag.takes(-10f, 0f, isOpen = false))
        assertTrue(DrawerDrag.takes(-10f, 0f, isOpen = true))
    }

    @Test fun refusalMatchesTheIphoneOrder() {
        assertEquals(DrawerDrag.Decision.SystemBack, decide(dx = 20f, inSystemBack = true))
        assertEquals(DrawerDrag.Decision.Pushed, decide(dx = 20f, atRoot = false))
        assertEquals(DrawerDrag.Decision.Direction, decide(dx = 10f, dy = 30f))
        assertEquals(DrawerDrag.Decision.Direction, decide(dx = -20f))
        assertEquals(DrawerDrag.Decision.SidewaysScroller, decide(dx = 20f, sidewaysRoom = true))
        assertEquals(DrawerDrag.Decision.Take, decide(dx = 20f))
        // Open: a leftward drag on a swipe row is the row's (line 500). A sideways scroller is not checked.
        assertEquals(DrawerDrag.Decision.SwipeRow, decide(dx = -20f, isOpen = true, onSwipeRow = true))
        assertEquals(DrawerDrag.Decision.Take, decide(dx = -20f, isOpen = true, sidewaysRoom = true))
        assertEquals(DrawerDrag.Decision.Take, decide(dx = 20f, isOpen = true, sidewaysRoom = true))
        assertEquals(DrawerDrag.Decision.Direction, decide(dx = -10f, dy = 30f, isOpen = true, onSwipeRow = true))
        // A child that already owns the move keeps it, closed or open. A steeper move is still just direction.
        assertEquals(DrawerDrag.Decision.Child, decide(dx = 20f, childOwns = true))
        assertEquals(DrawerDrag.Decision.Child, decide(dx = 20f, isOpen = true, childOwns = true))
        assertEquals(DrawerDrag.Decision.Direction, decide(dx = 10f, dy = 40f, childOwns = true))
        assertEquals(DrawerDrag.Decision.SwipeRow, decide(dx = -20f, isOpen = true, onSwipeRow = true, childOwns = true))
    }

    @Test fun trailingInsetStillRefusesAndLeadingOnlyOutsideTheBand() {
        // Trailing system-back inset always refuses. Leading inset is the drawer's only inside the
        // centred exclusion band (#9803); outside that band system Back keeps the edge.
        val height = 640f
        val band = 200f
        val midY = height / 2f
        assertFalse(inset(x = 10f, y = midY, height = height, band = band))
        assertTrue(inset(x = 10f, y = 10f, height = height, band = band))
        assertTrue(inset(x = 10f, y = height - 10f, height = height, band = band))
        assertFalse(inset(x = 80f, y = midY, height = height, band = band))
        assertTrue(inset(x = 330f, y = midY, height = height, band = band))
        assertFalse(inset(x = 10f, y = midY, leading = 0f, trailing = 0f, height = height, band = band))
        // A pushed page still refuses via atRoot, not the left inset (`AssistantSidebar.swift:505-509`).
        assertEquals(DrawerDrag.Decision.Pushed, decide(dx = 20f, atRoot = false, inSystemBack = false))
        assertEquals(DrawerDrag.Decision.Take, decide(dx = 20f, atRoot = true, inSystemBack = false))
    }

    private fun inset(
        x: Float,
        y: Float,
        leading: Float = 48f,
        trailing: Float = 48f,
        width: Float = 360f,
        height: Float,
        band: Float,
    ) = DrawerDrag.inSystemBackInset(x, y, leading, trailing, width, height, band)

    @Test fun liftAndHairlineMatchTheIphoneAndStayExactAtRest() {
        assertEquals(8f, DrawerDrag.liftPx(0f, 8f), 0f)
        assertEquals(4f, DrawerDrag.liftPx(0.5f, 8f), 0.0001f)
        assertEquals(0f, DrawerDrag.liftPx(1f, 8f), 0f)
        assertNull(DrawerDrag.hairlineAlpha(0f, 0.12f))
        assertEquals(0.06f, DrawerDrag.hairlineAlpha(0.5f, 0.12f)!!, 0.0001f)
        assertEquals(0.12f, DrawerDrag.hairlineAlpha(1f, 0.12f)!!, 0f)
    }

    private fun decide(
        dx: Float,
        dy: Float = 0f,
        isOpen: Boolean = false,
        atRoot: Boolean = true,
        onSwipeRow: Boolean = false,
        sidewaysRoom: Boolean = false,
        inSystemBack: Boolean = false,
        childOwns: Boolean = false,
    ) = DrawerDrag.decision(dx, dy, isOpen, atRoot, onSwipeRow, sidewaysRoom, inSystemBack, childOwns)
}
