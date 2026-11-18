package persona.core.domain.sort

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.Session

/** What closing an agent asks, the same from the list's long press, its swipe and the chat's menu (iOS CloseAgentSheet). */
data class CloseConfirmation(val title: CoreText, val message: CoreText)

/**
 * Says which agent goes and where: its title, then "machine / space › tab", then what happens (the agent quits, its tab
 * closes, work in progress stops) and that the conversation stays for later. A session without a terminal (a runtime's,
 * #6294) has no tab or pane to close: it only says the session ends, since whether its conversation stays is the
 * runtime's to say.
 */
fun Session.closeConfirmation(machineName: String?): CloseConfirmation {
    val machine = (machineName?.takeIf(String::isNotBlank) ?: machineId).takeIf(String::isNotBlank)
        ?.let(CoreText::plain) ?: CoreText.of(Key.ComputerFallback)
    val tab = slotName?.takeIf(String::isNotBlank)
    val place = listOfNotNull(spaceName?.takeIf(String::isNotBlank), tab).joinToString(" › ")
    val where = CoreText.joined(" / ", listOf(machine, CoreText.plain(place)))
    return CloseConfirmation(
        CoreText.of(Key.CloseAgentTitle),
        when {
            // First: a Paseo row's slot_name is its workspace's title, not a terminal tab.
            !canTerminal -> CoreText.of(Key.CloseAgentMessageRuntime, displayTitle, where, machine)
            tab != null -> CoreText.of(Key.CloseAgentMessageTab, displayTitle, where, machine, agentLabel, tab)
            else -> CoreText.of(Key.CloseAgentMessagePane, displayTitle, where, machine, agentLabel)
        },
    )
}
