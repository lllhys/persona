package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.ScrollState
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
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
 * A quick filter's toned count (#9053, the Inbox's section bar): a badge in its state's colours that hides at 0 and caps
 * at 99+, with the exact number in the spoken name; a bay without a tone keeps its quiet digits and its old semantics,
 * so the Agents list's filters draw as before. A pinned first bay stays whole in sight from every choice, and a board
 * that scrolls sideways marks its chosen bay. Every style, light and dark; PNGs in build/quick-filter-count-tone-shots/.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h400dp-xhdpi")
class QuickFilterCountToneTest {
    @get:Rule val rule = createComposeRule()

    private val toned = listOf(
        Bay("Needs you", 120, Status.NeedsYou, countTone = Status.NeedsYou),
        Bay("FYI", 0, countTone = Status.Idle),
        Bay("In progress", 4, countTone = Status.Idle),
        Bay("All"),
    )

    private fun texts(text: String) = rule.onAllNodes(hasText(text), useUnmergedTree = true).fetchSemanticsNodes()
    private fun spoken(text: String) = rule.onAllNodes(hasContentDescription(text)).fetchSemanticsNodes()

    @Test fun aTonedCountHidesAtZeroCapsAndIsSpokenWithItsName() {
        var style by mutableStateOf(AppStyle.Glass)
        rule.setContent {
            PersonaTheme(style = style, reduceMotion = true) { QuickFilterBar(toned, 0, onSelect = {}) }
        }
        for (s in AppStyle.entries) {
            style = s
            rule.waitForIdle()
            assertTrue("${s.key}: 99+ past 99", texts("99+").isNotEmpty())
            assertTrue("${s.key}: never the raw number", texts("120").isEmpty())
            assertTrue("${s.key}: no badge at 0", texts("0").isEmpty())
            assertEquals("${s.key}: the exact number spoken", 1, spoken("Needs you, 120").size)
            assertEquals("${s.key}: the name alone at 0", 1, spoken("FYI").size)
            assertEquals("${s.key}: a neutral count spoken too", 1, spoken("In progress, 4").size)
        }
    }

    @Test fun aBayWithoutAToneKeepsItsDigitsAndSemantics() {
        var style by mutableStateOf(AppStyle.Glass)
        rule.setContent {
            PersonaTheme(style = style, reduceMotion = true) {
                QuickFilterBar(listOf(Bay("All", 0), Bay("Needs you", 120, Status.NeedsYou), Bay("Working", 3, Status.Working)), 0, onSelect = {})
            }
        }
        for (s in AppStyle.entries) {
            style = s
            rule.waitForIdle()
            // Brief's tabs read "Needs you 120" in one text.
            assertTrue("${s.key}: the plain count as it was",
                rule.onAllNodes(hasText("120", substring = true), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
            assertTrue("${s.key}: no cap without a tone", rule.onAllNodes(hasText("99+", substring = true), useUnmergedTree = true).fetchSemanticsNodes().isEmpty())
            val described = rule.onAllNodes(hasText("Needs you")).fetchSemanticsNodes()
                .filter { SemanticsProperties.ContentDescription in it.config }
            assertTrue("${s.key}: no spoken name added", described.isEmpty())
        }
    }

    private fun choice(label: String) = rule.onNode(hasText(label) and SemanticsMatcher.keyIsDefined(SemanticsProperties.Selected))

    @Test fun aPinnedFirstBayStaysWholeInSightWhenTheLastIsChosen() {
        var style by mutableStateOf(AppStyle.Glass)
        rule.setContent {
            PersonaTheme(style = style, reduceMotion = true) {
                Box(Modifier.width(360.dp).padding(Tokens.Space.l)) {
                    QuickFilterBar(listOf(Bay("Needs you", 31, Status.NeedsYou, countTone = Status.NeedsYou),
                        Bay("In progress", 6, countTone = Status.Idle), Bay("FYI", 40, countTone = Status.Idle), Bay("Done"), Bay("All")),
                        4, onSelect = {}, revealSelected = true, pinFirst = true)
                }
            }
        }
        for (s in AppStyle.entries) {
            style = s
            rule.waitForIdle()
            choice("All").assertIsDisplayed()
            val needs = choice("Needs you").fetchSemanticsNode()
            assertEquals("${s.key}: Needs you whole", needs.size.width.toFloat(), needs.boundsInRoot.width, 1f)
        }
    }

    /**
     * [JSegmented] drawn by itself, as another caller may: its toned counts and the scroll to a chosen segment hold in every
     * style, Glass's one sliding pill (#9632) included, where the segments sit in boxes the pill's layout places.
     */
    @Test fun aSegmentedRowKeepsItsTonesAndScrollsToItsChoiceInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var scroll: ScrollState? = null
        val segments = listOf(JSegment("Needs you", 120, countTone = Status.NeedsYou), JSegment("In progress", 4, countTone = Status.Idle),
            JSegment("FYI", 40, countTone = Status.Idle), JSegment("Done"), JSegment("All"))
        val width = 200.dp
        rule.setContent {
            PersonaTheme(style = style, reduceMotion = true) {
                // A new row per style, so each one scrolls from its start.
                key(style) {
                    val state = rememberScrollState().also { scroll = it }
                    Box(Modifier.width(width)) { JSegmented(segments, 4, onSelect = {}, modifier = Modifier.horizontalScroll(state), revealIn = state) }
                }
            }
        }
        val edge = with(rule.density) { width.toPx() }
        for (s in AppStyle.entries) {
            style = s
            rule.waitForIdle()
            assertTrue("${s.key}: 99+ past 99", texts("99+").isNotEmpty())
            assertEquals("${s.key}: the exact number spoken", 1, spoken("Needs you, 120").size)
            // Board's ruled strip shares out the width instead of running past it; the others scroll to the last segment.
            val state = scroll!!
            if (state.maxValue > 0) assertTrue("${s.key}: the row scrolled", state.value > 0)
            else assertTrue("${s.key}: only Board fits", s == AppStyle.Board)
            val all = choice("All").fetchSemanticsNode().boundsInRoot
            assertTrue("${s.key}: the chosen segment whole in sight (${all.left}..${all.right} of $edge)", all.left >= 0f && all.right <= edge + 1f)
        }
    }

    @Test fun aBoardMarksItsChosenBayAlsoWhenItScrolls() {
        var mark = Color.Unspecified
        var bays by mutableStateOf(emptyList<Bay>())
        var selected by mutableStateOf(1)
        var pin by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = AppStyle.Board, reduceMotion = true) {
                mark = PersonaTheme.palette.status(Status.NeedsYou).dot
                Box(Modifier.testTag("bar").width(400.dp)) { QuickFilterBar(bays, selected, onSelect = {}, pinFirst = pin) }
            }
        }
        val three = listOf(Bay("All", 9), Bay("Needs you", 3, Status.NeedsYou), Bay("Done", 4))
        val six = listOf(Bay("Needs you", 3, Status.NeedsYou), Bay("All", 9), Bay("Working", 2, Status.Working),
            Bay("Unread", 1, Status.Unread), Bay("Done", 4), Bay("Terminal", 0))
        fun marked(label: String): Boolean {
            rule.waitForIdle()
            val bar = rule.onNodeWithTag("bar").fetchSemanticsNode().boundsInRoot
            val cell = choice(label).fetchSemanticsNode().boundsInRoot
            val image = rule.onNodeWithTag("bar").captureToImage().asAndroidBitmap()
            // Inside the 3dp bar at the cell's foot, above the board's 1dp edge.
            return image.getPixel((cell.center.x - bar.left).toInt(), (cell.bottom - bar.top).toInt() - 4) == mark.toArgb()
        }
        bays = three; selected = 1
        assertTrue("a board that fits", marked("Needs you"))
        // Six bays run past 400dp: the board scrolls, and its cells have no width of their own to fill.
        bays = six; selected = 0
        assertTrue("a board that scrolls", marked("Needs you"))
        pin = true
        assertTrue("pinned", marked("Needs you"))
        bays = listOf(Bay("All", 9)) + six.take(1) + six.drop(2); selected = 1
        assertTrue("in the part that scrolls beside the pinned bay", marked("Needs you"))
    }

    @Test fun aBoardBayWithoutANumberShowsNoneAndSaysOnlyItsName() {
        rule.setContent {
            PersonaTheme(style = AppStyle.Board, reduceMotion = true) { QuickFilterBar(toned, 0, onSelect = {}) }
        }
        rule.waitForIdle()
        assertTrue("no dash for All or FYI's 0", texts("—").isEmpty())
        assertEquals(listOf("All"), choice("All").fetchSemanticsNode().config.getOrNull(SemanticsProperties.Text)?.map { it.text })
    }

    @Test fun drawsInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    QuickFilterBar(listOf(Bay("Needs you", 12, Status.NeedsYou, countTone = Status.NeedsYou),
                        Bay("In progress", 3, countTone = Status.Idle), Bay("FYI", 140, countTone = Status.Idle), Bay("Done"), Bay("All")), 0, onSelect = {})
                }
            }
        }
        val dir = File(System.getProperty("persona.shots") ?: "build/quick-filter-count-tone-shots").also { it.mkdirs() }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(dir, "count-tone-${s.key}-${if (d) "dark" else "light"}.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
