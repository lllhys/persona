package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import org.junit.Assert.assertEquals
import org.junit.Test

class ExtensionPageNavigationTest {
    private val page = ExtensionUiRoute("extension", "page", "Page")

    @Test
    fun assistantDeepLinkReturnsToTheAssistantRatherThanInventingSettings() {
        val stacks = AppTab.entries.associateWith { mutableListOf(tabRoot(it)) }
        val route = Router().parse(RouteInput.DeepLink("persona://open?target=extension&extension=extension&ui=page"))!!
        assertEquals(AppTab.Persona, route.restore(stacks))
        assertEquals(listOf(AssistantRoute, route.key), stacks.getValue(AppTab.Persona))
        stacks.getValue(AppTab.Persona).removeAt(stacks.getValue(AppTab.Persona).lastIndex)
        assertEquals(listOf(AssistantRoute), stacks.getValue(AppTab.Persona))
    }

    @Test
    fun inboxAndAssistantPushesKeepTheirActualSourcePages() {
        for (source in listOf<NavKey>(InboxRoute, page.copy(uiId = "source"))) {
            val stack = mutableListOf<NavKey>(AssistantRoute, source, page)
            restoreSecondaryParents(stack)
            assertEquals(listOf(AssistantRoute, source, page), stack)
            stack.removeAt(stack.lastIndex)
            assertEquals(source, stack.last())
        }
    }

    @Test
    fun settingsOpenedExtensionKeepsItsSettingsParent() {
        val stack = mutableListOf<NavKey>(SettingsRoute, ExtensionRoute("extension"), page)
        restoreSecondaryParents(stack)
        assertEquals(listOf(SettingsRoute, ExtensionRoute("extension"), page), stack)
        stack.removeAt(stack.lastIndex)
        assertEquals(ExtensionRoute("extension"), stack.last())
        stack.removeAt(stack.lastIndex)
        assertEquals(SettingsRoute, stack.single())
    }
}
