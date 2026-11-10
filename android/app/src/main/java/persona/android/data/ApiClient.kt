package persona.android.data

import persona.android.AppText
import persona.android.R
import persona.core.contracts.Interaction
import persona.core.contracts.JsonCodec
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.UUID
import java.util.concurrent.TimeUnit

/** An error answer from the hub: the HTTP status, its `error` text and, for refusals, its `code` (docs/protocol/protocol.md). */
class HubException(val status: Int, override val message: String, val code: String? = null, val fields: JsonObject? = null) :
    Exception(message)

/** REST calls to the hub (docs/protocol/protocol.md "REST"). One instance per paired hub. */
class ApiClient(
    val baseUrl: String,
    val token: String,
    http: OkHttpClient = defaultHttpClient(),
    onUnauthorized: () -> Unit = {},
    private val selectedOrigin: (() -> String)? = null,
) {
    companion object {
        private fun defaultHttpClient(): OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(110, TimeUnit.SECONDS)
        .pingInterval(20, TimeUnit.SECONDS)
        .build()
    }

    // Compatibility callers also use this client for streaming uploads.
    val http = http.newBuilder().addInterceptor { chain ->
        // The language the app shows, so the hub answers in it (docs/protocol/i18n.md).
        val request = chain.request().newBuilder()
            .header("Accept-Language", persona.core.contracts.ClientLanguage.tag).build()
        chain.proceed(request).also { response ->
            // Only 401 means the credential is gone; a 403 refuses one request (#4692). A 401 from Cloudflare Access is
            // about the service token, not the pairing: the hub client names it (#3502), so the pairing stays.
            if (response.code == 401 && !persona.core.hub.isCloudflareAccessBlock(response)) onUnauthorized()
        }
    }.build()

    private val jsonType = "application/json; charset=utf-8".toMediaType()

    fun url(path: String, query: Map<String, String> = emptyMap()) =
        (selectedOrigin?.invoke() ?: baseUrl).trimEnd('/').toHttpUrl().newBuilder().apply {
            path.trimStart('/').split('/').forEach { addPathSegment(it) }
            query.forEach { (k, v) -> addQueryParameter(k, v) }
        }.build()

    suspend fun raw(method: String, path: String, body: String? = null, query: Map<String, String> = emptyMap()): String =
        withContext(Dispatchers.IO) {
            val req = Request.Builder().url(url(path, query))
                .header("Authorization", "Bearer $token")
                .header("X-Client-Request-Id", UUID.randomUUID().toString().take(16))
                .method(method, body?.toRequestBody(jsonType) ?: if (method == "GET" || method == "DELETE") null else "".toRequestBody(jsonType))
                .build()
            http.newCall(req).execute().use { resp ->
                val text = resp.body?.string().orEmpty()
                if (!resp.isSuccessful) {
                    val obj = runCatching { JsonCodec.parseToJsonElement(text) as? JsonObject }.getOrNull()
                    val msg = obj?.get("error")?.jsonPrimitive?.content ?: "HTTP ${resp.code}"
                    val rid = resp.header("X-Request-Id")
                    throw HubException(
                        resp.code, if (resp.code >= 500 && rid != null) AppText.get(R.string.app_error_request_id, msg, rid) else msg,
                        obj?.get("code")?.jsonPrimitive?.content, obj,
                    )
                }
                text
            }
        }

    private suspend inline fun <reified T> get(path: String, query: Map<String, String> = emptyMap()): T =
        JsonCodec.decodeFromString(raw("GET", path, query = query))

    @Serializable private data class Interactions(val interactions: List<Interaction> = emptyList())
    suspend fun interactions(): List<Interaction> = get<Interactions>("/api/v1/interactions").interactions
}
