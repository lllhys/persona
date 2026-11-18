package persona.feature.settings

import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.ExtensionUiRoute
import java.io.File

/** The schema's own UI version 8 dashboard (schema/extension/examples/View.v8-today.json), as the hub sends it to an app of 8. */
internal fun v8Today(): ExtensionView = JsonCodec.decodeFromString(ExtensionView.serializer(),
    File(System.getProperty("persona.repo.root"), "schema/extension/examples/View.v8-today.json").readText())

/** UI version 8 (#9158): how many columns of cards a page has room for, rows of cards, rows side by side. */
class ExtensionColumnsRulesTest {
    @Test fun columnsFollowThePanesContentWidthAsTheCardGridDoes() {
        // floor((width + Space.m) / (cardColumn + Space.m)): 330 + 12 a column, one at least, three at most
        assertEquals(1, ExtensionColumns.count(0.dp))
        assertEquals(1, ExtensionColumns.count(Float.NaN.dp))
        assertEquals(1, ExtensionColumns.count(380.dp)) // a phone
        assertEquals(1, ExtensionColumns.count(671.dp))
        assertEquals(2, ExtensionColumns.count(672.dp))
        assertEquals(2, ExtensionColumns.count(1013.dp))
        assertEquals(3, ExtensionColumns.count(1014.dp))
        assertEquals(3, ExtensionColumns.count(2400.dp))
        // only a columns view is a grid, and only with room for two
        val today = v8Today()
        assertTrue(ExtensionColumns.grid(today, 2))
        assertFalse(ExtensionColumns.grid(today, 1))
        assertFalse(ExtensionColumns.grid(today.copy(layout = null), 3))
        assertFalse(ExtensionColumns.grid(today.copy(layout = "split"), 3))
        assertFalse(ExtensionColumns.grid(null, 3))
    }

    @Test fun cardsGoInReadingOrderAndAFullCardHasARowOfItsOwn() {
        fun s(full: Boolean = false) = ExtensionViewSection(span = if (full) "full" else null)
        val today = listOf(s(full = true), s(), s(), s(), s())
        assertEquals(listOf(listOf(0), listOf(1, 2), listOf(3, 4)), ExtensionColumns.rows(today, 2))
        assertEquals(listOf(listOf(0), listOf(1, 2, 3), listOf(4)), ExtensionColumns.rows(today, 3))
        // a full card in the middle ends the short row before it, which keeps its empty place
        assertEquals(listOf(listOf(0), listOf(1), listOf(2, 3)), ExtensionColumns.rows(listOf(s(), s(full = true), s(), s()), 2))
        // any other span is one column
        assertEquals(listOf(listOf(0, 1)), ExtensionColumns.rows(listOf(ExtensionViewSection(span = "half"), s()), 2))
        assertEquals(listOf(listOf(0), listOf(1)), ExtensionColumns.rows(listOf(s(), s()), 1))
        assertEquals(emptyList<List<Int>>(), ExtensionColumns.rows(emptyList(), 3))
        // the schema's dashboard: Needs me on top, then Calendar | Lark today
        assertEquals(listOf(listOf(0), listOf(1, 2)), ExtensionColumns.rows(v8Today().sections, 2))
    }

    @Test fun rowsGoSideBySideOnlyWhereEachKeepsACardColumn() {
        assertEquals(1, ExtensionColumns.perLine(null, 2000.dp))
        assertEquals(1, ExtensionColumns.perLine(1, 2000.dp))
        assertEquals(1, ExtensionColumns.perLine(2, 659.dp)) // a phone, and a card of a two-column grid
        assertEquals(2, ExtensionColumns.perLine(2, 660.dp))
        assertEquals(2, ExtensionColumns.perLine(2, 2000.dp))
        assertEquals(2, ExtensionColumns.perLine(3, 989.dp))
        assertEquals(3, ExtensionColumns.perLine(3, 990.dp))
        assertEquals(3, ExtensionColumns.perLine(7, 2000.dp)) // the schema allows 3; more is three
        assertEquals(1, ExtensionColumns.perLine(0, 2000.dp))
    }

    @Test fun anythingButARowTakesALineOfItsOwn() {
        fun row(key: String) = ExtensionViewItem("row", title = key, key = key)
        val text = ExtensionViewItem("text", text = "4 things wait")
        val link = ExtensionViewItem("link", title = "Docs", url = "https://example.com")
        val items = listOf(text, row("a"), row("b"), row("c"), link, row("d"))
        assertEquals(listOf(listOf(text), listOf(row("a"), row("b")), listOf(row("c")), listOf(link), listOf(row("d"))),
            ExtensionColumns.lines(items, 2))
        assertEquals(items.map { listOf(it) }, ExtensionColumns.lines(items, 1))
    }

    @Test fun aHeadNeedsAnIconOrALinkThatCanBePressed() {
        assertFalse(ExtensionColumns.head(ExtensionViewSection(title = "Plain")))
        assertTrue(ExtensionColumns.head(ExtensionViewSection(icon = "bell")))
        assertFalse(ExtensionColumns.head(ExtensionViewSection(icon = " ")))
        assertTrue(ExtensionColumns.hasLink(ExtensionViewSection(link = ExtensionViewSectionLink("All", ExtensionViewAction("all")))))
        assertTrue(ExtensionColumns.hasLink(ExtensionViewSection(link = ExtensionViewSectionLink("All", ExtensionViewAction(push = ExtensionViewPush("all"))))))
        assertFalse(ExtensionColumns.hasLink(ExtensionViewSection(link = ExtensionViewSectionLink("", ExtensionViewAction("all")))))
        assertFalse(ExtensionColumns.hasLink(ExtensionViewSection(link = ExtensionViewSectionLink("All", ExtensionViewAction()))))
        assertTrue(ExtensionColumns.sideBySide(ExtensionViewSection(columns = 2)))
        assertFalse(ExtensionColumns.sideBySide(ExtensionViewSection(columns = 1)))
        assertFalse(ExtensionColumns.sideBySide(ExtensionViewSection()))
    }

    @Test fun theSchemasDashboardReadsIntoTheAppsTypes() {
        val today = v8Today()
        assertEquals("columns", today.layout)
        val needs = today.sections[0]
        assertEquals("full", needs.span)
        assertEquals("bell", needs.icon)
        assertEquals("orange", needs.iconColor)
        assertEquals(2, needs.columns)
        assertEquals("All", needs.link!!.title)
        assertEquals(ExtensionViewPush("needs", "Needs me"), needs.link!!.action.push)
        assertEquals("open_calendar", today.sections[1].link!!.action.id)
        assertNull(today.sections[1].span)
    }
}

private class TodayUi(var view: ExtensionView = v8Today()) : ExtensionApiStub() {
    val actions = mutableListOf<String>()
    override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?): ExtensionView = view
    override suspend fun uiAction(extension: String, ui: String, action: String, context: ExtensionUiContext?, state: String?,
        values: Map<String, String>?): ExtensionUiResult { actions += action; return ExtensionUiResult(toast = "ok") }
}

/** A dashboard page as an extension's page draws it, at a phone's, a foldable's and a tablet's width. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ExtensionColumnsPageTest {
    @get:Rule val rule = createComposeRule()
    private val api = TodayUi()
    private val pushed = mutableListOf<ExtensionUiRoute>()

    private fun show() {
        rule.setContent { PersonaTheme(reduceMotion = true) { ExtensionUiPage(api, "today", "today", "Today", {}, onPush = { pushed += it }) } }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("Weekly sync").fetchSemanticsNodes().isNotEmpty() }
    }
    /** The first node with [text] (a section's title comes before its link, which may have the same words). */
    private fun bounds(text: String): Rect = rule.onAllNodesWithText(text)[0].fetchSemanticsNode().boundsInRoot
    private fun cards(): List<Rect> = rule.onAllNodesWithTag(ExtensionCardSurfaceTag).fetchSemanticsNodes().map { it.boundsInRoot }
        .sortedWith(compareBy({ it.top }, { it.left }))

    @Test @Config(qualifiers = "en-w400dp-h900dp-xhdpi")
    fun aPhoneIsOneColumnAsAStackWithTheHeadsIconsAndLinks() {
        show()
        assertTrue("a phone draws no grid", cards().isEmpty())
        // reading order, one under another: Needs me, Calendar, Lark today
        assertTrue(bounds("Needs me").bottom <= bounds("Calendar").top)
        assertTrue(bounds("Weekly sync").bottom <= bounds("Lark today").top)
        // Needs me asks for its rows two a line: a phone has no room, so one under another
        assertTrue(bounds("Approve the travel request").bottom <= bounds("Reply about the design review").top)
        linksAreButtonsOfTheirOwnWords()
    }

    @Test @Config(qualifiers = "en-w1280dp-h800dp-xhdpi")
    fun aTabletHasThreeColumnsAFullCardAndRowsAligned() {
        show()
        val cards = cards()
        assertEquals("Needs me, then Calendar | Lark today", 3, cards.size)
        val (needs, calendar, lark) = cards
        // three columns: a card is a third of the row less the gaps; Needs me spans the row
        assertTrue(needs.width > 2.9f * calendar.width)
        assertEquals(calendar.width, lark.width, 1f)
        assertTrue(lark.left > calendar.right)
        // rows aligned: Calendar has a row more than Lark today, and both cards are as tall as the taller
        assertEquals(calendar.top, lark.top, 1f)
        assertEquals(calendar.height, lark.height, 1f)
        assertTrue(bounds("Release").bottom < lark.bottom - 40f)
        // the heads are inside their cards
        assertTrue(needs.contains(bounds("Needs me").center))
        assertTrue(rule.onAllNodesWithText("Calendar").fetchSemanticsNodes().all { calendar.contains(it.boundsInRoot.center) })
        // Needs me's rows two a line in the full card, in reading order
        val first = bounds("Approve the travel request")
        val second = bounds("Reply about the design review")
        assertEquals(first.top, second.top, 1f)
        assertTrue(second.left >= first.right - 1f) // each row a cell of its own, side by side
        assertTrue(bounds("Agent waits for a permission").top >= first.bottom)
        linksAreButtonsOfTheirOwnWords()
    }

    @Test @Config(qualifiers = "en-w800dp-h1280dp-xhdpi")
    fun aFoldableOrPortraitTabletHasTwoColumns() {
        show()
        val (needs, calendar, lark) = cards()
        assertEquals(calendar.width, lark.width, 1f)
        assertTrue(needs.width > 1.9f * calendar.width && needs.width < 2.2f * calendar.width)
        assertEquals(calendar.top, lark.top, 1f)
    }

    /** "All" pushes its page without asking the extension; "Calendar" goes to the extension as a row's action does. */
    private fun linksAreButtonsOfTheirOwnWords() {
        val all = rule.onNode(hasContentDescription("All") and hasClickAction())
        val node = all.fetchSemanticsNode()
        assertEquals(androidx.compose.ui.semantics.Role.Button, node.config.getOrNull(SemanticsProperties.Role))
        // 48 dp to touch (the button's own height may be less)
        val density = rule.density.density
        assertTrue("touch ${node.touchBoundsInRoot}", node.touchBoundsInRoot.height >= 48 * density - 1 && node.touchBoundsInRoot.width >= 48 * density - 1)
        assertNotNull(node.config.getOrNull(SemanticsActions.OnClick))
        all.performClick()
        rule.waitForIdle()
        assertEquals(listOf(ExtensionUiRoute("today", "today", "Needs me", "needs", 1)), pushed)
        assertTrue("a push is not sent", api.actions.isEmpty())
        rule.onNode(hasContentDescription("Calendar") and hasClickAction()).performClick()
        rule.waitUntil(5_000) { api.actions.isNotEmpty() }
        assertEquals(listOf("open_calendar"), api.actions)
    }
}
