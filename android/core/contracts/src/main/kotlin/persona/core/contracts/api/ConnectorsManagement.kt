package persona.core.contracts.api

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class MailAccount(
    val id: String,
    val name: String? = null,
    val email: String = "",
    /** gmail | imap | feishu | outlook */
    val provider: String? = null,
    /** password | oauth2 */
    val auth: String? = null,
    val username: String? = null,
    @SerialName("imap_host") val imapHost: String? = null,
    @SerialName("imap_port") val imapPort: Int? = null,
    @SerialName("smtp_host") val smtpHost: String? = null,
    @SerialName("smtp_port") val smtpPort: Int? = null,
    @SerialName("smtp_security") val smtpSecurity: String? = null,
    /** ok | error */
    val status: String? = null,
    val error: String? = null,
) {
    val isError: Boolean get() = status == "error"

    /** The line under the address in Mail (iOS `MailAccountsView`). */
    val detail: CoreText
        get() = when {
            isError -> error?.takeIf { it.isNotBlank() }?.let(CoreText::plain) ?: CoreText.of(Key.MailError)
            auth == "oauth2" -> CoreText.of(if (provider == "outlook") Key.MailOutlook else Key.MailGmail)
            provider == "feishu" -> CoreText.of(Key.MailLark)
            else -> CoreText.plain(imapHost.orEmpty())
        }
}

/** Body of `POST /api/v1/mail/accounts`; servers left out are filled from the domain's preset on the hub. */
@Serializable
data class AddMailAccountRequest(
    val name: String = "",
    val email: String = "",
    val provider: String? = null,
    val auth: String? = null,
    val username: String? = null,
    val password: String? = null,
    @SerialName("imap_host") val imapHost: String? = null,
    @SerialName("imap_port") val imapPort: Int? = null,
    @SerialName("smtp_host") val smtpHost: String? = null,
    @SerialName("smtp_port") val smtpPort: Int? = null,
    @SerialName("smtp_security") val smtpSecurity: String? = null,
)

/** `POST /api/v1/mail/accounts/{id}/test`: 200 either way, `ok: false` + `error` when the login fails. */
@Serializable
data class MailTestResult(val ok: Boolean = false, val error: String? = null)

/** `POST /api/v1/mail/oauth/google/start`: Google's consent URL (PKCE) and the scheme it calls back on. */
@Serializable
data class GoogleStart(
    @SerialName("auth_url") val authUrl: String = "",
    @SerialName("callback_scheme") val callbackScheme: String = "",
    val state: String = "",
)

@Serializable
data class GoogleFinish(val state: String, val code: String? = null, val error: String? = null)

/** `GET /api/v1/mail/oauth/google/config`: whether this hub can start "Sign in with Google" (google.client_id in the manager's manager.json). */
@Serializable
data class GoogleConfig(val configured: Boolean)

/** `GET /api/v1/lark/status` (Settings › Connectors › Lark): whether the hub reaches the Lark gateway and who is signed in there. */
@Serializable
data class LarkStatus(
    val configured: Boolean = false,
    val connected: Boolean = false,
    val name: String? = null,
    val error: String? = null,
    /** Unix seconds of the last real-time event. */
    @SerialName("last_event_at") val lastEventAt: Long? = null,
)

@Serializable
data class LarkAccount(
    val id: String,
    val title: String = "",
    val avatar: String? = null,
    val tenant: String? = null,
    @SerialName("signed_in") val signedIn: Boolean = true,
    val unread: Int = 0,
    val quarantined: Boolean? = null,
)

@Serializable
data class LarkAccounts(
    val accounts: List<LarkAccount> = emptyList(),
    val default: String? = null,
)

/**
 * One of `GET /api/v1/connectors`: a source the hub reads (GitHub…) and how it is doing (docs/protocol/protocol.md "Connector API").
 */
@Serializable
data class HubConnector(
    val id: String,
    val name: String,
    val status: String = "",
    val account: String? = null,
    val via: String? = null,
    val error: String? = null,
    val guide: List<String>? = null,
    @SerialName("last_sync") val lastSync: String? = null,
    @SerialName("last_event") val lastEvent: String? = null,
    val capabilities: List<String>? = null,
    val hosts: List<String>? = null,
    val enabled: Boolean? = null,
    val archive: Boolean? = null,
    val inbox: Boolean? = null,
    /** Whose it is (several managers: the hub's merged list stamps each one, docs/assistant/managers.md); null: the default's. */
    val manager: String? = null,
)

/** What deleting a connector's data removed (`DELETE /api/v1/connectors/{id}/data`, `…/archive`). */
@Serializable
data class ConnectorDataDeleted(
    val chats: Int? = null,
    val people: Int? = null,
    val inbox: Int? = null,
)

/**
 * Settings › Connectors (docs/extensions/connectors.md, docs/extensions/mail.md): managing mail accounts, Google OAuth,
 * Lark gateway Feishu connection status and accounts, and general hub connectors.
 */
interface ConnectorsManagement {
    /** `GET /api/v1/connectors?all=1`. */
    suspend fun connectors(all: Boolean = true): List<HubConnector>

    /** `PUT /api/v1/connectors/{id}/settings` {"enabled": enabled}. */
    suspend fun setConnectorEnabled(id: String, enabled: Boolean): HubConnector

    /** `PUT /api/v1/connectors/{id}/settings` {"archive": on} or {"inbox": on}. */
    suspend fun setConnectorFeed(id: String, feed: String, on: Boolean): HubConnector

    /** `DELETE /api/v1/connectors/{id}/data`. */
    suspend fun deleteConnectorData(id: String): ConnectorDataDeleted

    /** `DELETE /api/v1/connectors/{id}/archive`. */
    suspend fun deleteConnectorArchive(id: String): ConnectorDataDeleted

    // Mail API
    suspend fun mailAccounts(): List<MailAccount>
    suspend fun addMailAccount(req: AddMailAccountRequest): MailAccount
    suspend fun deleteMailAccount(id: String)
    suspend fun testMailAccount(id: String): MailTestResult
    suspend fun googleSignInStart(loginHint: String? = null): GoogleStart
    suspend fun googleSignInFinish(finish: GoogleFinish): MailAccount

    /** `GET /api/v1/mail/oauth/google/config`; null from a hub too old to answer it, which counts as configured. */
    suspend fun googleConfig(): GoogleConfig? = null

    // Lark API
    suspend fun larkStatus(): LarkStatus
    suspend fun larkAccounts(): LarkAccounts?

    /** Settings › Translation's chat picker: `GET /api/v1/lark/pinned` (when the hub has it), then `/api/v1/lark/conversations?scope=chats`. */
    suspend fun larkChatChoices(): List<LarkChatChoice>

    // Any connector with `login` / `threads` (docs/protocol/protocol.md "Connector API"): what the hub hands down, nothing per source.

    /**
     * `POST /api/v1/connectors/{id}/login` `{step, values}`: one sign-in step ([step] empty starts over). The answer has
     * the same shape as a channel's binding (`connector.Prompt`): fields the hub asks for, a QR code or a device code to
     * wait on, or done.
     */
    suspend fun connectorLogin(id: String, step: String, values: Map<String, String>): BindPrompt =
        throw UnsupportedOperationException("Connector sign-in unavailable")

    /** `POST /api/v1/connectors/{id}/logout`: the hub forgets the connector's sign-in. */
    suspend fun connectorLogout(id: String): Unit = throw UnsupportedOperationException("Connector sign-out unavailable")

    /** `GET /api/v1/connectors/{id}/thread?thread=&before=&limit=`: a page of an inbox item's conversation, oldest first. */
    suspend fun connectorThread(id: String, thread: String, before: String? = null, limit: Int = ConnectorThreadPage.Limit): ConnectorThreadPage =
        throw UnsupportedOperationException("Connector threads unavailable")

    /**
     * `POST /api/v1/connectors/{id}/thread/reply` `{thread, action, text, reply_to?}`: a reply through the action the page
     * named ([ConnectorThreadChat.reply]). Never throws for what the hub answered: the outcome says it as it was.
     */
    suspend fun connectorThreadReply(id: String, thread: String, action: String, text: String, replyTo: String?): ConnectorReplyOutcome =
        throw UnsupportedOperationException("Connector replies unavailable")

    // Outlook's "Sign in with Microsoft" (docs/extensions/mail.md "Outlook: Sign In with Microsoft"): the client ID the user registered lives on the hub.

    /** `GET /api/v1/mail/oauth/microsoft/config`. */
    suspend fun microsoftConfig(): MicrosoftConfig = throw UnsupportedOperationException("Outlook sign-in unavailable")

    /** `PUT /api/v1/mail/oauth/microsoft/config` `{client_id}` ("" forgets it). */
    suspend fun setMicrosoftClientId(clientId: String): MicrosoftConfig = throw UnsupportedOperationException("Outlook sign-in unavailable")

    /** `POST /api/v1/mail/oauth/microsoft/start`: Microsoft's sign-in URL and the scheme it calls back on. */
    suspend fun microsoftSignInStart(): GoogleStart = throw UnsupportedOperationException("Outlook sign-in unavailable")

    /** `POST /api/v1/mail/oauth/microsoft/finish`: the callback as it came (an error too: the hub says what to do). */
    suspend fun microsoftSignInFinish(finish: MicrosoftFinish): MailAccount = throw UnsupportedOperationException("Outlook sign-in unavailable")

    // Settings › Lark › "Message display" (iOS `LarkMessageDisplay`): shared with the Lark gateway's web for the signed-in account.

    /** `GET /api/v1/lark/message-display`. */
    suspend fun larkMessageDisplay(): LarkMessageDisplay = throw UnsupportedOperationException("Message display unavailable")

    /** `POST /api/v1/lark/message-display` `{account_id, key, value, expected}`: one switch, from what it was. */
    suspend fun setLarkMessageDisplay(current: LarkMessageDisplay, key: String, value: Boolean): LarkMessageDisplay =
        throw UnsupportedOperationException("Message display unavailable")

    // Settings › Connectors › Messages (iOS `MessagesConnectorView`): one Mac's Messages, read through the hub.

    /** `GET /api/v1/imessage`. */
    suspend fun messagesStatus(): MessagesStatus = throw UnsupportedOperationException("Messages unavailable")

    /** `PUT /api/v1/imessage/settings` `{ai}`: "Hand to Persona". */
    suspend fun setMessagesAi(on: Boolean): MessagesStatus = throw UnsupportedOperationException("Messages unavailable")

    /** `DELETE /api/v1/imessage/data`: what the hub kept (knowledge base, inbox); the Mac's messages stay. */
    suspend fun deleteMessagesData(): ConnectorDataDeleted = throw UnsupportedOperationException("Messages unavailable")

    /** `GET /api/v1/imessage/chats?limit=50&before=`: the Mac's conversations, newest first. */
    suspend fun messagesChats(before: String? = null): MessagesChatsPage = throw UnsupportedOperationException("Messages unavailable")

    /** `GET /api/v1/imessage/chats/{id}/messages?limit=50&before=`: a page of one conversation, oldest first. */
    suspend fun messagesThread(chatId: String, before: String? = null): MessagesPage = throw UnsupportedOperationException("Messages unavailable")
}

@Serializable
data class MicrosoftConfig(
    val configured: Boolean = false,
    @SerialName("client_id") val clientId: String? = null,
    val source: String? = null,
    @SerialName("redirect_uri") val redirectUri: String = "",
)

@Serializable
data class MicrosoftFinish(
    val state: String,
    val code: String? = null,
    val error: String? = null,
    @SerialName("error_description") val errorDescription: String? = null,
)


@Serializable
data class LarkMessageDisplay(
    @SerialName("account_id") val accountId: String = "",
    @SerialName("anti_recall") val antiRecall: Boolean = false,
    @SerialName("show_edit_history") val showEditHistory: Boolean = false,
) {
    /** The switch behind a key of `POST /api/v1/lark/message-display`. */
    fun value(key: String): Boolean? = when (key) {
        "anti_recall" -> antiRecall
        "show_edit_history" -> showEditHistory
        else -> null
    }

    companion object {
        val Hidden = LarkMessageDisplay()
    }
}

/** `GET /api/v1/imessage`: which Mac, whether it may read, the steps there when it may not, "Hand to Persona" (null: an older hub). */
@Serializable
data class MessagesStatus(
    val machine: String = "",
    /** ok | no_permission | not_configured | … */
    val status: String = "",
    val error: String? = null,
    val guide: List<String>? = null,
    val via: String? = null,
    val ai: Boolean? = null,
)

@Serializable
data class MessagesPerson(val handle: String = "", val name: String? = null) {
    val shown: String get() = name?.takeIf { it.isNotEmpty() } ?: handle
}

@Serializable
data class MessagesChat(
    val id: String,
    val name: String = "",
    val group: Boolean? = null,
    val members: List<MessagesPerson> = emptyList(),
    val services: List<String> = emptyList(),
    /** RFC 3339. */
    val last: String = "",
    val preview: String? = null,
    @SerialName("from_me") val fromMe: Boolean? = null,
    val unread: Int? = null,
) {
    val isGroup: Boolean get() = group == true
}

@Serializable
data class MessagesAttachment(
    /** image | video | audio | file */
    val kind: String = "",
    val name: String? = null,
    val mime: String? = null,
    val size: Long? = null,
)

@Serializable
data class MessagesMessage(
    val id: String,
    /** RFC 3339. */
    val at: String = "",
    /** message | event */
    val kind: String = "message",
    @SerialName("from_me") val fromMe: Boolean? = null,
    val sender: MessagesPerson? = null,
    val text: String? = null,
    val service: String? = null,
    val attachments: List<MessagesAttachment>? = null,
    @SerialName("reply_to") val replyTo: String? = null,
    val edited: Boolean? = null,
    val unsent: Boolean? = null,
    val unread: Boolean? = null,
    val undecoded: Boolean? = null,
) {
    val isMine: Boolean get() = fromMe == true
    val isEvent: Boolean get() = kind == "event"
}

@Serializable
data class MessagesChatsPage(val chats: List<MessagesChat> = emptyList(), val more: Boolean? = null, val before: String? = null)

@Serializable
data class MessagesPage(val chat: MessagesChat? = null, val messages: List<MessagesMessage> = emptyList(), val more: Boolean? = null, val before: String? = null)

/** A person in a connector's thread. */
@Serializable
data class ConnectorThreadPerson(val id: String? = null, val name: String = "", val avatar: String? = null)

/** A message's media: drawn as a placeholder of its kind (image, sticker, video, voice, file) with its name and size. */
@Serializable
data class ConnectorThreadMedia(
    val kind: String = "",
    val ref: String? = null,
    val thumb: String? = null,
    val mime: String? = null,
    val name: String? = null,
    val size: Long? = null,
    /** Seconds. */
    val duration: Int? = null,
    val width: Int? = null,
    val height: Int? = null,
    val emoji: String? = null,
)

/** What a message answers (`reply_to`): the quoted message's id, who wrote it and a line of it. */
@Serializable
data class ConnectorThreadQuote(val id: String = "", val from: String? = null, val text: String? = null)

@Serializable
data class ConnectorThreadMessage(
    val id: String,
    /** RFC 3339. */
    val at: String = "",
    val sender: ConnectorThreadPerson = ConnectorThreadPerson(),
    val mine: Boolean = false,
    /** A line about the conversation (someone joined), drawn centred. */
    val event: Boolean = false,
    val text: String? = null,
    val format: String? = null,
    val media: List<ConnectorThreadMedia> = emptyList(),
    @SerialName("reply_to") val replyTo: ConnectorThreadQuote? = null,
    val edited: Boolean = false,
    val url: String? = null,
)

@Serializable
data class ConnectorThreadChat(
    val id: String = "",
    val name: String = "",
    /** dm | group | channel | pr | issue */
    val kind: String = "",
    val url: String? = null,
    val avatar: String? = null,
    /** How many are in a group / subscribe to a channel; the first page only, absent when the hub does not know. */
    val members: Int? = null,
    /** The action that answers here; null: read-only. */
    val reply: String? = null,
) {
    /** A chat app's conversation, drawn as bubbles; a pull request or issue is not (its page shows what the hub collected). */
    val isChat: Boolean get() = kind == "dm" || kind == "group" || kind == "channel"
}

@Serializable
data class ConnectorThreadPage(
    val chat: ConnectorThreadChat = ConnectorThreadChat(),
    val messages: List<ConnectorThreadMessage> = emptyList(),
    /** Goes back one more page; null: the thread's start. */
    val before: String? = null,
) {
    companion object {
        const val Limit = 30
    }
}

/** How a reply in a connector's thread went, as the hub said it. */
sealed interface ConnectorReplyOutcome {
    /** Done; [note] is what the connector answered when it said something (it may not be delivered yet). */
    data class Sent(val note: String?) : ConnectorReplyOutcome
    /** Not sent: not allowed or not approved (403 `declined`), with the hub's words. */
    data class Declined(val reason: String) : ConnectorReplyOutcome
    /** Not sent: anything else (the connector failed, a rate limit, the network), with why. */
    data class Failed(val reason: String) : ConnectorReplyOutcome
}
