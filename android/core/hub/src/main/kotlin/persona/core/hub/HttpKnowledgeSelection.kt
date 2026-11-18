package persona.core.hub

import kotlinx.coroutines.flow.emptyFlow
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.KnowledgeDetail
import persona.core.contracts.api.KnowledgeDetailResponse
import persona.core.contracts.api.KnowledgeList
import persona.core.contracts.api.KnowledgeSelection
import persona.core.contracts.api.KnowledgeSelectionCandidates
import persona.core.contracts.api.KnowledgeSelectionChange
import persona.core.contracts.api.KnowledgeSelectionUnsupported

/** Knowledge selection on the active pairing's authenticated routes. */
class HttpKnowledgeSelection internal constructor(
    private val request: suspend (path: String, method: String, body: RequestBody?) -> Response,
) : KnowledgeSelection {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body -> connection.request(path, method, body) })

    /** Socket pushes are composed above this HTTP transport. */
    override val changes = emptyFlow<KnowledgeSelectionChange>()

    override suspend fun selection(): JsonObject = selectionCall("GET", null)

    override suspend fun put(document: JsonObject): JsonObject = selectionCall(
        "PUT", document.toString().toRequestBody("application/json".toMediaType()),
    )

    override suspend fun candidates(source: String): KnowledgeSelectionCandidates = try {
        val path = path("kb", "selection", "candidates", query = mapOf("source" to source))
        JsonCodec.decodeFromString(KnowledgeSelectionCandidates.serializer(), call(path))
    } catch (failure: HubFailure) {
        throw unsupportedOn404(failure)
    }

    override suspend fun documents(): KnowledgeList =
        JsonCodec.decodeFromString(KnowledgeList.serializer(), call(path("kb", "docs")))

    override suspend fun document(id: String): KnowledgeDetail =
        JsonCodec.decodeFromString(KnowledgeDetailResponse.serializer(), call(path("kb", "docs", id))).detail

    private suspend fun selectionCall(method: String, body: RequestBody?): JsonObject = try {
        Json.parseToJsonElement(call(SelectionPath, method, body)).jsonObject
    } catch (failure: HubFailure) {
        throw unsupportedOn404(failure)
    }

    private suspend fun call(path: String, method: String = "GET", body: RequestBody? = null): String =
        request(path, method, body).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            it.body?.string().orEmpty()
        }

    private fun unsupportedOn404(failure: HubFailure): Exception =
        if (failure.status == 404) KnowledgeSelectionUnsupported() else failure

    private fun path(vararg segments: String, query: Map<String, String> = emptyMap()): String {
        val url = HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1")
        segments.forEach { url.addPathSegment(it) }
        query.forEach { (key, value) -> url.addQueryParameter(key, value) }
        val built = url.build()
        return built.encodedPath + (built.encodedQuery?.let { "?$it" } ?: "")
    }

    private companion object {
        const val SelectionPath = "/api/v1/kb/selection"
    }
}
