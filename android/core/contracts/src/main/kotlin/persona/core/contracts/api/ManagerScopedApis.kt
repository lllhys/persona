package persona.core.contracts.api

import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonObject

// Settings for the current assistant (docs/assistant/managers.md "Clients"): every request made through one of these
// names [manager] (RequestManager), as ProviderSetup.forManager and ModelConfiguration.forManager do, so a page reads
// and writes that assistant's alone. A merged list asked this way is that manager's only.

/** This triage API with every request for [manager]. */
fun AgentTriage.forManager(manager: String): AgentTriage = ManagerAgentTriage(this, RequestManager(manager))

/** This knowledge-selection API with every request and invalidation for [manager]. */
fun KnowledgeSelection.forManager(manager: String): KnowledgeSelection =
    ManagerKnowledgeSelection(this, manager, RequestManager(manager))

/** This connectors API (connectors, mail accounts, Lark, Messages) with every request for [manager]. */
fun ConnectorsManagement.forManager(manager: String): ConnectorsManagement = ManagerConnectors(this, RequestManager(manager))

/** This channels API with every request for [manager]. */
fun ChannelManagement.forManager(manager: String): ChannelManagement = ManagerChannels(this, RequestManager(manager))

/** This Lark bot API with every request for [manager]. */
fun FeishuBotSettings.forManager(manager: String): FeishuBotSettings = ManagerFeishuBot(this, RequestManager(manager))

/** This sent-as-you ledger with every request for [manager]. */
fun HubSent.forManager(manager: String): HubSent = ManagerSent(this, RequestManager(manager))

/** This standing-authorizations list with every request for [manager]. */
fun StandingAuthorizationsSource.forManager(manager: String): StandingAuthorizationsSource =
    ManagerStandingAuthorizations(this, RequestManager(manager))

private class ManagerAgentTriage(private val source: AgentTriage, private val owner: RequestManager) : AgentTriage {
    override suspend fun switches() = withContext(owner) { source.switches() }
    override suspend fun put(value: TriageSwitches) = withContext(owner) { source.put(value) }
}

private class ManagerKnowledgeSelection(
    private val upstream: KnowledgeSelection,
    private val manager: String,
    private val owner: RequestManager,
) : KnowledgeSelection {
    override val changes = upstream.changes.filter { persona.core.contracts.pushManager(it.manager) == manager }
    override suspend fun selection() = withContext(owner) { upstream.selection() }
    override suspend fun put(document: JsonObject) = withContext(owner) { upstream.put(document) }
    override suspend fun <T> withPinnedConnection(block: suspend KnowledgeSelection.() -> T): T =
        withContext(owner) { upstream.withPinnedConnection(block) }
    override suspend fun candidates(source: String) = withContext(owner) { upstream.candidates(source) }
    override suspend fun documents() = withContext(owner) { upstream.documents() }
    override suspend fun document(id: String) = withContext(owner) { upstream.document(id) }
}

private class ManagerChannels(private val source: ChannelManagement, private val owner: RequestManager) : ChannelManagement by source {
    override suspend fun channels() = withContext(owner) { source.channels() }
    override suspend fun channel(id: String) = withContext(owner) { source.channel(id) }
    override suspend fun update(id: String, change: ChannelChange) = withContext(owner) { source.update(id, change) }
    override suspend fun bind(id: String, step: String?, values: Map<String, String>?) = withContext(owner) { source.bind(id, step, values) }
    override suspend fun unbind(id: String) = withContext(owner) { source.unbind(id) }
    override suspend fun test(id: String) = withContext(owner) { source.test(id) }
}

private class ManagerFeishuBot(private val source: FeishuBotSettings, private val owner: RequestManager) : FeishuBotSettings {
    override suspend fun status() = withContext(owner) { source.status() }
    override suspend fun update(update: FeishuBotUpdate) = withContext(owner) { source.update(update) }
}

private class ManagerStandingAuthorizations(
    private val source: StandingAuthorizationsSource,
    private val owner: RequestManager,
) : StandingAuthorizationsSource {
    override val hubIdentity: StateFlow<String> get() = source.hubIdentity
    override fun pinned(): StandingAuthorizationsSource = ManagerStandingAuthorizations(source.pinned(), owner)
    override suspend fun list() = withContext(owner) { source.list() }
    override suspend fun revoke(id: String) = withContext(owner) { source.revoke(id) }
}

private class ManagerSent(private val source: HubSent, private val owner: RequestManager) : HubSent {
    override suspend fun page(before: Long?, limit: Int?, connector: String?) = withContext(owner) { source.page(before, limit, connector) }
    override suspend fun recall(id: Long) = withContext(owner) { source.recall(id) }
}

private class ManagerConnectors(private val source: ConnectorsManagement, private val owner: RequestManager) : ConnectorsManagement {
    override suspend fun connectors(all: Boolean) = withContext(owner) { source.connectors(all) }
    override suspend fun setConnectorEnabled(id: String, enabled: Boolean) = withContext(owner) { source.setConnectorEnabled(id, enabled) }
    override suspend fun setConnectorFeed(id: String, feed: String, on: Boolean) = withContext(owner) { source.setConnectorFeed(id, feed, on) }
    override suspend fun deleteConnectorData(id: String) = withContext(owner) { source.deleteConnectorData(id) }
    override suspend fun deleteConnectorArchive(id: String) = withContext(owner) { source.deleteConnectorArchive(id) }
    override suspend fun mailAccounts() = withContext(owner) { source.mailAccounts() }
    override suspend fun addMailAccount(req: AddMailAccountRequest) = withContext(owner) { source.addMailAccount(req) }
    override suspend fun deleteMailAccount(id: String) = withContext(owner) { source.deleteMailAccount(id) }
    override suspend fun testMailAccount(id: String) = withContext(owner) { source.testMailAccount(id) }
    override suspend fun googleSignInStart(loginHint: String?) = withContext(owner) { source.googleSignInStart(loginHint) }
    override suspend fun googleSignInFinish(finish: GoogleFinish) = withContext(owner) { source.googleSignInFinish(finish) }
    override suspend fun googleConfig() = withContext(owner) { source.googleConfig() }
    override suspend fun larkStatus() = withContext(owner) { source.larkStatus() }
    override suspend fun larkAccounts() = withContext(owner) { source.larkAccounts() }
    override suspend fun larkChatChoices() = withContext(owner) { source.larkChatChoices() }
    override suspend fun connectorLogin(id: String, step: String, values: Map<String, String>) =
        withContext(owner) { source.connectorLogin(id, step, values) }
    override suspend fun connectorLogout(id: String) = withContext(owner) { source.connectorLogout(id) }
    override suspend fun connectorThread(id: String, thread: String, before: String?, limit: Int) =
        withContext(owner) { source.connectorThread(id, thread, before, limit) }
    override suspend fun connectorThreadReply(id: String, thread: String, action: String, text: String, replyTo: String?) =
        withContext(owner) { source.connectorThreadReply(id, thread, action, text, replyTo) }
    override suspend fun microsoftConfig() = withContext(owner) { source.microsoftConfig() }
    override suspend fun setMicrosoftClientId(clientId: String) = withContext(owner) { source.setMicrosoftClientId(clientId) }
    override suspend fun microsoftSignInStart() = withContext(owner) { source.microsoftSignInStart() }
    override suspend fun microsoftSignInFinish(finish: MicrosoftFinish) = withContext(owner) { source.microsoftSignInFinish(finish) }
    override suspend fun larkMessageDisplay() = withContext(owner) { source.larkMessageDisplay() }
    override suspend fun setLarkMessageDisplay(current: LarkMessageDisplay, key: String, value: Boolean) =
        withContext(owner) { source.setLarkMessageDisplay(current, key, value) }
    override suspend fun messagesStatus() = withContext(owner) { source.messagesStatus() }
    override suspend fun setMessagesAi(on: Boolean) = withContext(owner) { source.setMessagesAi(on) }
    override suspend fun deleteMessagesData() = withContext(owner) { source.deleteMessagesData() }
    override suspend fun messagesChats(before: String?) = withContext(owner) { source.messagesChats(before) }
    override suspend fun messagesThread(chatId: String, before: String?) = withContext(owner) { source.messagesThread(chatId, before) }
    override suspend fun gitHubAuth() = withContext(owner) { source.gitHubAuth() }
    override suspend fun setGitHubAuth(machine: String?) = withContext(owner) { source.setGitHubAuth(machine) }
    override suspend fun setGitHubInboxOptions(options: GitHubInboxOptions) = withContext(owner) { source.setGitHubInboxOptions(options) }
}
