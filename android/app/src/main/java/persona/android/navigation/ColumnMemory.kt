package persona.android.navigation

import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/**
 * Remembers whether the Persona sidebar column and the Agents list were last left shown or hidden beside the page
 * (matching iOS `SplitListMemory`): starts from the kept flag and updates it only when a person explicitly toggles
 * that column while it is beside the page in the visible, foreground, non-demo, non-automation shell.
 */
@Stable
internal class ColumnMemory(
    initialSidebarShown: Boolean = true,
    initialAgentsShown: Boolean = true,
    private val persist: Boolean = true,
) {
    var sidebarShown by mutableStateOf(if (persist) initialSidebarShown else true)
        private set
    var agentsShown by mutableStateOf(if (persist) initialAgentsShown else true)
        private set

    fun toggleSidebarBeside(
        shown: Boolean,
        regular: Boolean,
        columnsOver: Boolean,
        tabShown: Boolean,
        foreground: Boolean,
    ): Boolean {
        if (!persist || !regular || columnsOver || !tabShown || !foreground) return false
        sidebarShown = shown
        return true
    }

    fun toggleAgentsBeside(
        shown: Boolean,
        twoPanes: Boolean,
        columnsOver: Boolean,
        tabShown: Boolean,
        foreground: Boolean,
    ): Boolean {
        if (!persist || !twoPanes || columnsOver || !tabShown || !foreground) return false
        agentsShown = shown
        return true
    }
}
