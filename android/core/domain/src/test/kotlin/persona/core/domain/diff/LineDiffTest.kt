package persona.core.domain.diff

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** An edit's lines, words and folds, as iOS's `LineDiffTests` pin them for the same texts. */
class LineDiffTest {
    private fun diff(old: String, new: String) = LineDiff(old, new)
    private fun kinds(d: LineDiff) = d.lines.map { it.kind }
    private fun cut(text: String, spans: List<WordSpan>) = spans.map { text.substring(it.start, it.end) }

    @Test fun sameTextIsAllContext() {
        val d = diff("a\nb\nc\n", "a\nb\nc\n")
        assertEquals(List(3) { LineDiff.Kind.Context }, kinds(d))
        assertEquals(0, d.adds)
        assertEquals(0, d.dels)
        assertTrue(d.lines.all { it.changed.isEmpty() })
    }

    @Test fun aChangedLineIsRemovedThenAddedBetweenItsContext() {
        val d = diff("same\nold value\ntail\n", "same\nnew value\ntail\n")
        assertEquals(listOf(LineDiff.Kind.Context, LineDiff.Kind.Del, LineDiff.Kind.Add, LineDiff.Kind.Context), kinds(d))
        assertEquals(1, d.adds)
        assertEquals(1, d.dels)
        assertEquals(listOf(1 to 1, 2 to null, null to 2, 3 to 3), d.lines.map { it.oldLine to it.newLine })
        // the words that changed, not the whole line
        assertEquals(listOf("old"), cut("old value", d.lines[1].changed))
        assertEquals(listOf("new"), cut("new value", d.lines[2].changed))
    }

    @Test fun aOneWordChangeInALongEditIsOneLineEachWay() {
        val old = (1..30).joinToString("\n") { "line $it of the file" }
        val new = old.replace("line 17 of", "line 17 in")
        val d = diff(old, new)
        assertEquals(1, d.adds)
        assertEquals(1, d.dels)
        assertEquals(31, d.lines.size)
    }

    @Test fun aWriteIsAllAddedAndItsTrailingNewlineIsNotALine() {
        val d = diff("", "first\nsecond\n")
        assertEquals(listOf("first", "second"), d.lines.map { it.text })
        assertEquals(listOf(1, 2), d.lines.map { it.newLine })
        assertEquals(2, d.adds)
        assertEquals(0, d.dels)
        assertEquals(emptyList<LineDiff.Line>(), diff("", "").lines)
    }

    @Test fun aNewlineOnlyAtTheEndOfOneSideIsALineBreakPutIn() {
        val d = diff("x", "x\n")
        assertEquals(listOf(LineDiff.Kind.Context, LineDiff.Kind.Add), kinds(d))
        assertEquals("", d.lines[1].text)
        val gone = diff("x\n", "x")
        assertEquals(listOf(LineDiff.Kind.Context, LineDiff.Kind.Del), kinds(gone))
    }

    @Test fun removedLinesComeBeforeAddedOnesAndAreNumberedOnTheirSide() {
        val d = diff("a\nb\nc\nd", "a\nx\ny\nd")
        assertEquals(listOf("a", "b", "c", "x", "y", "d"), d.lines.map { it.text })
        assertEquals(listOf(LineDiff.Kind.Context, LineDiff.Kind.Del, LineDiff.Kind.Del, LineDiff.Kind.Add, LineDiff.Kind.Add, LineDiff.Kind.Context), kinds(d))
        assertEquals(listOf(1, 2, 3, null, null, 4), d.lines.map { it.oldLine })
        assertEquals(listOf(1, null, null, 2, 3, 4), d.lines.map { it.newLine })
    }

    @Test fun aMovedLineIsOneRemovalAndOneAddition() {
        val d = diff("a\nb\nc\nd", "a\nc\nd\nb")
        assertEquals(1, d.adds)
        assertEquals(1, d.dels)
    }

    @Test fun aSingleInsertionLeavesTheOthersAlone() {
        val d = diff("one\ntwo\nthree", "one\ntwo\ninserted\nthree")
        assertEquals(1, d.adds)
        assertEquals(0, d.dels)
        assertEquals("inserted", d.lines.single { it.kind == LineDiff.Kind.Add }.text)
    }

    @Test fun twoUnlikeLinesAreRewrittenWithNoWordsMarked() {
        val d = diff("completely different text here", "nothing in common at all")
        assertEquals(listOf(LineDiff.Kind.Del, LineDiff.Kind.Add), kinds(d))
        assertTrue(d.lines.all { it.changed.isEmpty() })
        assertFalse(d.capped)
    }

    @Test fun aBigChangeComesBackRemovedThenAddedWithNoWords() {
        val old = (1..6).joinToString("\n") { "old $it" }
        val new = (1..6).joinToString("\n") { "new $it" }
        val d = LineDiff(old, new, maxChangedLines = 10)
        assertTrue(d.capped)
        assertEquals(List(6) { LineDiff.Kind.Del } + List(6) { LineDiff.Kind.Add }, kinds(d))
        assertTrue(d.lines.all { it.changed.isEmpty() })
        // too many characters on one side: the same
        val chars = LineDiff("a\nb", "a\nc", maxChars = 2)
        assertTrue(chars.capped)
        assertEquals(4, chars.adds + chars.dels)
    }

    @Test fun anEditsOwnCommonStartAndEndAreNotCountedAgainstTheBudget() {
        val head = (1..50).joinToString("\n") { "keep $it" }
        val d = LineDiff("$head\nold\ntail", "$head\nnew\ntail", maxChangedLines = 2)
        assertFalse(d.capped)
        assertEquals(listOf("old"), d.lines.filter { it.kind == LineDiff.Kind.Del }.map { it.text })
    }

    @Test fun theWordBudgetStopsMarkingButKeepsTheLines() {
        val d = LineDiff("let a = 1\nlet b = 2", "let a = 9\nlet b = 8", maxWordWork = 1)
        assertEquals(2, d.adds)
        assertEquals(2, d.dels)
        // the first pair spent it: the second is not compared
        assertTrue(d.lines.filter { it.changed.isNotEmpty() }.size <= 2)
    }

    // ── folding ──

    @Test fun aLongRunOfContextFoldsWithThreeLinesKeptAroundTheChange() {
        val old = (1..20).joinToString("\n") { "l$it" }
        val d = diff(old, old.replace("l10", "changed"))
        assertEquals(21, d.lines.size)
        val shown = d.shown()
        assertEquals(Shown.Fold(0, 6), shown.first())
        assertEquals(Shown.Fold(14, 7), shown.last())
        assertEquals((6..13).map { Shown.Line(it) }, shown.subList(1, shown.size - 1))
    }

    @Test fun aDiffWithNoChangeIsShownWhole() {
        val d = diff("a\nb\nc\nd\ne\nf\ng\nh", "a\nb\nc\nd\ne\nf\ng\nh")
        assertEquals((0..7).map { Shown.Line(it) }, d.shown())
        assertEquals(emptyList<Shown>(), foldUnchanged(emptyList()))
    }

    @Test fun oneLoneContextLineIsNotFolded() {
        // changes at 0 and 8: only line 4 lies beyond three lines of either
        val flags = List(9) { it == 0 || it == 8 }
        val shown = foldUnchanged(flags)
        assertEquals((0..8).map { Shown.Line(it) }, shown)
        // two lone lines are a fold
        val two = foldUnchanged(List(10) { it == 0 || it == 9 })
        assertEquals(listOf(Shown.Line(0), Shown.Line(1), Shown.Line(2), Shown.Line(3), Shown.Fold(4, 2), Shown.Line(6), Shown.Line(7), Shown.Line(8), Shown.Line(9)), two)
    }

    @Test fun contextCanBeWiderOrNone() {
        val flags = List(12) { it == 6 }
        assertEquals(listOf(Shown.Fold(0, 5), Shown.Line(5), Shown.Line(6), Shown.Line(7), Shown.Fold(8, 4)), foldUnchanged(flags, context = 1))
        assertEquals(listOf(Shown.Fold(0, 6), Shown.Line(6), Shown.Fold(7, 5)), foldUnchanged(flags, context = 0))
    }

    // ── words ──

    @Test fun wordsInCodeAreMarkedWhereTheyDiffer() {
        val (old, new) = LineDiff.words("foo(a, b)", "foo(a, c)")!!
        assertEquals(listOf("b"), cut("foo(a, b)", old))
        assertEquals(listOf("c"), cut("foo(a, c)", new))
    }

    @Test fun twoChangedWordsWithASpaceBetweenAreOneRange() {
        val (old, new) = LineDiff.words("x one two y", "x uno dos y")!!
        assertEquals(listOf("one two"), cut("x one two y", old))
        assertEquals(listOf("uno dos"), cut("x uno dos y", new))
    }

    @Test fun chineseIsComparedCharacterByCharacter() {
        val (old, new) = LineDiff.words("修改了文件名称", "修改了目录名称")!!
        assertEquals(listOf("文件"), cut("修改了文件名称", old))
        assertEquals(listOf("目录"), cut("修改了目录名称", new))
        val (kana, kana2) = LineDiff.words("これはペンです", "これはペンでした")!!
        assertEquals(listOf("す"), cut("これはペンです", kana))
        assertEquals(listOf("した"), cut("これはペンでした", kana2))
    }

    @Test fun escapesAndQuotesAreOrdinaryCharacters() {
        val a = "say \"hi\"\\n\t"
        val b = "say \"yo\"\\n\t"
        val (old, new) = LineDiff.words(a, b)!!
        assertEquals(listOf("hi"), cut(a, old))
        assertEquals(listOf("yo"), cut(b, new))
    }

    @Test fun aWordPutInOrTakenOutIsMarkedEvenWhenTheLinesAreOtherwiseNotAlike() {
        val a = "return value"
        val b = "return value ?: error(\"missing\")"
        val (old, new) = LineDiff.words(a, b)!!
        assertEquals(emptyList<String>(), cut(a, old))
        assertEquals(listOf(" ?: error(\"missing\")"), cut(b, new))
    }

    @Test fun equalLinesHaveNoRangesAndUnlikeOrTooLongOnesAreNotCompared() {
        assertEquals(emptyList<WordSpan>() to emptyList<WordSpan>(), LineDiff.words("same", "same"))
        assertNull(LineDiff.words("completely different text here", "nothing in common at all"))
        assertNull(LineDiff.words("x".repeat(2000), "y".repeat(2000)))
        assertNull(LineDiff.words("", "something"))
        assertNull(LineDiff.words((1..600).joinToString(" ") { "w" }, (1..600).joinToString(" ") { "v" }))
    }

    @Test fun anIndentChangeMarksTheWhitespace() {
        val (old, new) = LineDiff.words("    return x", "        return x")!!
        assertEquals(listOf("    "), cut("    return x", old))
        assertEquals(listOf("        "), cut("        return x", new))
    }

    @Test fun rangesNeverSplitACharacter() {
        val family = "\uD83D\uDC68\u200D\uD83D\uDC69\u200D\uD83D\uDC67"
        assertEquals(listOf(family, "x"), LineDiff.tokens(family + "x"))
        val flag = "\uD83C\uDDE8\uD83C\uDDF3"
        assertEquals(listOf(flag, flag), LineDiff.tokens(flag + flag))
        val accented = "e\u0301"
        assertEquals(listOf(accented + "t"), LineDiff.tokens(accented + "t"))
        assertEquals(listOf("foo_bar", "  ", "=", "1"), LineDiff.tokens("foo_bar  =1"))
        assertEquals(listOf("你", "好", " ", "world"), LineDiff.tokens("你好 world"))
        assertEquals(listOf("a", "\r\n", "b"), LineDiff.tokens("a\r\nb"))
        assertEquals(emptyList<String>(), LineDiff.tokens(""))
    }

    @Test fun piecesCutTheTextWhereTheRangesBeginAndEnd() {
        assertEquals(listOf(DiffPiece("abc", false)), LineDiff.pieces("abc", emptyList()))
        assertEquals(
            listOf(DiffPiece("ab", false), DiffPiece("cd", true), DiffPiece("e", false), DiffPiece("f", true)),
            LineDiff.pieces("abcdef", listOf(WordSpan(2, 4), WordSpan(5, 6))),
        )
        // out of order, empty and past the end are left out
        assertEquals(
            listOf(DiffPiece("a", true), DiffPiece("bc", false)),
            LineDiff.pieces("abc", listOf(WordSpan(0, 1), WordSpan(0, 2), WordSpan(2, 2), WordSpan(2, 9))),
        )
    }

    @Test fun theShortestScriptKeepsWhatBothHave() {
        val (gone, came) = LineDiff.myers(intArrayOf(1, 2, 3), intArrayOf(1, 3))
        assertEquals(listOf(false, true, false), gone.toList())
        assertEquals(listOf(false, false), came.toList())
        val (allGone, allCame) = LineDiff.myers(intArrayOf(1, 2), intArrayOf(3, 4, 5))
        assertEquals(listOf(true, true), allGone.toList())
        assertEquals(listOf(true, true, true), allCame.toList())
        val (none, same) = LineDiff.myers(intArrayOf(), intArrayOf())
        assertEquals(0, none.size + same.size)
        val (emptyGone, emptyCame) = LineDiff.myers(intArrayOf(), intArrayOf(7))
        assertEquals(0, emptyGone.size)
        assertEquals(listOf(true), emptyCame.toList())
    }

    @Test fun editsAtBothEndsOfALongFileAreComparedWithinTheBudget() {
        fun file(edge: String, middle: Int) = "$edge head\n" + (1..middle).joinToString("\n") { "stable $it" } + "\n$edge tail"
        val d = diff(file("old", 300), file("new", 300))
        assertFalse(d.capped)
        assertEquals(2, d.adds)
        assertEquals(2, d.dels)
        assertNotNull(d.shown().firstOrNull { it is Shown.Fold })
        // past the budget the lines in between come back as all removed, then all added
        assertTrue(diff(file("old", 800), file("new", 800)).capped)
    }

    private fun assertFalse(value: Boolean) = assertTrue(!value)
}
