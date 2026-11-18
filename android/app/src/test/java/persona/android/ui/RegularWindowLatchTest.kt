package persona.android.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** The Settings sheet keeps its list-detail scene only in a window that has been regular: a phone never changes scenes. */
class RegularWindowLatchTest {
    @Test fun aWindowThatIsNeverRegularNeverKeepsTheScene() {
        val latch = RegularWindowLatch(regular = false)
        repeat(3) { assertFalse(latch.seen(false)) }
    }

    @Test fun aWindowThatStartsRegularKeepsItWhenItNarrows() {
        val latch = RegularWindowLatch(regular = true)
        assertTrue(latch.seen(true))
        assertTrue(latch.seen(false))
        assertTrue(latch.seen(true))
    }

    @Test fun aWindowThatBecomesRegularKeepsItFromThen() {
        val latch = RegularWindowLatch(regular = false)
        assertFalse(latch.seen(false))
        assertTrue(latch.seen(true))
        assertTrue(latch.seen(false))
    }

    @Test fun readingTheSameHistoryAgainGivesTheSameAnswer() {
        val latch = RegularWindowLatch(regular = false)
        assertTrue(latch.seen(true))
        // A recomposition that runs again (or is thrown away) cannot undo or double the change.
        assertTrue(latch.seen(true))
        assertTrue(latch.seen(true))
    }
}
