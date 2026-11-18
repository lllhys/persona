package persona.feature.terminal

import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import persona.core.contracts.SessionKey
import persona.core.contracts.api.SessionsRepository
import persona.core.contracts.api.TerminalPanes
import persona.core.contracts.api.TerminalTransport
import persona.core.navigation.Navigator
import persona.core.navigation.TerminalPanesRoute
import persona.core.navigation.TerminalRoute

@EntryPoint
@InstallIn(SingletonComponent::class)
interface TerminalServices { fun terminal(): TerminalTransport }

/**
 * The terminal page, and with [panes] and [sessions] the "Terminal" page that lists every machine's panes (docs/integrations/terminal.md
 * "Standalone terminal"). A pane's terminal comes with its own title in the route; a session's is [title]'s. [routed] gives the
 * terminal of a machine in another hub group (its own hub, its own features); others use [transport].
 */
fun EntryProviderScope<NavKey>.terminalEntries(navigator: Navigator, transport: TerminalTransport? = null, title: (SessionKey) -> String? = { null },
                                               panes: TerminalPanes? = null, sessions: SessionsRepository? = null,
                                               routed: (SessionKey) -> TerminalTransport? = { null }, resumer: TerminalResumer? = null) {
    entry<TerminalRoute> { route ->
        val context = LocalContext.current.applicationContext
        val services = remember(context, transport) { if (transport == null) EntryPointAccessors.fromApplication(context, TerminalServices::class.java) else null }
        val model = viewModel {
            val key = SessionKey(route.machineId, route.sessionId)
            TerminalViewModel(key, routed(key) ?: transport ?: requireNotNull(services).terminal(),
                preferences = SharedTerminalPreferences(context), resumer = resumer)
        }
        TerminalPage(
            model,
            navigator::back,
            route.title.ifEmpty { null } ?: title(model.key) ?: model.key.sessionId,
            grab = null,
            resumeOnOpen = route.resume,
        )
    }
    if (panes != null && sessions != null) entry<TerminalPanesRoute> {
        val model = viewModel(key = "terminal-panes:${System.identityHashCode(panes)}:${System.identityHashCode(sessions)}") {
            TerminalPanesViewModel(panes, sessions)
        }
        TerminalPanesScreen(model, navigator)
    }
}
