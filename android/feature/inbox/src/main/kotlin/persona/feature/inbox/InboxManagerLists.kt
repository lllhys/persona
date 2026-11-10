package persona.feature.inbox

import persona.core.contracts.api.CurrentAssistant

// "Rules" and "Muted" are each manager's own (docs/assistant/managers.md "Clients"; the web's RulesDialog and MutesDialog): the
// sheets show the current assistant's, read with it named, and what is changed there goes to it.

/**
 * Whose list a sheet reads: [id] the current assistant to name on the request (null with one manager, or a hub
 * without a roster: none is named, as before), and whether it is [offline] (its list can't be read until it is back).
 */
internal data class ListOwner(val id: String? = null, val offline: Boolean = false)

internal fun CurrentAssistant.listOwner(): ListOwner =
    if (roster?.isMulti == true) ListOwner(id, manager?.online == false) else ListOwner()
