package persona.core.designsystem

import androidx.compose.animation.core.SpringSpec
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.math.PI
import kotlin.math.pow

/** `SplitColumnControl` and `Theme.Split`, without a device. */
class SidebarColumnDragTest {
    @Test fun tokensMatchTheSplitConstants() {
        assertEquals(Tokens.Size.iconXS, ColumnDrag.fullMaterial)
        // Blur reach is `Tokens.Size.box80` (`Theme.Split.blurReach`, `Theme.swift:49`).
        assertEquals(0.8f, columnDragBlur(Tokens.Size.box80.value), 0.0001f)
    }

    @Test fun takeOverMatchesTheEdgeAndTheAngle() {
        val edge = (Tokens.MinTouch.ios / 2).value
        assertEquals(ColumnDragZone.Open, take(shown = false, startX = edge, vx = 2f, vy = 0f))
        assertNull(take(shown = false, startX = edge + 0.1f, vx = 2f, vy = 0f))
        assertNull(take(shown = false, startX = 0f, vx = 1.5f, vy = 1f))
        assertEquals(ColumnDragZone.Open, take(shown = false, startX = 0f, vx = 1.51f, vy = 1f))
        assertNull(take(shown = false, startX = 0f, vx = 0f, vy = 0f))
        assertNull(take(shown = false, startX = 0f, vx = -2f, vy = 0f))
        assertNull(take(shown = false, detailAtRoot = false, startX = 0f, vx = 2f, vy = 0f))
        assertNull(take(drags = false, shown = false, startX = 0f, vx = 2f, vy = 0f))
        assertEquals(ColumnDragZone.Close, take(shown = true, startX = 320f, vx = -2f, vy = 0f))
        assertEquals(ColumnDragZone.Close, take(shown = true, startX = 320f - edge, vx = -2f, vy = 0f))
        assertNull(take(shown = true, startX = 320f - edge - 0.1f, vx = -2f, vy = 0f))
        assertNull(take(shown = true, startX = 320f, vx = 2f, vy = 0f))
        assertNull(take(shown = true, startX = 320f, vx = -1.5f, vy = 1f))
        // Close does not ask whether the detail can go back (`:139-146` versus `:137`).
        assertEquals(ColumnDragZone.Close, take(shown = true, detailAtRoot = false, startX = 320f, vx = -2f, vy = 0f))
    }

    @Test fun theLineIsClampedOneToOne() {
        assertEquals(0f, columnDragLine(opening = true, width = 320f, dx = -40f), 0f)
        assertEquals(40f, columnDragLine(opening = true, width = 320f, dx = 40f), 0f)
        assertEquals(320f, columnDragLine(opening = true, width = 320f, dx = 800f), 0f)
        assertEquals(280f, columnDragLine(opening = false, width = 320f, dx = -40f), 0f)
        assertEquals(320f, columnDragLine(opening = false, width = 320f, dx = 40f), 0f)
        assertEquals(0f, columnDragLine(opening = false, width = 320f, dx = -800f), 0f)
    }

    @Test fun releaseUsesTheFlingThenHalfTheWidth() {
        assertTrue(columnDragShows(line = 0f, width = 320f, velocity = 500.1f))
        assertTrue(!columnDragShows(line = 320f, width = 320f, velocity = -500.1f))
        // Exactly 500 dp/s is not a fling (`abs(velocity) > fling`).
        assertTrue(!columnDragShows(line = 0f, width = 320f, velocity = 500f))
        assertTrue(columnDragShows(line = 320f, width = 320f, velocity = -500f))
        assertTrue(!columnDragShows(line = 160f, width = 320f, velocity = 0f))
        assertTrue(columnDragShows(line = 160.1f, width = 320f, velocity = 0f))
        assertTrue(!columnDragShows(line = 159f, width = 320f, velocity = 100f))
    }

    @Test fun velocityShareIsTheFingerVelocityOverTheDistance() {
        assertEquals(3f, columnDragVelocityShare(1200f, 400f), 0f)
        assertEquals(1200f, columnDragVelocityShare(1200f, 400f) * 400f, 0f)
        assertEquals(0f, columnDragVelocityShare(1200f, 0f), 0f)
        val spring = columnDragSettleSpring().spec<Float>()
        assertTrue(spring is SpringSpec<*>)
        val spec = spring as SpringSpec<*>
        assertEquals(1f, spec.dampingRatio, 0f)
        assertEquals((2.0 * PI / 0.28).pow(2).toFloat(), spec.stiffness, 1f)
    }

    @Test fun blurAndDimAndGuide() {
        assertEquals(0f, columnDragBlur(0f), 0f)
        assertEquals(0.4f, columnDragBlur(40f), 0.0001f)
        assertEquals(0.8f, columnDragBlur(80f), 0.0001f)
        assertEquals(0.8f, columnDragBlur(160f), 0.0001f)
        assertEquals(0.8f, columnDragBlur(-80f), 0.0001f)
        assertEquals(ColumnDrag.fullMaterial * 0.8f, columnDragBlurRadius(80f, ColumnDrag.fullMaterial))
        assertEquals(0f, columnDragOverDim(0f, 320f), 0f)
        assertEquals(0.1f, columnDragOverDim(160f, 320f), 0.0001f)
        assertEquals(0.2f, columnDragOverDim(320f, 320f), 0.0001f)
        assertEquals(0f, columnDragOverDim(160f, 0f), 0f)
        assertEquals(0f, columnDragGuideX(0f, 3f, 400f), 0f)
        assertEquals(48.5f, columnDragGuideX(50f, 3f, 400f), 0f)
        assertEquals(397f, columnDragGuideX(400f, 3f, 400f), 0f)
        assertEquals(0f, columnDragGuideX(2f, 3f, 2f), 0f)
        // API 31 is `Build.VERSION_CODES.S`. Older platforms draw no blur; the call is not made.
        assertNull(columnDragBlurEffect(8f, sdk = 28))
        assertNull(columnDragBlurEffect(0.4f, sdk = 35))
    }

    private fun take(
        drags: Boolean = true,
        shown: Boolean,
        detailAtRoot: Boolean = true,
        startX: Float,
        vx: Float,
        vy: Float,
        columnWidth: Float = 320f,
    ) = columnDragTakeOver(drags, shown, detailAtRoot, startX, columnWidth, (Tokens.MinTouch.ios / 2).value, vx, vy)
}
