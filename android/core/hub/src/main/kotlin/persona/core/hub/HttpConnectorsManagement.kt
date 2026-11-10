package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

class HttpConnectorsManagement internal constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
) : ConnectorsManagement {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body ->
        connection.request(path, method, body)
    })

    override suspend fun connectors(all: Boolean): List<HubConnector> {
        val path = if (all) "/api/v1/connectors?all=1" else "/api/v1/connectors"
        // Only an older hub without the list (404) has none; any other failure is the page's error to show (#4814).
        val text = try { call(path, "GET") } catch (failure: HubFailure) { if (failure.status == 404) return emptyList() else throw failure }
        val body = parse(text) as? JsonObject ?: return emptyList()
        val list = body["connectors"] as? JsonArray ?: return emptyList()
        return decode { JsonCodec.decodeFromJsonElement(ListSerializer(HubConnector.serializer()), list) }
    }

    override suspend fun setConnectorEnabled(id: String, enabled: Boolean): HubConnector {
        val body = buildJsonObject { put("enabled", enabled) }
        return decode { JsonCodec.decodeFromJsonElement(HubConnector.serializer(), parse(call("/api/v1/connectors/$id/settings", "PUT", body))) }
    }

    override suspend fun setConnectorFeed(id: String, feed: String, on: Boolean): HubConnector {
        val body = buildJsonObject { put(feed, on) }
        return decode { JsonCodec.decodeFromJsonElement(HubConnector.serializer(), parse(call("/api/v1/connectors/$id/settings", "PUT", body))) }
    }

    override suspend fun deleteConnectorData(id: String): ConnectorDataDeleted =
        read(call("/api/v1/connectors/$id/data", "DELETE"), ConnectorDataDeleted.serializer())

    override suspend fun deleteConnectorArchive(id: String): ConnectorDataDeleted =
        read(call("/api/v1/connectors/$id/archive", "DELETE"), ConnectorDataDeleted.serializer())

    override suspend fun mailAccounts(): List<MailAccount> {
        val body = parse(call("/api/v1/mail/accounts", "GET")) as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val list = body["accounts"] as? JsonArray ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(ListSerializer(MailAccount.serializer()), list) }
    }

    override suspend fun addMailAccount(req: AddMailAccountRequest): MailAccount {
        val payload = JsonCodec.encodeToString(AddMailAccountRequest.serializer(), req)
        val body = parse(callRaw("/api/v1/mail/accounts", "POST", payload)) as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val account = body["account"] ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(MailAccount.serializer(), account) }
    }

    override suspend fun deleteMailAccount(id: String) {
        call("/api/v1/mail/accounts/$id", "DELETE")
    }

    override suspend fun testMailAccount(id: String): MailTestResult =
        read(callRaw("/api/v1/mail/accounts/$id/test", "POST", "{}"), MailTestResult.serializer())

    override suspend fun googleSignInStart(loginHint: String?): GoogleStart {
        val payload = if (loginHint.isNullOrBlank()) "{}" else buildJsonObject { put("login_hint", loginHint) }.toString()
        return read(callRaw("/api/v1/mail/oauth/google/start", "POST", payload), GoogleStart.serializer())
    }

    override suspend fun googleSignInFinish(finish: GoogleFinish): MailAccount {
        val payload = JsonCodec.encodeToString(GoogleFinish.serializer(), finish)
        val body = parse(callRaw("/api/v1/mail/oauth/google/finish", "POST", payload)) as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val account = body["account"] ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(MailAccount.serializer(), account) }
    }

    override suspend fun googleConfig(): GoogleConfig? = try {
        read(call("/api/v1/mail/oauth/google/config", "GET"), GoogleConfig.serializer())
    } catch (failure: HubFailure) {
        if (failure.status == 404) null else throw failure
    }

    override suspend fun larkStatus(): LarkStatus =
        read(call("/api/v1/lark/status", "GET"), LarkStatus.serializer())

    override suspend fun larkAccounts(): LarkAccounts? = try {
        read(call("/api/v1/lark/accounts", "GET"), LarkAccounts.serializer())
    } catch (_: Exception) {
        null
    }

    override suspend fun larkChatChoices(): List<LarkChatChoice> {
        fun rows(body: String) = ((parse(body) as? JsonObject)?.get("conversations") as? JsonArray).orEmpty().mapNotNull { row ->
            val o = row as? JsonObject ?: return@mapNotNull null
            fun text(key: String) = (o[key] as? JsonPrimitive)?.contentOrNull?.takeIf(String::isNotBlank)
            val id = text("id") ?: return@mapNotNull null
            LarkChatChoice(id, text("name") ?: id, text("avatar"), (o["is_group"] as? JsonPrimitive)?.booleanOrNull ?: true)
        }
        // Pinned chats are optional (an older hub has none); the recent ones are not.
        val pinned = try { rows(call("/api/v1/lark/pinned", "GET")) } catch (_: HubFailure) { emptyList() }
        val recent = rows(call("/api/v1/lark/conversations?scope=chats&limit=60", "GET"))
        return (pinned + recent).distinctBy { it.id }
    }

    override suspend fun connectorLogin(id: String, step: String, values: Map<String, String>): BindPrompt {
        val body = buildJsonObject {
            put("step", step)
            putJsonObject("values") { values.forEach { (key, value) -> put(key, value) } }
        }
        return read(call("${connectorPath(id)}/login", "POST", body), BindPrompt.serializer())
    }

    override suspend fun connectorLogout(id: String) {
        call("${connectorPath(id)}/logout", "POST")
    }

    override suspend fun connectorThread(id: String, thread: String, before: String?, limit: Int): ConnectorThreadPage {
        val query = buildList {
            add("thread=${encode(thread)}")
            before?.takeIf { it.isNotEmpty() }?.let { add("before=${encode(it)}") }
            add("limit=$limit")
        }.joinToString("&")
        return read(call("${connectorPath(id)}/thread?$query", "GET"), ConnectorThreadPage.serializer())
    }

    override suspend fun connectorThreadReply(id: String, thread: String, action: String, text: String, replyTo: String?): ConnectorReplyOutcome {
        val body = buildJsonObject {
            put("thread", thread)
            put("action", action)
            put("text", text)
            replyTo?.takeIf { it.isNotEmpty() }?.let { put("reply_to", it) }
        }
        return try {
            val answer = parse(call("${connectorPath(id)}/thread/reply", "POST", body)) as? JsonObject
            ConnectorReplyOutcome.Sent(replyNote(answer?.get("result")))
        } catch (failure: HubFailure) {
            val reason = failure.detail?.takeIf { it.isNotBlank() } ?: failure.message
            if (failure.status == 403 && failure.code == "declined") ConnectorReplyOutcome.Declined(reason) else ConnectorReplyOutcome.Failed(reason)
        }
    }

    override suspend fun microsoftConfig(): MicrosoftConfig =
        read(call("/api/v1/mail/oauth/microsoft/config", "GET"), MicrosoftConfig.serializer())

    override suspend fun setMicrosoftClientId(clientId: String): MicrosoftConfig =
        read(call("/api/v1/mail/oauth/microsoft/config", "PUT", buildJsonObject { put("client_id", clientId) }), MicrosoftConfig.serializer())

    override suspend fun microsoftSignInStart(): GoogleStart =
        read(callRaw("/api/v1/mail/oauth/microsoft/start", "POST", "{}"), GoogleStart.serializer())

    override suspend fun microsoftSignInFinish(finish: MicrosoftFinish): MailAccount {
        val payload = JsonCodec.encodeToString(MicrosoftFinish.serializer(), finish)
        val body = parse(callRaw("/api/v1/mail/oauth/microsoft/finish", "POST", payload)) as? JsonObject ?: throw HubFailure(HubError.Protocol)
        val account = body["account"] ?: throw HubFailure(HubError.Protocol)
        return decode { JsonCodec.decodeFromJsonElement(MailAccount.serializer(), account) }
    }

    override suspend fun larkMessageDisplay(): LarkMessageDisplay =
        read(call("/api/v1/lark/message-display", "GET"), LarkMessageDisplay.serializer())

    override suspend fun setLarkMessageDisplay(current: LarkMessageDisplay, key: String, value: Boolean): LarkMessageDisplay {
        val expected = requireNotNull(current.value(key)) { "Unknown message display key" }
        val body = buildJsonObject {
            put("account_id", current.accountId); put("key", key); put("value", value); put("expected", expected)
        }
        return read(call("/api/v1/lark/message-display", "POST", body), LarkMessageDisplay.serializer())
    }

    override suspend fun messagesStatus(): MessagesStatus = read(call("/api/v1/imessage", "GET"), MessagesStatus.serializer())

    override suspend fun setMessagesAi(on: Boolean): MessagesStatus =
        read(call("/api/v1/imessage/settings", "PUT", buildJsonObject { put("ai", on) }), MessagesStatus.serializer())

    override suspend fun deleteMessagesData(): ConnectorDataDeleted =
        read(call("/api/v1/imessage/data", "DELETE"), ConnectorDataDeleted.serializer())

    override suspend fun messagesChats(before: String?): MessagesChatsPage {
        val query = "limit=50" + (before?.takeIf { it.isNotEmpty() }?.let { "&before=${encode(it)}" } ?: "")
        return read(call("/api/v1/imessage/chats?$query", "GET"), MessagesChatsPage.serializer())
    }

    override suspend fun messagesThread(chatId: String, before: String?): MessagesPage {
        val query = "limit=50" + (before?.takeIf { it.isNotEmpty() }?.let { "&before=${encode(it)}" } ?: "")
        return read(call("/api/v1/imessage/chats/${encode(chatId)}/messages?$query", "GET"), MessagesPage.serializer())
    }

    private fun <T> read(body: String, serializer: KSerializer<T>): T =
        decode { JsonCodec.decodeFromJsonElement(serializer, parse(body)) }

    private fun parse(body: String): JsonElement = try {
        JsonCodec.parseToJsonElement(body)
    } catch (error: SerializationException) {
        throw HubFailure(HubError.Protocol, cause = error)
    }

    private inline fun <T> decode(block: () -> T): T = try {
        block()
    } catch (error: IllegalArgumentException) {
        throw HubFailure(HubError.Protocol, cause = error)
    }

    private suspend fun call(path: String, method: String, body: JsonObject? = null): String =
        callRaw(path, method, body?.toString())

    private suspend fun callRaw(path: String, method: String, bodyText: String? = null): String = try {
        val reqBody = bodyText?.toRequestBody(json) ?: if (method == "POST" || method == "PUT") "".toRequestBody(json) else null
        request(path, method, reqBody).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string().orEmpty()
        }
    } catch (failure: HubFailure) {
        throw failure
    }

    private companion object {
        val json = "application/json; charset=utf-8".toMediaType()

        fun encode(value: String): String = java.net.URLEncoder.encode(value, "UTF-8").replace("+", "%20")

        fun connectorPath(id: String) = "/api/v1/connectors/${encode(id)}"

        /** The connector's answer to a reply, when it is words (a JSON string, or an object's `message` / `text`). */
        fun replyNote(result: JsonElement?): String? = when (result) {
            is JsonPrimitive -> result.contentOrNull
            is JsonObject -> listOf("message", "text", "summary").firstNotNullOfOrNull { (result[it] as? JsonPrimitive)?.contentOrNull }
            else -> null
        }?.trim()?.takeIf { it.isNotEmpty() }
    }
}
