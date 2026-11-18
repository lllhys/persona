package persona.feature.chat

import androidx.compose.ui.graphics.Color
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Item
import persona.core.domain.diff.WordSpan

/** An edit's tool diff: its lines, the words that changed in them, and what shows before Show All. */
class ToolDiffTest {
    private fun tool(name: String, vararg fields: Pair<String, String>) = Item("call", kind = "tool_use", toolName = name,
        toolInput = buildJsonObject { fields.forEach { (key, value) -> put(key, value) } })

    private fun lines(count: Int) = (1..count).joinToString("\n") { "line $it" }

    @Test fun aReplacedWordIsMarkedInBothLinesOfTheEdit() {
        val diff = toolDiff(tool("Edit", "old_string" to "keep\nval size = small\n", "new_string" to "keep\nval size = large\n"))!!
        assertEquals(listOf(' ', '-', '+'), diff.lines.map { it.sign })
        val removed = diff.lines[1]
        val added = diff.lines[2]
        assertEquals(listOf(WordSpan(11, 16)), removed.changed)
        assertEquals(listOf(WordSpan(11, 16)), added.changed)
        assertEquals("small", removed.text.substring(11, 16))
        assertEquals("large", added.text.substring(11, 16))
        assertTrue(diff.lines[0].changed.isEmpty())
        assertEquals(1, diff.added)
        assertEquals(1, diff.removed)
    }

    @Test fun anEditNumbersNoLinesButAWriteNumbersItsOwn() {
        val edited = toolDiff(tool("Edit", "old_string" to "a\nb", "new_string" to "a\nc"))!!
        assertTrue(edited.lines.all { it.old == null && it.new == null })
        val write = toolDiff(tool("Write", "content" to "x\ny\n"))!!
        assertEquals(listOf(1, 2), write.lines.map { it.new })
        assertTrue(write.lines.all { it.old == null && it.changed.isEmpty() })
    }

    @Test fun aMultiEditComparesEachEditOnItsOwn() {
        val input = buildJsonObject {
            put("edits", buildJsonArray {
                add(buildJsonObject { put("old_string", "let a = 1"); put("new_string", "let a = 2") })
                add(buildJsonObject { put("old_string", "let b = 1"); put("new_string", "let b = 2") })
            })
        }
        val diff = toolDiff(Item("call", kind = "tool_use", toolName = "MultiEdit", toolInput = input))!!
        assertEquals(listOf('-', '+', '-', '+'), diff.lines.map { it.sign })
        assertEquals(listOf("1", "2", "1", "2"), diff.lines.map { it.text.substring(it.changed.single().start, it.changed.single().end) })
    }

    @Test fun aLongStretchOfContextFoldsAndKeepsThreeLinesAroundTheChange() {
        val old = lines(40)
        val diff = toolDiff(tool("Edit", "old_string" to old, "new_string" to old.replace("line 20\n", "line twenty\n")))!!
        val rows = diff.folded()
        assertEquals(DiffRow.Fold(0, 16), rows.first())
        assertTrue(rows.last() is DiffRow.Fold)
        // the fold takes away rows; what is left lines up with the whole diff by index
        assertTrue(rows.size < diff.lines.size)
        val shown = rows.filterIsInstance<DiffRow.Line>()
        assertEquals((16..22).toList(), shown.take(7).map { it.index })
        assertEquals(shown.map { it.line }, shown.map { diff.lines[it.index] })
        assertEquals(diff.lines.size, rows.sumOf { if (it is DiffRow.Fold) it.count else 1 })
    }

    @Test fun aShortDiffAndAWriteAreNotFolded() {
        val short = toolDiff(tool("Edit", "old_string" to "a\nb\nc", "new_string" to "a\nx\nc"))!!
        assertTrue(short.folded().all { it is DiffRow.Line })
        val write = toolDiff(tool("Write", "content" to lines(100)))!!
        assertEquals(100, write.folded().size)
        assertTrue(write.folded().all { it is DiffRow.Line })
    }

    @Test fun aCodexPatchShowsWholeWithItsNumbersAndNoWordMarks() {
        val patch = "@@ -1,12 +1,12 @@\n" + (1..5).joinToString("") { " ctx $it\n" } + "-let a = 1\n+let a = 2\n" + (6..11).joinToString("") { " ctx $it\n" }
        val diff = toolDiff(tool("Edit", "diff" to patch, "change" to "update"))!!
        assertFalse(diff.foldsContext)
        assertTrue(diff.folded().all { it is DiffRow.Line })
        assertEquals(diff.lines.size, diff.folded().size)
        assertTrue(diff.lines.all { it.changed.isEmpty() })
        assertEquals(6, diff.lines.first { it.sign == '-' }.old)
    }

    @Test fun aNewOrDeletedFileIsAllAddedOrAllRemoved() {
        val created = toolDiff(tool("Edit", "diff" to "one\ntwo\n", "change" to "add"))!!
        assertEquals(listOf('+', '+'), created.lines.map { it.sign })
        val deleted = toolDiff(tool("Edit", "diff" to "one\ntwo\n", "change" to "delete"))!!
        assertEquals(listOf('-', '-'), deleted.lines.map { it.sign })
        assertEquals(listOf(1, 2), deleted.lines.map { it.old })
    }

    @Test fun anEditWithTooMuchChangeIsAllRemovedThenAllAdded() {
        val old = (1..600).joinToString("\n") { "old $it" }
        val new = (1..600).joinToString("\n") { "new $it" }
        val diff = toolDiff(tool("Edit", "old_string" to old, "new_string" to new))!!
        assertEquals(600, diff.removed)
        assertEquals(600, diff.added)
        assertEquals(List(600) { '-' } + List(600) { '+' }, diff.lines.map { it.sign })
        assertTrue(diff.lines.all { it.changed.isEmpty() })
    }

    @Test fun theChangedWordsGetTheirTintAndTabsKeepTheRangesAligned() {
        val line = DiffLine("\tcall(a, old)", '-', null, null, listOf(WordSpan(9, 12)))
        val word = Color(0x33FF0000)
        val text = diffText(line, word)
        assertEquals("    call(a, old)", text.text)
        val span = text.spanStyles.single()
        assertEquals(word, span.item.background)
        assertEquals("old", text.text.substring(span.start, span.end))
        // a line with nothing changed carries no style at all
        assertTrue(diffText(DiffLine("plain", ' ', null, null), word).spanStyles.isEmpty())
    }

    @Test fun aStreamingEditKeepsTheRowsItAlreadyHad() {
        val old = lines(30)
        val first = toolDiff(tool("Edit", "old_string" to old, "new_string" to old.replace("line 15\n", "line 15!\n")))!!
        val grown = toolDiff(tool("Edit", "old_string" to old, "new_string" to old.replace("line 15\n", "line 15!\n").replace("line 25\n", "line 25!\n")))!!
        // the rows before the second change keep their index, so a keyed list does not move them
        val before = first.folded().filterIsInstance<DiffRow.Line>().filter { it.index < 16 }
        assertEquals(before, grown.folded().filterIsInstance<DiffRow.Line>().filter { it.index < 16 }.take(before.size))
    }
}
