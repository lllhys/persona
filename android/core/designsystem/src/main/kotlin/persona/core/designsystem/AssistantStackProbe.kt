package persona.core.designsystem

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import kotlinx.coroutines.delay

/** DEBUG automation backend; release has no installer, listener or file writes. */
interface AssistantStackProbeBackend {
    fun publishNavigation(route: String, selectedKey: String?)
    fun publishDraft(page: String, draft: String, focused: Boolean)
    fun removeDraft(page: String)
    fun touch()
}

object AssistantStackProbe {
    @Volatile private var backend: AssistantStackProbeBackend? = null
    @Volatile var enabled: Boolean = false
        private set

    fun install(backend: AssistantStackProbeBackend) {
        if (enabled) return
        this.backend = backend
        enabled = true
    }

    fun uninstall(backend: AssistantStackProbeBackend) {
        if (this.backend !== backend) return
        enabled = false
        this.backend = null
    }

    internal fun current(): AssistantStackProbeBackend? = backend
}

@Composable
fun AssistantStackNavigationProbe(route: String, selectedKey: String?) {
    val backend = AssistantStackProbe.current() ?: return
    SideEffect { backend.publishNavigation(route, selectedKey) }
    LaunchedEffect(backend) {
        while (true) {
            delay(2_000)
            backend.touch()
        }
    }
}

@Composable
fun AssistantStackDraftProbe(page: String?, draft: String, focused: Boolean) {
    if (page == null) return
    val backend = AssistantStackProbe.current() ?: return
    SideEffect { backend.publishDraft(page, draft, focused) }
    DisposableEffect(backend, page) { onDispose { backend.removeDraft(page) } }
}
