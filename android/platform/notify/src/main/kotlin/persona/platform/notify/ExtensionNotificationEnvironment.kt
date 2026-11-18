package persona.platform.notify

import persona.core.contracts.GroupKeys
import persona.core.contracts.api.ExtensionManagement
import persona.core.contracts.api.ExtensionNotificationActionRecord
import persona.core.contracts.api.ExtensionNotificationScope
import persona.core.contracts.api.ExtensionUi
import persona.core.contracts.api.ManagerPins
import persona.core.contracts.api.pinned

/** Resolves only the currently selected, still-identical pairing; it never opens another hub connection. */
interface ExtensionNotificationEnvironment {
    fun currentScope(): ExtensionNotificationScope?
    fun extensionUi(record: ExtensionNotificationActionRecord): ExtensionUi?
    /** Merged events without a machine or alert id are routable only when one open group makes their source unambiguous. */
    fun canRouteUnscopedEvents(): Boolean = false
}

internal fun extensionEventScope(environment: ExtensionNotificationEnvironment, machineId: String, alertId: String?): ExtensionNotificationScope? {
    val current = environment.currentScope() ?: return null
    val hasMachine = machineId.isNotEmpty()
    val hasAlert = !alertId.isNullOrEmpty()
    if (!hasMachine && !hasAlert) return current.takeIf { environment.canRouteUnscopedEvents() }
    val machineNamespace = if (hasMachine) GroupKeys.namespaceOf(machineId) else null
    val alertNamespace = if (hasAlert) GroupKeys.namespaceOf(alertId.orEmpty()) else null
    if (hasMachine && hasAlert && machineNamespace != alertNamespace) return null
    val namespace = if (hasMachine) machineNamespace else alertNamespace
    return current.takeIf { it.namespace == namespace }
}

fun ExtensionManagement.forExtensionNotification(record: ExtensionNotificationActionRecord): ExtensionUi {
    val pins = ManagerPins.of(listOf(listOf("extensions", record.extension), listOf("plugins", record.extension)), record.scope.manager)
    return pinned(pins)
}
