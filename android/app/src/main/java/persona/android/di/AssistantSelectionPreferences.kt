package persona.android.di

import android.content.SharedPreferences
import persona.core.contracts.api.AssistantManagerChoice
import persona.core.contracts.api.ManagerRoster

/** Restores a hub's selection before its roster arrives, then validates it against a successful roster. */
internal class AssistantSelectionPreferences(private val preferences: SharedPreferences) {
    private var hubId: String? = null
    private var rosterReady = false

    @Synchronized
    fun atHub(id: String?) {
        remember(AssistantManagerChoice.current.value)
        hubId = id
        rosterReady = false
        if (id == null) {
            AssistantManagerChoice.atHub(null)
            return
        }
        AssistantManagerChoice.choose(
            id,
            preferences.getString(managerKey(id), null),
            preferences.getString(defaultKey(id), ManagerRoster.ASSISTANT_MANAGER) ?: ManagerRoster.ASSISTANT_MANAGER,
        )
    }

    @Synchronized
    fun reconcile(id: String?, roster: ManagerRoster) {
        if (id == null || id != hubId) return
        val choice = AssistantManagerChoice.current.value
        // Kept while the roster lists it installed and in the switcher: one gone, still installing or hidden there (the
        // hub's own assistant, kept out while it is not used) falls back to the default.
        val chosen = choice.manager?.takeIf { manager ->
            choice.hubId == id && roster.isMulti && roster.installed.any { it.id == manager && !it.hidden }
        }
        AssistantManagerChoice.choose(id, chosen, roster.defaultManagerId)
        rosterReady = true
        remember(AssistantManagerChoice.current.value)
    }

    @Synchronized
    fun remember(choice: AssistantManagerChoice.Choice) {
        val id = hubId ?: return
        if (!rosterReady || choice.hubId != id || choice != AssistantManagerChoice.current.value) return
        preferences.edit()
            .putString(managerKey(id), choice.manager)
            .putString(defaultKey(id), choice.defaultId)
            .apply()
    }

    private fun managerKey(id: String) = "manager:$id"
    private fun defaultKey(id: String) = "default:$id"

    companion object {
        const val FILE = "assistant-selection"
    }
}
