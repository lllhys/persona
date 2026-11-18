package persona.core.domain.diff

/** Characters [start, end) of a line's text, in UTF-16 units. */
data class WordSpan(val start: Int, val end: Int) {
    val isEmpty: Boolean get() = end <= start
}

/** A run of a line's text, changed or not ([LineDiff.pieces]). */
data class DiffPiece(val text: String, val changed: Boolean)

/** What to show of a diff's lines: one by its index, or in place of a long run of context, the [count] lines left out. */
sealed interface Shown {
    data class Line(val index: Int) : Shown
    data class Fold(val from: Int, val count: Int) : Shown
}

/**
 * An edit's text before and after, compared line by line: the lines that stayed as context, the ones taken out and put
 * in, and inside a line changed rather than rewritten, the words that changed. Claude's Edit, MultiEdit and Write carry
 * the old and new text, not a patch; drawn as all of the old removed and all of the new added, a one-word change in a
 * 30-line edit would read as 60 changed lines. iOS compares the same texts the same way (`LineDiff.swift`).
 *
 * Lines are compared with a Myers diff after the lines both texts start and end with are set aside. Past
 * [maxChangedLines] lines in between (or [maxChars] on either side) the lines in between come back as all removed,
 * then all added ([capped]), and no words are marked.
 */
class LineDiff(
    old: String,
    new: String,
    maxChars: Int = MaxChars,
    maxChangedLines: Int = MaxChangedLines,
    maxWordWork: Int = MaxWordWork,
) {
    enum class Kind { Context, Add, Del }

    class Line(
        val kind: Kind,
        val text: String,
        /** The line's number in the old text (1-based), for context and removed lines. */
        val oldLine: Int? = null,
        /** The line's number in the new text (1-based), for context and added lines. */
        val newLine: Int? = null,
        /** The words that changed, in order and apart; empty when the line is not paired with one it was changed from or to. */
        val changed: List<WordSpan> = emptyList(),
    )

    val lines: List<Line>
    val adds: Int
    val dels: Int

    /** Too big to compare: the lines between the common start and end are removed, then added, and no words are marked. */
    val capped: Boolean

    init {
        val (a, b) = split(old, new)
        val m = compare(a, b, old, new, maxChars, maxChangedLines)
        // Both walked together: within a change the removed lines come first, then the added ones.
        val rows = ArrayList<Line>(a.size + b.size)
        var i = 0
        var j = 0
        while (i < a.size || j < b.size) {
            if (i < a.size && (m.removed[i] || j == b.size)) rows += Line(Kind.Del, a[i], oldLine = ++i)
            else if (j < b.size && (m.inserted[j] || i == a.size)) rows += Line(Kind.Add, b[j], newLine = ++j)
            else { rows += Line(Kind.Context, b[j], oldLine = i + 1, newLine = j + 1); i++; j++ }
        }
        lines = if (m.capped) rows else markWords(rows, maxWordWork)
        adds = lines.count { it.kind == Kind.Add }
        dels = lines.count { it.kind == Kind.Del }
        capped = m.capped
    }

    /** The changes with [context] lines around each; a longer run of unchanged lines folds into one row. */
    fun shown(context: Int = ContextLines): List<Shown> = foldUnchanged(lines.map { it.kind != Kind.Context }, context)

    companion object {
        /** Lines of both texts compared at most, once the lines they start and end with are set aside. */
        const val MaxChangedLines = 1_000

        /** UTF-16 units of either text compared at most. */
        const val MaxChars = 100_000

        /** Unchanged lines kept on each side of a change when the rest folds. */
        const val ContextLines = 3

        /** Lines longer than this (UTF-16 units), or with more words, are not compared word by word: too slow, too noisy. */
        const val MaxLineChars = 1_000
        const val MaxLineWords = 500

        /**
         * Word comparisons at most over the whole diff, each pair of lines counted as one's words times the other's, so a
         * long run of long changed lines stays quick.
         */
        const val MaxWordWork = 1_000_000

        /** Under this share of characters in common (of both lines'), two lines read as rewritten: the row's colour says enough. */
        private const val AlikeLeast = 0.4

        /** Unless one is nearly all in the other: words put in or taken out. */
        private const val ContainedLeast = 0.7

        /**
         * Both texts' lines. A newline at the very end of both ends their last lines rather than starting an empty one,
         * and so does a newline at the end of either when the other is empty (a Write's content, nearly always). An Edit's
         * texts are pieces of a file: a newline at the end of only one of them puts in or takes out a line break, and the
         * empty line after it stays to show that.
         */
        internal fun split(old: String, new: String): Pair<List<String>, List<String>> {
            val oldEnds = old.endsWith('\n')
            val newEnds = new.endsWith('\n')
            val ends = (oldEnds && newEnds) || old.isEmpty() || new.isEmpty()
            fun lines(text: String, endsWithNewline: Boolean): List<String> {
                if (text.isEmpty()) return emptyList()
                val all = text.split('\n')
                return if (ends && endsWithNewline) all.dropLast(1) else all
            }
            return lines(old, oldEnds) to lines(new, newEnds)
        }

        private class Marks(val removed: BooleanArray, val inserted: BooleanArray, val capped: Boolean)

        /** Which lines of [a] were removed and which of [b] inserted; capped past the budgets. */
        private fun compare(a: List<String>, b: List<String>, old: String, new: String, maxChars: Int, maxChangedLines: Int): Marks {
            val removed = BooleanArray(a.size)
            val inserted = BooleanArray(b.size)
            if (old.length > maxChars || new.length > maxChars) {
                removed.fill(true)
                inserted.fill(true)
                return Marks(removed, inserted, true)
            }
            // Equal lines share a number, so the diff compares integers.
            val ids = HashMap<String, Int>()
            val x = IntArray(a.size) { ids.getOrPut(a[it]) { ids.size } }
            val y = IntArray(b.size) { ids.getOrPut(b[it]) { ids.size } }
            var head = 0
            while (head < x.size && head < y.size && x[head] == y[head]) head++
            var tail = 0
            while (tail < x.size - head && tail < y.size - head && x[x.size - 1 - tail] == y[y.size - 1 - tail]) tail++
            val midX = x.copyOfRange(head, x.size - tail)
            val midY = y.copyOfRange(head, y.size - tail)
            if (midX.isEmpty() || midY.isEmpty() || midX.size + midY.size > maxChangedLines) {
                for (k in midX.indices) removed[head + k] = true
                for (k in midY.indices) inserted[head + k] = true
                return Marks(removed, inserted, midX.isNotEmpty() && midY.isNotEmpty())
            }
            val (gone, came) = myers(midX, midY)
            for (k in gone.indices) if (gone[k]) removed[head + k] = true
            for (k in came.indices) if (came[k]) inserted[head + k] = true
            return Marks(removed, inserted, false)
        }

        /** Which elements of [a] a shortest edit script removes, and which of [b] it inserts (Myers, O(ND) steps). */
        internal fun myers(a: IntArray, b: IntArray): Pair<BooleanArray, BooleanArray> {
            val gone = BooleanArray(a.size)
            val came = BooleanArray(b.size)
            val n = a.size
            val m = b.size
            val max = n + m
            if (max == 0) return gone to came
            val offset = max + 1
            val v = IntArray(2 * max + 3)
            // The frontier as each round began, to walk the script back from the end.
            val trace = ArrayList<IntArray>()
            search@ for (d in 0..max) {
                trace += v.copyOf()
                for (k in -d..d step 2) {
                    var x = if (k == -d || (k != d && v[offset + k - 1] < v[offset + k + 1])) v[offset + k + 1] else v[offset + k - 1] + 1
                    var y = x - k
                    while (x < n && y < m && a[x] == b[y]) { x++; y++ }
                    v[offset + k] = x
                    if (x >= n && y >= m) break@search
                }
            }
            var x = n
            var y = m
            for (d in trace.size - 1 downTo 0) {
                if (d == 0) break
                val frontier = trace[d]
                val k = x - y
                val prevK = if (k == -d || (k != d && frontier[offset + k - 1] < frontier[offset + k + 1])) k + 1 else k - 1
                val prevX = frontier[offset + prevK]
                val prevY = prevX - prevK
                while (x > prevX && y > prevY) { x--; y-- }
                if (x == prevX) came[prevY] = true else gone[prevX] = true
                x = prevX
                y = prevY
            }
            return gone to came
        }

        /** In each change, the first removed line is paired with the first added one, the second with the second…, each pair compared word by word. */
        private fun markWords(rows: List<Line>, budget: Int): List<Line> {
            var left = budget
            val out = rows.toMutableList()
            var k = 0
            while (k < out.size && left > 0) {
                if (out[k].kind != Kind.Del) { k++; continue }
                val dels = ArrayList<Int>()
                val adds = ArrayList<Int>()
                while (k < out.size && out[k].kind == Kind.Del) dels += k++
                while (k < out.size && out[k].kind == Kind.Add) adds += k++
                for (p in 0 until minOf(dels.size, adds.size)) {
                    if (left <= 0) break
                    val d = out[dels[p]]
                    val a = out[adds[p]]
                    val result = words(d.text, a.text) { left -= it } ?: continue
                    out[dels[p]] = Line(d.kind, d.text, d.oldLine, d.newLine, result.first)
                    out[adds[p]] = Line(a.kind, a.text, a.oldLine, a.newLine, result.second)
                }
            }
            return out
        }

        /**
         * The ranges that changed between an old line and the new one it became, word by word; null when the lines are
         * too long or too unlike for it to help. Whitespace between two changed words joins them into one range.
         * [spend] is told the work done, so a diff can keep a budget.
         */
        fun words(a: String, b: String, spend: (Int) -> Unit = {}): Pair<List<WordSpan>, List<WordSpan>>? {
            if (a == b) return emptyList<WordSpan>() to emptyList()
            val la = a.length
            val lb = b.length
            // A line against an empty one is all changed: the row's colour says so.
            if (la == 0 || lb == 0 || la > MaxLineChars || lb > MaxLineChars) return null
            val x = tokens(a)
            val y = tokens(b)
            if (x.size > MaxLineWords || y.size > MaxLineWords) return null
            fun alike(common: Int) = 2.0 * common / (la + lb) >= AlikeLeast || common.toDouble() / minOf(la, lb) >= ContainedLeast
            // At most the words both have, counted once each: when even that is too little, the diff is not needed.
            val count = HashMap<String, Int>()
            for (t in x) count[t] = (count[t] ?: 0) + 1
            var most = 0
            for (t in y) {
                val left = count[t] ?: 0
                if (left > 0) { count[t] = left - 1; most += t.length }
            }
            if (!alike(most)) return null
            spend(x.size * y.size)
            val ids = HashMap<String, Int>()
            val (gone, came) = myers(IntArray(x.size) { ids.getOrPut(x[it]) { ids.size } }, IntArray(y.size) { ids.getOrPut(y[it]) { ids.size } })
            val common = x.indices.sumOf { if (gone[it]) 0 else x[it].length }
            if (!alike(common)) return null
            return spans(x, gone) to spans(y, came)
        }

        private fun spans(words: List<String>, dropped: BooleanArray): List<WordSpan> {
            val out = ArrayList<WordSpan>()
            var at = 0
            for ((n, w) in words.withIndex()) {
                val last = out.lastOrNull()
                if (dropped[n]) {
                    if (last != null && last.end == at) out[out.lastIndex] = WordSpan(last.start, at + w.length)
                    else out += WordSpan(at, at + w.length)
                } else if (last != null && last.end == at && w.all { it.isWhitespace() } && n + 1 < words.size && dropped[n + 1]) {
                    // A space between two changed words: one range.
                    out[out.lastIndex] = WordSpan(last.start, at + w.length)
                }
                at += w.length
            }
            return out
        }

        private enum class TokenClass { Word, Space, Alone }

        /**
         * A line as words: runs of letters, digits and _, runs of whitespace, and every other character on its own. Han
         * characters and kana are words of their own too: Chinese and Japanese put no spaces between words, and a whole
         * sentence as one word would never match. Cut between characters (a base with its marks, a joined emoji, a pair of
         * regional indicators count as one), so a range never splits one.
         */
        internal fun tokens(s: String): List<String> {
            val out = ArrayList<String>()
            var start = 0
            var previous: TokenClass? = null
            var i = 0
            while (i < s.length) {
                val end = clusterEnd(s, i)
                val c = tokenClass(s.codePointAt(i))
                if (i > start && (c == TokenClass.Alone || c != previous)) {
                    out += s.substring(start, i)
                    start = i
                }
                previous = c
                i = end
            }
            if (start < s.length) out += s.substring(start)
            return out
        }

        /** The end of the character that starts at [from]: the code point and what is glued to it. */
        private fun clusterEnd(s: String, from: Int): Int {
            var i = from + Character.charCount(s.codePointAt(from))
            val first = s.codePointAt(from)
            if (first == '\r'.code && i < s.length && s[i] == '\n') return i + 1
            var regional = isRegional(first)
            while (i < s.length) {
                val cp = s.codePointAt(i)
                val width = Character.charCount(cp)
                when {
                    cp == ZeroWidthJoiner -> {
                        // joins the next code point too
                        i += width
                        if (i < s.length) i += Character.charCount(s.codePointAt(i))
                    }
                    isMark(cp) -> i += width
                    regional && isRegional(cp) -> { i += width; regional = false }
                    else -> return i
                }
            }
            return i
        }

        private fun isRegional(cp: Int) = cp in 0x1F1E6..0x1F1FF

        private fun isMark(cp: Int): Boolean = when (Character.getType(cp).toByte()) {
            Character.NON_SPACING_MARK, Character.ENCLOSING_MARK, Character.COMBINING_SPACING_MARK -> true
            else -> cp in 0xFE00..0xFE0F || cp in 0x1F3FB..0x1F3FF
        }

        private const val ZeroWidthJoiner = 0x200D

        private fun tokenClass(cp: Int): TokenClass = when {
            Character.isWhitespace(cp) || Character.isSpaceChar(cp) -> TokenClass.Space
            Character.isIdeographic(cp) || isKana(cp) -> TokenClass.Alone
            Character.isLetterOrDigit(cp) || cp == '_'.code -> TokenClass.Word
            else -> TokenClass.Alone
        }

        private fun isKana(cp: Int) = cp in 0x3040..0x30FF || cp in 0x31F0..0x31FF || cp in 0xFF66..0xFF9F

        /** [text] cut where its [changed] ranges begin and end; ranges out of order, empty or past the end are left out. */
        fun pieces(text: String, changed: List<WordSpan>): List<DiffPiece> {
            if (changed.isEmpty()) return listOf(DiffPiece(text, false))
            val out = ArrayList<DiffPiece>()
            var at = 0
            for (r in changed) {
                if (r.isEmpty || r.start < at || r.end > text.length) continue
                if (r.start > at) out += DiffPiece(text.substring(at, r.start), false)
                out += DiffPiece(text.substring(r.start, r.end), true)
                at = r.end
            }
            if (at < text.length) out += DiffPiece(text.substring(at), false)
            return out
        }
    }
}

/**
 * What to show of [isChange] (one flag per line, true for an added or removed one): each line by its index, or in
 * place of a run of more than one unchanged line beyond [context] lines of any change, one fold. A diff with no change
 * is shown whole.
 */
fun foldUnchanged(isChange: List<Boolean>, context: Int = LineDiff.ContextLines): List<Shown> {
    if (isChange.none { it }) return isChange.indices.map { Shown.Line(it) }
    val near = BooleanArray(isChange.size)
    for ((k, changed) in isChange.withIndex()) if (changed) {
        for (n in maxOf(0, k - context)..minOf(isChange.size - 1, k + context)) near[n] = true
    }
    val out = ArrayList<Shown>()
    var k = 0
    while (k < isChange.size) {
        if (near[k]) { out += Shown.Line(k); k++; continue }
        var end = k
        while (end < isChange.size && !near[end]) end++
        // One line folded into a row of its own saves nothing.
        if (end - k > 1) out += Shown.Fold(k, end - k) else out += Shown.Line(k)
        k = end
    }
    return out
}
