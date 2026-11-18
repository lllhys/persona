package persona.feature.settings

import persona.core.contracts.ExtensionUiEvent
import persona.core.contracts.api.*
import persona.core.designsystem.icons.PhIcons
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4

// Extensions' UI slots on Android (docs/extensions/extensions.md "UI slots"): what Settings › Extensions, the settings tab's badge and
// machines' pages get from the hub's list, and the badge events.
/** Robolectric for the words: the app's strings in Simplified Chinese (robolectric.properties). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ExtensionSlotsTest {
    private val res get() = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    private fun nav(id: String, badge: String? = null, icon: String? = "chart") =
        ExtensionUiPlace("example-gateway", id, "用量 $id", "nav_page", icon = icon, badge = badge?.let { ExtensionViewBadge(it, "danger") })

    private val card = ExtensionUiPlace("example-gateway", "today", "今天的用量", "overview_card", size = "compact", opens = "usage")
    private val section = ExtensionUiPlace("example-gateway", "gpu", "网关调用", "machine_section")

    private class FakeSlots(var places: List<ExtensionUiPlace>) : ExtensionApiStub() {
        val asked = mutableListOf<String>()
        var fail: Exception? = null
        override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?): List<ExtensionUiPlace> {
            asked += placement
            fail?.let { throw it }
            return places
        }
    }

    @Test fun theSettingsTabAddsTheNavPagesBadgesUp() {
        assertEquals(SlotBadge(5), ExtensionSlotText.tabBadge(listOf(nav("a", "3"), nav("b", "2"), nav("c", "新"), nav("d"))))
        // Only words: a dot, no number.
        assertEquals(SlotBadge(0, dot = true), ExtensionSlotText.tabBadge(listOf(nav("a", "新"), nav("b", " "))))
        assertEquals(SlotBadge(), ExtensionSlotText.tabBadge(listOf(nav("a"))))
        // Only nav pages have badges.
        assertEquals(SlotBadge(), ExtensionSlotText.tabBadge(listOf(card.copy(badge = ExtensionViewBadge("9")))))
    }

    @Test fun iconsFromTheFixedListAndTheExtensionsOtherwise() {
        assertSame(PhIcons.ChartBar, ExtensionSlotText.icon("chart"))
        assertSame(PhIcons.Coins, ExtensionSlotText.icon("money"))
        assertSame(PhIcons.CalendarBlank, ExtensionSlotText.icon("calendar"))
        assertSame(PhIcons.List, ExtensionSlotText.icon("list"))
        assertSame(PhIcons.PuzzlePiece, ExtensionSlotText.icon("plugin"))
        assertSame(PhIcons.PuzzlePiece, ExtensionSlotText.icon("rocket"))
        assertSame(PhIcons.PuzzlePiece, ExtensionSlotText.icon(null))
    }

    @Test fun aCardOpensThePageItNames() {
        val places = listOf(nav("usage"), card)
        assertEquals("用量 usage", ExtensionSlotText.opened(places, card)?.title)
        // A page that is not listed (an extension_page) still opens, under the card's title.
        assertEquals("今天的用量", ExtensionSlotText.opened(listOf(card.copy(opens = "home")), card.copy(opens = "home"))?.title)
        assertNull(ExtensionSlotText.opened(places, card.copy(opens = null)))
    }

    // Read only on a hub with extension_ui_slots while it is online, all slots at once (no placement); a placement this app
    // does not know is ignored; extension_ui events change one nav page's badge, none clears it.
    @Test fun readsTheSlotsAndAppliesBadgeEvents() = runTest {
        val api = FakeSlots(listOf(nav("usage", "3"), card, section, ExtensionUiPlace("example-gateway", "bar", "工具栏", "session_action"),
            ExtensionUiPlace("example-gateway", "home", "概况", "plugin_page")))
        val slotsOn = MutableStateFlow(false)
        val online = MutableStateFlow(true)
        val events = MutableSharedFlow<ExtensionUiEvent>()
        val slots = ExtensionSlots(api, slotsOn, events)
        backgroundScope.launch { slots.run(online) }
        runCurrent()
        assertTrue(api.asked.isEmpty())
        slots.load()
        assertTrue("an older hub is never asked", api.asked.isEmpty())
        slotsOn.value = true
        runCurrent()
        // All slots at once, then the inbox items' pages, which are listed by their placement only.
        assertEquals(listOf("", "inbox_item"), api.asked)
        val state = slots.state.value
        assertTrue(state.available)
        assertEquals(listOf("usage"), state.navPages.map { it.id })
        assertEquals(listOf("today"), state.cards.map { it.id })
        assertEquals(listOf("gpu"), state.machineSections.map { it.id })
        assertEquals(3, state.places.size)
        assertEquals(SlotBadge(3), state.tabBadge)

        events.emit(ExtensionUiEvent("example-gateway", "usage", ExtensionViewBadge("7", "caution")))
        runCurrent()
        assertEquals("7", slots.state.value.navPages.single().badge?.text)
        assertEquals(SlotBadge(7), slots.state.value.tabBadge)
        events.emit(ExtensionUiEvent("example-gateway", "today", ExtensionViewBadge("1")))
        events.emit(ExtensionUiEvent("other", "usage", ExtensionViewBadge("1")))
        runCurrent()
        assertNull(slots.state.value.cards.single().badge)
        assertEquals(SlotBadge(7), slots.state.value.tabBadge)
        events.emit(ExtensionUiEvent("example-gateway", "usage", null))
        runCurrent()
        assertNull(slots.state.value.navPages.single().badge)

        // Back online: read again. A failure keeps what was there.
        online.value = false
        runCurrent()
        api.fail = HubFailure(HubError.Server, "坏了", status = 500)
        online.value = true
        runCurrent()
        assertEquals(4, api.asked.size)
        assertEquals("扩展的入口读不到：坏了", slots.state.value.error?.text(res))
        assertEquals(1, slots.state.value.navPages.size)

        // The hub lost extension_ui_slots (another hub selected, an older one): nothing is drawn.
        slotsOn.value = false
        runCurrent()
        assertEquals(ExtensionSlotsState(), slots.state.value)
    }
}
