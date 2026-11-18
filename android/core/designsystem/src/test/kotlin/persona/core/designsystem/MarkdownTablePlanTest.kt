package persona.core.designsystem

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MarkdownTablePlanTest {
    @Test fun emptyTableFits() {
        assertEquals(MarkdownTablePlan(emptyList(), scrolls = false, fits = true),
            MarkdownTablePlan.fit(emptyList(), available = 360, minimum = 160, maximum = 220))
    }

    @Test fun tableWhoseCellsFitTheirEqualSharesKeepsTheLegacyLayout() {
        val plan = MarkdownTablePlan.fit(listOf(80, 100), available = 360, minimum = 160, maximum = 220)
        assertTrue(plan.fits)
        assertFalse(plan.scrolls)
        assertEquals(listOf(80, 100), plan.widths)
    }

    @Test fun fittingBoundaryDoesNotScroll() {
        val plan = MarkdownTablePlan.fit(listOf(20, 20), available = 320, minimum = 160, maximum = 220)
        assertTrue(plan.fits)
        assertFalse(plan.scrolls)
    }

    @Test fun aFittingColumnIsNotCappedBecauseItsRendererDoesNotChange() {
        val plan = MarkdownTablePlan.fit(listOf(300), available = 360, minimum = 160, maximum = 220)
        assertTrue(plan.fits)
        assertEquals(listOf(300), plan.widths)
    }

    @Test fun allShortColumnsThatCannotFitScrollWithoutBeingPadded() {
        val plan = MarkdownTablePlan.fit(listOf(10, 20, 30, 40), available = 90, minimum = 160, maximum = 220)
        assertFalse(plan.fits)
        assertTrue(plan.scrolls)
        assertEquals(listOf(10, 20, 30, 40), plan.widths)
    }

    @Test fun longColumnsWrapAtTheMaximumAndOnlyTheTableScrolls() {
        val plan = MarkdownTablePlan.fit(listOf(500, 80, 700, 120), available = 360, minimum = 160, maximum = 220)
        assertFalse(plan.fits)
        assertTrue(plan.scrolls)
        assertEquals(listOf(220, 80, 220, 120), plan.widths)
        assertTrue(plan.widths.sum() > 360)
    }

    @Test fun unevenColumnsShareTheRoomInsteadOfEllipsizingTheLongOne() {
        val plan = MarkdownTablePlan.fit(listOf(80, 600), available = 400, minimum = 160, maximum = 220)
        assertFalse(plan.fits)
        assertFalse(plan.scrolls)
        assertEquals(listOf(80, 220), plan.widths)
    }

    @Test fun waterFilledColumnsNeverDropBelowTheirOwnFloors() {
        val natural = listOf(160, 400, 800)
        val plan = MarkdownTablePlan.fit(natural, available = 600, minimum = 100, maximum = 900)
        assertFalse(plan.fits)
        assertFalse(plan.scrolls)
        assertTrue(plan.widths.sum() <= 600)
        assertTrue(plan.widths.zip(natural).all { (width, content) -> width >= minOf(content, 100) })
        assertEquals(160, plan.widths.first())
    }

    @Test fun oneLongColumnCanWrapWithoutClaimingTheWholeBubble() {
        val plan = MarkdownTablePlan.fit(listOf(1_000), available = 600, minimum = 160, maximum = 220)
        assertFalse(plan.fits)
        assertFalse(plan.scrolls)
        assertEquals(listOf(220), plan.widths)
    }

    @Test fun aScaledMinimumDoesNotMakeAnUpstreamFittingTableScroll() {
        val plan = MarkdownTablePlan.fit(listOf(100, 120), available = 360, minimum = 208, maximum = 286,
            legacyColumnWidth = 160)
        assertTrue(plan.fits)
        assertFalse(plan.scrolls)
    }

    @Test fun naturalWidthsSurviveAnUpstreamFixedWidthOverflowBelowTheDefaultFontScale() {
        val plan = MarkdownTablePlan.fit(listOf(80, 100), available = 300, minimum = 136, maximum = 187,
            legacyColumnWidth = 160)
        assertFalse(plan.fits)
        assertFalse(plan.scrolls)
        assertEquals(listOf(80, 100), plan.widths)
    }

    @Test fun pinningAColumnRecomputesTheRemainingProportionalShares() {
        val plan = MarkdownTablePlan.fit(listOf(290, 470), available = 418, minimum = 160, maximum = 220)
        assertFalse(plan.fits)
        assertFalse(plan.scrolls)
        assertEquals(listOf(160, 220), plan.widths)
    }

    @Test fun aColumnFitsTheViewportWhenItsMinimumAllowsIt() {
        val plan = MarkdownTablePlan.fit(listOf(1_000, 1_000), available = 360, minimum = 320, maximum = 440,
            legacyColumnWidth = 160)
        assertFalse(plan.fits)
        assertTrue(plan.scrolls)
        assertEquals(listOf(360, 360), plan.widths)
    }

    @Test fun imageSourceTextDoesNotDefeatTheUpstreamFitDecision() {
        val plan = MarkdownTablePlan.fit(natural = listOf(500, 80), available = 360, minimum = 160, maximum = 220,
            legacyColumnWidth = 160, legacyNatural = listOf(32, 80))
        assertTrue(plan.fits)
        assertFalse(plan.scrolls)
    }

    @Test fun aOneDigitColumnBesideTwoSentenceColumnsKeepsItsNaturalWidth() {
        val plan = MarkdownTablePlan.fit(listOf(24, 500, 600), available = 360, minimum = 160, maximum = 220,
            legacyColumnWidth = 160)
        assertFalse(plan.fits)
        assertFalse(plan.scrolls)
        assertEquals(listOf(24, 160, 176), plan.widths)
    }

    @Test fun threeShortColumnsAndOneLongOneFitByWrappingOnlyTheLongOne() {
        val plan = MarkdownTablePlan.fit(listOf(40, 60, 80, 500), available = 360, minimum = 160, maximum = 220,
            legacyColumnWidth = 160)
        assertFalse(plan.fits)
        assertFalse(plan.scrolls)
        assertEquals(listOf(40, 60, 80, 180), plan.widths)
    }

    @Test fun fontScaleTwoDoesNotPadTheShortFirstColumn() {
        val plan = MarkdownTablePlan.fit(listOf(24, 500, 600), available = 360, minimum = 320, maximum = 440,
            legacyColumnWidth = 160)
        assertFalse(plan.fits)
        assertTrue(plan.scrolls)
        assertEquals(listOf(24, 360, 360), plan.widths)
    }
}
