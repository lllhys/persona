package persona.feature.knowledge

import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.core.contracts.api.*
import persona.core.navigation.Navigator
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent

@Serializable @SerialName("persona.feature.knowledge.KnowledgeRoute") data object KnowledgeRoute : NavKey
@Serializable @SerialName("persona.feature.knowledge.KnowledgeDetailRoute") data class KnowledgeDetailRoute(val kind: KnowledgeKind, val id: String) : NavKey

@EntryPoint
@InstallIn(SingletonComponent::class)
interface KnowledgeSendingEntryPoint {
    fun mainConversationSender(): MainConversationSender
}

/**
 * The host must route links to its in-app reader, never to an external ACTION_VIEW intent. [onNewAgent] opens
 * "New session" with a project's directory and context filled in ("Start agent"); without it the page copies the context and
 * switches to Agents, where the user starts the session.
 */
fun EntryProviderScope<NavKey>.knowledgeEntries(
    navigator: Navigator,
    connection: KnowledgeConnection,
    @Suppress("UNUSED_PARAMETER") assistant: AssistantConversations,
    sender: MessageSender,
    openLinkInApp: (String) -> Unit,
    onNewAgent: ((KnowledgeAgentStart) -> Unit)? = null,
) {
    entry<KnowledgeRoute> {
        // The current assistant's knowledge base (docs/assistant/managers.md "Clients": the hub client names it on `kb…`):
        // a model per assistant, so switching reads the other one's and nothing of the last one's stays.
        val assistant = persona.core.designsystem.currentAssistant().id
        val model = viewModel(key = "knowledge:$assistant") { KnowledgeViewModel(connection) }
        KnowledgeScreen(model, navigator)
    }
    entry<KnowledgeDetailRoute> { route ->
        val context = LocalContext.current.applicationContext
        val mainSender = remember(sender, context) {
            sender as? MainConversationSender ?: EntryPointAccessors.fromApplication(
                context, KnowledgeSendingEntryPoint::class.java,
            ).mainConversationSender()
        }
        val model = viewModel(key = "knowledge/${route.kind.wire}/${route.id}") {
            KnowledgeDetailViewModel(route.kind, route.id, connection, mainSender)
        }
        KnowledgeDetailScreen(model, navigator, openLinkInApp, onNewAgent)
    }
}
