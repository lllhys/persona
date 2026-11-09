package persona.feature.inbox

import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.CompositionLocalProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.core.navigation.InboxRoute
import persona.core.navigation.Navigator
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

val LocalInboxRepository = staticCompositionLocalOf<InboxRepository?> { null }
internal val LocalInboxNavigator = staticCompositionLocalOf<Navigator?> { null }

@Serializable @SerialName("persona.feature.inbox.LarkRoute")
data class LarkRoute(val feedId: String, val itemId: String? = null, val startPosition: Long? = null) : NavKey

fun EntryProviderScope<NavKey>.inboxEntries(
    navigator: Navigator,
    repository: InboxRepository? = null,
    /** The hub's connectors (their threads and replies); null in demo mode: a connector's item opens its detail page. */
    connectors: persona.core.contracts.api.ConnectorsManagement? = null,
    mailTransport: persona.core.contracts.api.MailTransport? = null,
    onMailAskAi: ((MailAiContext) -> Unit)? = null,
    onMailOpenLink: ((String) -> Unit)? = null,
    /** Opens a request card outside any session on Agents (#5610). */
    onOpenCard: ((persona.core.navigation.RequestRef) -> Unit)? = null,
) {
    entry<InboxRoute> {
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        InboxScreen(source, navigator, onOpenCard)
    }
    entry<MailRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        PinItemManager(source, route.itemId)
        val pairedMail by source.mailTransport.collectAsStateWithLifecycle()
        LarkLinkHost(source, itemManager(source, route.itemId)) {
            val opener = LocalLarkLinkOpener.current
            MailScreen(source, mailTransport ?: LocalMailTransport.current ?: pairedMail, route, navigator,
                onMailAskAi, onMailOpenLink ?: { url -> opener?.invoke(url, null); Unit })
        }
    }
    entry<LarkRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        PinItemManager(source, route.itemId)
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkScreen(source, route, navigator)
        }
    }
    entry<InboxItemDetailRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        PinItemManager(source, route.itemId)
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkLinkHost(source, itemManager(source, route.itemId)) { InboxItemDetailScreen(source, route, navigator) }
        }
    }
    entry<InboxEventRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        PinItemManager(source, route.itemId)
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkLinkHost(source, itemManager(source, route.itemId)) { InboxEventScreen(source, route, navigator) }
        }
    }
    entry<ConnectorThreadRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        PinItemManager(source, route.itemId)
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkLinkHost(source, itemManager(source, route.itemId)) {
                if (connectors == null) InboxItemDetailScreen(source, InboxItemDetailRoute(route.itemId), navigator)
                else ConnectorThreadScreen(source, connectors, route, navigator)
            }
        }
    }
    entry<LarkMomentRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        PinItemManager(source, route.itemId)
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkMomentsScreen(source, route, navigator)
        }
    }
}

@Serializable @SerialName("persona.feature.inbox.MailRoute")
data class MailRoute(val itemId: String) : NavKey

@Serializable @SerialName("persona.feature.inbox.LarkMomentRoute")
data class LarkMomentRoute(val postId: String, val itemId: String? = null) : NavKey

/** The own page of an item that is neither a chat, a Moments post nor a mail: a Feishu document's notice, or an unknown item. */
@Serializable @SerialName("persona.feature.inbox.InboxItemDetailRoute")
data class InboxItemDetailRoute(val itemId: String) : NavKey

/** A hub connector's item opened as its conversation (ConnectorThread.kt): the thread its `connector` block names. */
@Serializable @SerialName("persona.feature.inbox.ConnectorThreadRoute")
data class ConnectorThreadRoute(val itemId: String) : NavKey

val LocalMailTransport = staticCompositionLocalOf<persona.core.contracts.api.MailTransport?> { null }

/** What an inbox item's pages read besides the item itself (its Lark chat, its mail, its connector's thread). */
internal val ItemDetailPrefixes = listOf(
    listOf("lark", "conversations"), listOf("lark", "messages"), listOf("lark", "users"), listOf("lark", "moments"),
    listOf("lark", "media"), listOf("lark", "pinned"), listOf("lark", "documents"), listOf("lark", "raw"),
    listOf("mail", "messages"), listOf("connectors"),
)

/**
 * While an inbox item's page is open, what it reads besides the item goes to the item's manager (the merged list's
 * stamp, docs/assistant/managers.md "Clients"); the default's, or one manager, sends none.
 */
@androidx.compose.runtime.Composable
internal fun PinItemManager(source: InboxRepository, itemId: String?) {
    val snapshot by source.snapshot.collectAsStateWithLifecycle()
    val record = snapshot.records.inboxRecord(itemId)
    val manager = record?.manager
    // the item itself too (its mail page reads it), by its id on the hub (InboxItemKey)
    val hubId = record?.hubItemId
    androidx.compose.runtime.DisposableEffect(manager, hubId) {
        val release = persona.core.contracts.api.AssistantManagerChoice.pin(
            ItemDetailPrefixes + listOfNotNull(hubId?.let { listOf("inbox", it) }), manager)
        onDispose { release() }
    }
}

/** The manager the item [itemId] names is stamped with (null: none known), for the links its page opens (LarkLinkHost). */
@androidx.compose.runtime.Composable
internal fun itemManager(source: InboxRepository, itemId: String?): String? {
    val snapshot by source.snapshot.collectAsStateWithLifecycle()
    return snapshot.records.inboxRecord(itemId)?.manager
}
