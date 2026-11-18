package persona.android.navigation

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ColumnMemoryTest {
    @Test fun startsFromSavedWhenPersistingAndShownInAutomationOrDemo() {
        val memory = ColumnMemory(initialSidebarShown = false, initialAgentsShown = false, persist = true)
        assertFalse(memory.sidebarShown)
        assertFalse(memory.agentsShown)

        val nonPersisted = ColumnMemory(initialSidebarShown = false, initialAgentsShown = false, persist = false)
        assertTrue(nonPersisted.sidebarShown)
        assertTrue(nonPersisted.agentsShown)
    }

    @Test fun togglingBesideThePageUpdatesOnlyThatColumn() {
        val memory = ColumnMemory()
        assertTrue(memory.toggleSidebarBeside(shown = false, regular = true, columnsOver = false, tabShown = true, foreground = true))
        assertFalse(memory.sidebarShown)
        assertTrue(memory.agentsShown)

        assertTrue(memory.toggleAgentsBeside(shown = false, twoPanes = true, columnsOver = false, tabShown = true, foreground = true))
        assertFalse(memory.sidebarShown)
        assertFalse(memory.agentsShown)

        assertTrue(memory.toggleSidebarBeside(shown = true, regular = true, columnsOver = false, tabShown = true, foreground = true))
        assertTrue(memory.sidebarShown)
        assertFalse(memory.agentsShown)
    }

    @Test fun overThePageCompactHiddenTabBackgroundAndNonPersistedDoNotWrite() {
        val memory = ColumnMemory(initialSidebarShown = false, initialAgentsShown = false, persist = true)

        assertFalse(memory.toggleSidebarBeside(shown = true, regular = true, columnsOver = true, tabShown = true, foreground = true))
        assertFalse(memory.toggleSidebarBeside(shown = true, regular = false, columnsOver = false, tabShown = true, foreground = true))
        assertFalse(memory.toggleSidebarBeside(shown = true, regular = true, columnsOver = false, tabShown = false, foreground = true))
        assertFalse(memory.toggleSidebarBeside(shown = true, regular = true, columnsOver = false, tabShown = true, foreground = false))
        assertFalse(memory.sidebarShown)

        assertFalse(memory.toggleAgentsBeside(shown = true, twoPanes = true, columnsOver = true, tabShown = true, foreground = true))
        assertFalse(memory.toggleAgentsBeside(shown = true, twoPanes = false, columnsOver = false, tabShown = true, foreground = true))
        assertFalse(memory.toggleAgentsBeside(shown = true, twoPanes = true, columnsOver = false, tabShown = false, foreground = true))
        assertFalse(memory.toggleAgentsBeside(shown = true, twoPanes = true, columnsOver = false, tabShown = true, foreground = false))
        assertFalse(memory.agentsShown)

        val nonPersisted = ColumnMemory(persist = false)
        assertFalse(nonPersisted.toggleSidebarBeside(shown = false, regular = true, columnsOver = false, tabShown = true, foreground = true))
        assertFalse(nonPersisted.toggleAgentsBeside(shown = false, twoPanes = true, columnsOver = false, tabShown = true, foreground = true))
        assertTrue(nonPersisted.sidebarShown)
        assertTrue(nonPersisted.agentsShown)
    }
}
