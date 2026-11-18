package persona.core.designsystem

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

internal enum class HardwareShortcut { Escape, Help }
internal enum class HardwareKeyPhase { Down, Up }

internal class HardwareShortcutRegistration internal constructor(
    internal val token: Any,
    internal val onDismiss: () -> Unit,
)

/** The dismiss callbacks are ordered by composition, not by window type. Only the top one receives Escape. */
internal class HardwareShortcutController {
    private val registrations = mutableListOf<HardwareShortcutRegistration>()

    var helpVisible by mutableStateOf(false)
        private set

    internal var helpOwner: Any? = null
        private set

    internal val registrationCount: Int get() = registrations.size

    fun register(token: Any, onDismiss: () -> Unit): HardwareShortcutRegistration {
        registrations.removeAll { it.token === token }
        return HardwareShortcutRegistration(token, onDismiss).also(registrations::add)
    }

    fun unregister(registration: HardwareShortcutRegistration) {
        registrations.removeAll { it === registration }
    }

    fun dispatch(shortcut: HardwareShortcut, phase: HardwareKeyPhase, repeated: Boolean = false, owner: Any? = null): Boolean {
        if (phase == HardwareKeyPhase.Down && !repeated) {
            when (shortcut) {
                HardwareShortcut.Escape -> registrations.lastOrNull()?.onDismiss?.invoke()
                HardwareShortcut.Help -> if (!helpVisible) {
                    helpVisible = true
                    helpOwner = owner
                }
            }
        }
        // An empty stack is deliberately a no-op, and key-up / repeats never close another layer.
        return true
    }

    fun closeHelp() {
        helpVisible = false
        helpOwner = null
    }
}
