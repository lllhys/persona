package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/**
 * A quick filter bar's cut edge (#9053, the Inbox's section bar): with `fadeCut` the part that scrolls fades to nothing at
 * an edge with bays past it, the trailing one at the start and the leading one once it has scrolled to its end; a bar
 * that fits draws exactly as without it, and a bar without it (the Agents list's) draws its bays up to a hard edge.
 * Every style, light and dark, pixel for pixel against the same bar without the option; PNGs in build/quick-filter-fade-shots/.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w800dp-h400dp-xhdpi")
class QuickFilterFadeTest {
    @get:Rule val rule = createComposeRule()

    private val bays = listOf(
        Bay("Needs you", 31, Status.NeedsYou, countTone = Status.NeedsYou), Bay("In progress", 6, countTone = Status.Idle),
        Bay("FYI", 40, countTone = Status.Idle), Bay("Done"), Bay("All"),
    )

    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)
    private var width by mutableIntStateOf(700)
    private var selected by mutableIntStateOf(0)
    private var fadeCut by mutableStateOf(true)
    private var background = 0

    // One bar in one place, drawn with the option and without: a shadow's look depends on where on the screen it is.
    private fun show() {
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                background = PersonaTheme.palette.background.toArgb()
                Box(Modifier.testTag("bar").width(width.dp).background(PersonaTheme.palette.background)) {
                    // Each style and theme starts from a strip at its start (a board that fits keeps no scroll of its own).
                    key(style, dark) { QuickFilterBar(bays, selected, onSelect = {}, revealSelected = true, pinFirst = true, fadeCut = fadeCut) }
                }
            }
        }
    }

    /** The bar with the option ("faded") or without it ("plain"). */
    private fun image(tag: String): Bitmap {
        fadeCut = tag == "faded"
        rule.waitForIdle()
        return rule.onNodeWithTag("bar").captureToImage().asAndroidBitmap()
    }

    /** Whether two colours are the same to the eye: no channel more than [NEAR] apart. */
    private fun near(a: Int, b: Int) = listOf(16, 8, 0).all { shift -> kotlin.math.abs((a shr shift and 0xFF) - (b shr shift and 0xFF)) <= NEAR }

    /**
     * The columns where [a] and [b] differ to the eye. Exact where nothing fades; with a fade on, the strip is drawn in a
     * layer of its own, which moves a chip's shadow by a step or two of a channel.
     */
    private fun differing(a: Bitmap, b: Bitmap, exact: Boolean = false): List<Int> = (0 until a.width).filter { x ->
        (0 until a.height).any { y -> if (exact) a.getPixel(x, y) != b.getPixel(x, y) else !near(a.getPixel(x, y), b.getPixel(x, y)) }
    }

    /** Whether the column at [x] holds nothing but the page's background, give or take what rounding leaves of a fade. */
    private fun bare(image: Bitmap, x: Int) = (0 until image.height).all { y -> near(image.getPixel(x, y), background) }

    /** Whether a choice of the bar runs past its trailing edge with at least [shown] px of it in sight. */
    private fun cutAtTheEdge(shown: Int): Boolean {
        val bar = rule.onNodeWithTag("bar").fetchSemanticsNode()
        val top = bar.positionInRoot.y
        val right = bar.positionInRoot.x + bar.size.width
        return rule.onAllNodes(SemanticsMatcher.keyIsDefined(SemanticsProperties.Selected)).fetchSemanticsNodes()
            .filter { it.positionInRoot.y >= top && it.positionInRoot.y < top + bar.size.height }
            .any { it.positionInRoot.x <= right - shown && it.positionInRoot.x + it.size.width > right }
    }

    private fun save(name: String, image: Bitmap) {
        val dir = File(System.getProperty("persona.shots") ?: "build/quick-filter-fade-shots").also { it.mkdirs() }
        File(dir, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private companion object {
        const val NEAR = 8
    }

    @Test fun fadesOnlyTheEdgeThatCutsBaysOff() {
        show()
        val fadePx = with(rule.density) { AgentFilterTokens.fade.toPx() }.toInt()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            val key = "${s.key}-${if (d) "dark" else "light"}"
            // Room for every bay: nothing is cut, so nothing fades.
            width = 700; selected = 0
            assertTrue("$key: a bar that fits draws as without the option", differing(image("faded"), image("plain"), exact = true).isEmpty())
            // Too narrow: the start shows, the rest runs past the trailing edge.
            width = 300
            var faded = image("faded")
            var plain = image("plain")
            save("fade-$key-start", faded)
            var columns = differing(faded, plain)
            assertTrue("$key: the trailing edge fades", columns.isNotEmpty())
            // Brief also spaces its tabs so part of the next one shows under the fade; the others move nothing.
            if (s == AppStyle.Brief) assertTrue("$key: a tab is cut at the edge, part of it in sight", image("faded").let { cutAtTheEdge(fadePx / 2) })
            else assertTrue("$key: only the trailing edge (${columns.first()}..${columns.last()})", columns.first() >= faded.width - fadePx - 1)
            // To nothing at the very edge (the board's frame and border stay, drawn around the strip).
            if (s != AppStyle.Board) assertTrue("$key: to nothing at the edge", bare(faded, faded.width - 1))
            // All chosen: the strip scrolls to its end, and the leading edge fades where In progress is cut.
            selected = 4
            faded = image("faded")
            plain = image("plain")
            save("fade-$key-end", faded)
            columns = differing(faded, plain)
            // The board's cut cell can show only its empty end there (its words sit at its start), so nothing to fade.
            if (s != AppStyle.Board) assertTrue("$key: the leading edge fades", columns.isNotEmpty())
            // Where the part that scrolls begins, beside the pinned bay, and no wider than the fade.
            if (columns.isNotEmpty()) assertTrue("$key: only the leading edge (${columns.first()}..${columns.last()})",
                columns.first() < faded.width / 2 && columns.last() - columns.first() <= fadePx + 1)
        }
    }

    /** Brief's tabs under a fade ([peekGap]): the next tab past the edge shows at least the fade's width, or the gaps stay. */
    @Test fun briefKeepsPartOfTheNextTabInSight() {
        // They fit: the usual gap.
        assertEquals(44, peekGap(listOf(100, 100), viewport = 400, gap = 44, peek = 64, least = 16))
        // The edge cuts the third tab with more than the fade's width of it in sight: nothing changes.
        assertEquals(44, peekGap(listOf(100, 100, 100), viewport = 360, gap = 44, peek = 64, least = 16))
        // Less of it in sight: the two gaps before it narrow until the fade's width of it shows.
        assertEquals(38, peekGap(listOf(120, 100, 100), viewport = 360, gap = 44, peek = 64, least = 16))
        assertEquals(18, peekGap(listOf(100, 100, 100), viewport = 300, gap = 44, peek = 64, least = 16))
        // The edge in the gap before it, and narrowing would go under the compact gap: they stay, the fade alone shows the cut.
        assertEquals(44, peekGap(listOf(100, 100, 100), viewport = 280, gap = 44, peek = 64, least = 16))
        assertEquals(44, peekGap(listOf(160, 160, 100), viewport = 380, gap = 44, peek = 64, least = 30))
    }
}
