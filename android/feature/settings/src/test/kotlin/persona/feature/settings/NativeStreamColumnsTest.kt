package persona.feature.settings

import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.generated.*
import persona.core.contracts.stream.*
import persona.core.designsystem.*

/**
 * UI version 8 (#9158) in a native stream: the stream sends 8 and draws what pages draw, a section's link is pressed as
 * the section's own node, its icon colour is in the renderer's profile, and a diff's place in a grid is found.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class NativeStreamColumnsTest {
    @get:Rule val rule = createComposeRule()
    private val identity = NativeStreamIdentity("owner", "group", "example", "today", "navigation")
    private val requests = mutableListOf<ExtensionUIStreamRequest>()
    private lateinit var session: NativeStreamSession

    private val needs = """{"key":"needs","title":"Needs me","span":"full","icon":"bell","icon_color":"orange","columns":2,
        "link":{"title":"All","action":{"id":"open_needs"}},"items":[
        {"type":"row","key":"need-1","title":"Approve the travel request","action":{"id":"open_need"}},
        {"type":"row","key":"need-2","title":"Reply about the design review","action":{"id":"open_need"}}]}"""
    private val calendar = """{"key":"calendar","title":"Calendar","icon":"calendar","icon_color":"red",
        "link":{"title":"Calendar","action":{"id":"open_calendar","values":{"range":"day"}}},"items":[
        {"type":"row","key":"cal-1","title":"Weekly sync","value":"14:00"}]}"""
    private val lark = """{"key":"lark","title":"Lark today","icon":"chat","items":[{"type":"row","key":"lark-1","title":"Design team"}]}"""
    private fun snapshot(sections: String, layout: String = "columns") =
        """{"protocol":1,"session":"session","epoch":"epoch","version":1,"root_key":"root","view":{"title":"Today","layout":"$layout","sections":[$sections]}}"""

    private fun show(payload: String) {
        session = NativeStreamSession(identity, ExtensionUIContext(platform = "android", locale = "en", uiVersion = 8,
            stream = ExtensionUIStreamCapability(protocol = 1, components = NativeExtensionStreamAdapter.Components.toList())),
            NativeStreamTransport { request ->
                requests.add(request)
                if (request is ExtensionUIStreamOpenRequest) session.snapshot(identity, request.requestId, payload)
            })
        val adapter = NativeExtensionStreamAdapter(session, true)
        rule.setContent { PersonaTheme(reduceMotion = true) { ExtensionUiPage(null, "example", "today", "Today", {}, availableAdapter = adapter) } }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("Weekly sync").fetchSemanticsNodes().isNotEmpty() }
    }

    /** The last event sent, acknowledged as the hub would, so the next press may go. */
    private fun lastEvent(): ExtensionUIStreamEvent {
        rule.waitForIdle()
        val request = requests.filterIsInstance<ExtensionUIStreamEventRequest>().last()
        runBlocking { session.acknowledged(identity, request.requestId) }
        return request.interaction
    }

    @Test @Config(qualifiers = "en-w400dp-h900dp-xhdpi")
    fun onAPhoneASectionsLinkIsPressedAsTheSectionsNode() {
        show(snapshot("$needs,$calendar,$lark"))
        assertTrue("the profile takes all of UI version 8", session.state.value.unsupported.isEmpty())
        assertTrue(rule.onAllNodesWithTag(ExtensionCardSurfaceTag).fetchSemanticsNodes().isEmpty())
        rule.onNode(hasContentDescription("Calendar") and hasClickAction()).performClick()
        val event = lastEvent()
        assertEquals("calendar", event.key)
        assertEquals("open_calendar", event.action)
        assertEquals("press", event.event)
        assertEquals("""{"range":"day"}""", event.value.toString())
        runBlocking { session.close() }
    }

    @Test @Config(qualifiers = "en-w1280dp-h800dp-xhdpi")
    fun onATabletTheStreamIsAGridOfCards() {
        show(snapshot("$needs,$calendar,$lark"))
        assertEquals(3, rule.onAllNodesWithTag(ExtensionCardSurfaceTag).fetchSemanticsNodes().size)
        rule.onNode(hasContentDescription("All") and hasClickAction()).performClick()
        val event = lastEvent()
        assertEquals("needs", event.key)
        assertEquals("open_needs", event.action)
        // a row in a card is still its own node
        rule.onNodeWithText("Approve the travel request").performClick()
        assertEquals("need-1", lastEvent().key)
        runBlocking { session.close() }
    }

    @Test fun theProfileTakesASectionsIconColourButNotYetARows() {
        fun gaps(payload: String) = NativeExtensionProfile.gaps(Json.parseToJsonElement(payload).jsonObject.getValue("view").jsonObject)
        assertEquals(emptyList<String>(), gaps(snapshot("$needs,$calendar,$lark")))
        val row = """{"key":"s","items":[{"type":"row","key":"r","title":"Lamp","icon":"light","icon_color":"yellow"}]}"""
        assertEquals(listOf("view.sections[].items[].icon_color"), gaps(snapshot(row, "stack")))
    }

    @Test fun aDiffsPlaceMirrorsTheListInAStackAndInAGrid() {
        val diff = """{"type":"diff","key":"diff","path":"a.kt"}"""
        fun tree(payload: String) = (NativeStreamReducer().snapshot(payload) as NativeStreamUpdate.Applied).tree
        val rows = """{"type":"row","key":"r1","title":"One"},{"type":"row","key":"r2","title":"Two"},{"type":"row","key":"r3","title":"Three"}"""
        // a stack: the review bar, the section's head, three rows, the diff
        val plain = tree(snapshot("""{"key":"s","title":"Changes","items":[$rows,$diff]}""", "stack"))
        assertEquals("diff" to 5, nativeDiffIndex(plain, "a.kt"))
        // rows side by side are one block; a link without a title still has a head
        val side = tree(snapshot("""{"key":"s","columns":2,"link":{"title":"All","action":{"id":"all"}},"items":[$rows,$diff]}""", "stack"))
        assertEquals("diff" to 3, nativeDiffIndex(side, "a.kt"))
        // a grid: one item per row of cards (Needs me, then Calendar | the diff's card)
        val grid = tree(snapshot("""$needs,$calendar,{"key":"s","title":"Changes","items":[$diff]}"""))
        assertEquals("diff" to 2, nativeDiffIndex(grid, "a.kt", columns = 2))
        assertEquals("diff" to 2, nativeDiffIndex(grid, "a.kt", columns = 3))
        // the same view on a phone is the stack: the review bar, Needs me's head and its rows as one block, Calendar's
        // head and row, the head of the diff's section
        assertEquals("diff" to 6, nativeDiffIndex(grid, "a.kt", columns = 1))
        assertNull(nativeDiffIndex(grid, "b.kt", columns = 2))
    }
}
