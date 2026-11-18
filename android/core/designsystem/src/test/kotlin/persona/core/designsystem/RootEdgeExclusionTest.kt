package persona.core.designsystem

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Exclusion rects for the left edge at a page root (#9803). Present only while the drawer or column
 * can open from that edge; height stays within Android's 200 dp per-edge cap.
 */
class RootEdgeExclusionTest {
    @Test fun bandHeightIsThePlatformCap() {
        assertEquals(200f, RootEdgeExclusion.heightDp, 0f)
    }

    @Test fun drawerStripUsesTheLeadingInsetAndCentresTheBand() {
        val rect = RootEdgeExclusion.rect(
            active = true,
            stripWidthPx = 48f,
            layoutWidthPx = 411f,
            layoutHeightPx = 891f,
            bandHeightPx = 200f,
        )!!
        assertEquals(0f, rect.left, 0f)
        assertEquals(48f, rect.width, 0f)
        assertEquals(200f, rect.height, 0f)
        assertEquals((891f - 200f) / 2f, rect.top, 0.0001f)
        assertTrue(rect.height <= RootEdgeExclusion.heightDp)
    }

    @Test fun columnStripUsesTheOpenZone() {
        val zone = (Tokens.MinTouch.ios / 2).value
        val rect = RootEdgeExclusion.rect(
            active = true,
            stripWidthPx = zone,
            layoutWidthPx = 951f,
            layoutHeightPx = 673f,
            bandHeightPx = 200f,
        )!!
        assertEquals(zone, rect.width, 0f)
        assertEquals(200f, rect.height, 0f)
        assertEquals((673f - 200f) / 2f, rect.top, 0.0001f)
    }

    @Test fun absentWhenInactiveOrZeroWidthOrPushed() {
        assertNull(
            RootEdgeExclusion.rect(
                active = false,
                stripWidthPx = 48f,
                layoutWidthPx = 411f,
                layoutHeightPx = 891f,
                bandHeightPx = 200f,
            ),
        )
        assertNull(
            RootEdgeExclusion.rect(
                active = true,
                stripWidthPx = 0f,
                layoutWidthPx = 411f,
                layoutHeightPx = 891f,
                bandHeightPx = 200f,
            ),
        )
        // Drawer open / column shown: caller passes active = false (same as a pushed page).
        assertNull(
            RootEdgeExclusion.rect(
                active = false,
                stripWidthPx = 22f,
                layoutWidthPx = 951f,
                layoutHeightPx = 673f,
                bandHeightPx = 200f,
            ),
        )
    }

    @Test fun shortLayoutClampsTheBand() {
        val rect = RootEdgeExclusion.rect(
            active = true,
            stripWidthPx = 48f,
            layoutWidthPx = 200f,
            layoutHeightPx = 120f,
            bandHeightPx = 200f,
        )!!
        assertEquals(120f, rect.height, 0f)
        assertEquals(0f, rect.top, 0f)
    }

    @Test fun cutoutExtendsTheRectToWindowZero() {
        val rect = RootEdgeExclusion.rect(
            active = true,
            stripWidthPx = 22f,
            layoutWidthPx = 400f,
            layoutHeightPx = 800f,
            bandHeightPx = 200f,
            layoutLeftInWindowPx = 48f,
        )!!
        assertEquals(-48f, rect.left, 0f)
        assertEquals(22f, rect.right, 0f)
    }

    @Test fun leadingBandMatchesTheExclusionRect() {
        assertTrue(
            RootEdgeExclusion.inLeadingBand(
                x = 10f, y = 400f, leadingPx = 48f,
                layoutWidthPx = 360f, layoutHeightPx = 800f, bandHeightPx = 200f,
            ),
        )
        assertTrue(
            !RootEdgeExclusion.inLeadingBand(
                x = 10f, y = 10f, leadingPx = 48f,
                layoutWidthPx = 360f, layoutHeightPx = 800f, bandHeightPx = 200f,
            ),
        )
    }
}
