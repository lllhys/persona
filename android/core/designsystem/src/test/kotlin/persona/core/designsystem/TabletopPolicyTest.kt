package persona.core.designsystem

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TabletopPolicyTest {
    private val hinge = WindowLayout(tabletop = true, hingeTop = 400f, hingeBottom = 420f)

    private fun resolve(
        window: WindowLayout = hinge,
        top: Int = 60,
        dock: Int? = null,
        height: Int = 900,
        minimum: Int = 48,
        bottomInset: Int = 0,
    ): TabletopPageLayout = tabletopPageLayout(window, top, dock, height, minimum, bottomInset)

    @Test fun flatOrInvalidHingesStaySingle() {
        assertEquals(TabletopPageLayout.SinglePage, resolve(window = WindowLayout()))
        assertEquals(TabletopPageLayout.SinglePage, resolve(window = hinge.copy(tabletop = false)))
        assertEquals(TabletopPageLayout.SinglePage, resolve(window = hinge.copy(hingeTop = 400f, hingeBottom = 400f)))
        assertEquals(TabletopPageLayout.SinglePage, resolve(window = hinge.copy(hingeTop = 420f, hingeBottom = 400f)))
    }

    @Test fun aPageWithoutADockLeavesTheLowerSideAsBackground() {
        val split = resolve() as TabletopPageLayout.Split
        assertEquals(400, split.bodyHeight)
        assertEquals(420, split.hingeBottom)
        assertEquals(TabletopDockPlacement.BackgroundOnly, split.dockPlacement)
    }

    @Test fun aDockIsCentredInTheMeasuredLowerSafeArea() {
        val split = resolve(dock = 80) as TabletopPageLayout.Split
        assertEquals(400, split.bodyHeight)
        assertEquals(TabletopDockPlacement.BottomBar(top = 620), split.dockPlacement)
    }

    @Test fun theUpperSideNeedsTheWholeTopBarAndOneTouchTarget() {
        val exact = hinge.copy(hingeTop = 108f, hingeBottom = 120f)
        assertTrue(resolve(window = exact) is TabletopPageLayout.Split)
        assertEquals(TabletopPageLayout.SinglePage, resolve(window = exact.copy(hingeTop = 107f)))
    }

    @Test fun onlyAPageWithADockNeedsUsableLowerHeight() {
        val shortLower = hinge.copy(hingeBottom = 850f)
        assertEquals(TabletopPageLayout.SinglePage, resolve(window = shortLower, dock = 80))
        assertTrue(resolve(window = shortLower, dock = null) is TabletopPageLayout.Split)
    }

    @Test fun aDockUsesTheLowerAreaOutsideTheBottomSystemInset() {
        val exact = resolve(dock = 80, height = 540, bottomInset = 40) as TabletopPageLayout.Split
        assertEquals(TabletopDockPlacement.BottomBar(top = 420), exact.dockPlacement)
        assertEquals(TabletopPageLayout.SinglePage, resolve(dock = 80, height = 530, bottomInset = 40))
    }

    @Test fun fractionalHingeEdgesRoundAwayFromContent() {
        val split = resolve(window = hinge.copy(hingeTop = 400.9f, hingeBottom = 420.2f)) as TabletopPageLayout.Split
        assertEquals(400, split.bodyHeight)
        assertEquals(421, split.hingeBottom)
    }

    @Test fun hingeCoordinatesAreClampedWithoutNegativeGeometry() {
        val beyondBottom = resolve(window = hinge.copy(hingeBottom = 1_000f)) as TabletopPageLayout.Split
        assertEquals(900, beyondBottom.hingeBottom)
        assertEquals(400, beyondBottom.bodyHeight)
        assertEquals(TabletopDockPlacement.BackgroundOnly, beyondBottom.dockPlacement)
        assertEquals(TabletopPageLayout.SinglePage, resolve(window = hinge.copy(hingeBottom = 1_000f), dock = 80))

        val beyondTop = resolve(
            window = hinge.copy(hingeTop = -40f, hingeBottom = 200f),
            top = 0,
            minimum = 0,
        ) as TabletopPageLayout.Split
        assertEquals(0, beyondTop.bodyHeight)
        assertTrue(beyondTop.hingeBottom >= beyondTop.bodyHeight)

        assertEquals(TabletopPageLayout.SinglePage, resolve(window = hinge.copy(hingeTop = 950f, hingeBottom = 1_000f)))
        assertEquals(TabletopPageLayout.SinglePage, resolve(height = 0))
    }
}
