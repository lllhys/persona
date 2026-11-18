package persona.core.designsystem

import androidx.compose.ui.text.AnnotatedString
import org.junit.Assert.assertEquals
import org.junit.Test

class MarkdownTableMeasurementTest {
    @Test fun appendingARowToATenRowTableMeasuresOnlyTheNewCells() {
        val measured = mutableListOf<String>()
        val cache = MarkdownTableCellMeasureCache(cellPaddingPx = 16) { text, _ ->
            measured += text.text
            text.length
        }
        cache.measure(rows(10), columnCount = 2)
        assertEquals(20, measured.size)

        measured.clear()
        cache.measure(rows(11), columnCount = 2)
        assertEquals(listOf("row-10-column-0", "row-10-column-1"), measured)
    }

    @Test fun removedCellsLeaveTheMemoInsteadOfGrowingItForTheWholeStream() {
        var measurements = 0
        val cache = MarkdownTableCellMeasureCache(cellPaddingPx = 16) { text, _ ->
            measurements++
            text.length
        }
        cache.measure(rows(10), columnCount = 2)
        cache.measure(rows(1), columnCount = 2)
        cache.measure(rows(2), columnCount = 2)
        assertEquals("the removed second row is measured again, not retained outside the current table", 22, measurements)
    }

    private fun rows(count: Int) = List(count) { row ->
        List(2) { column ->
            MarkdownCellMeasurementInput(AnnotatedString("row-$row-column-$column"), header = false, imageOnly = false)
        }
    }
}
