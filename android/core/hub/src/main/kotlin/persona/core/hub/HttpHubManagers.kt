package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.HubManagers
import persona.core.contracts.api.ManagerCode
import persona.core.contracts.api.ManagerRoster
import persona.core.contracts.api.ManagersUnsupported
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/** Settings › Managers over the pairing's connection (docs/assistant/managers.md, `/api/v1/managers*`, answered by the hub itself). */
class HttpHubManagers internal constructor(
    private val request: suspend (path: String, method: String, body: RequestBody?) -> Response,
) : HubManagers {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body -> connection.request(path, method, body) })

    override suspend fun roster(): ManagerRoster = try { read(call(Base, "GET"), ManagerRoster.serializer()) }
    catch (failure: HubFailure) {
        // A hub from before the roster has no such route: its mux answers 404 (without the roster's own codes).
        if (failure.status == 404 && failure.code == null) throw ManagersUnsupported()
        throw failure
    }

    override suspend fun create(name: String): ManagerCode =
        read(call(Base, "POST", buildJsonObject { put("name", name) }), ManagerCode.serializer())

    // OkHttp sends no POST without a body: an empty object.
    override suspend fun newCode(id: String): ManagerCode =
        read(call(path(id, "code"), "POST", JsonObject(emptyMap())), ManagerCode.serializer())

    override suspend fun rename(id: String, name: String): HubManager = manager(call(path(id), "PATCH", buildJsonObject { put("name", name) }))

    override suspend fun makeDefault(id: String): HubManager = manager(call(path(id), "PATCH", buildJsonObject { put("default", true) }))

    // What it answers is not read: the roster says whether the choice is in effect (`hidden`).
    override suspend fun setHide(id: String, hide: Boolean) { call(path(id), "PATCH", buildJsonObject { put("hide", hide) }) }

    override suspend fun delete(id: String) { call(path(id), "DELETE") }

    /** `{manager}`: the manager as it is now. */
    private fun manager(body: String): HubManager = decode {
        JsonCodec.decodeFromJsonElement(HubManager.serializer(), JsonCodec.parseToJsonElement(body).jsonObject.getValue("manager"))
    }

    private fun <T> read(body: String, serializer: KSerializer<T>): T = decode { JsonCodec.decodeFromString(serializer, body) }

    /** Decoding errors are the hub's protocol, not the network's. */
    private inline fun <T> decode(block: () -> T): T = try { block() }
    catch (error: SerializationException) { throw HubFailure(HubError.Protocol, cause = error) }
    catch (error: IllegalArgumentException) { throw HubFailure(HubError.Protocol, cause = error) }
    catch (error: NoSuchElementException) { throw HubFailure(HubError.Protocol, cause = error) }

    // The pairing's connection throws a HubFailure for a status that is not 2xx; a bare request returns it.
    private suspend fun call(path: String, method: String, body: JsonObject? = null): String =
        request(path, method, body?.toString()?.toRequestBody(Json)).use { response ->
            if (!response.isSuccessful) throw HubFailure.from(response)
            response.body?.string().orEmpty()
        }

    /** `/api/v1/managers/{id}[/{more}]` with the id as one escaped path segment (as HubPairingClient does). */
    private fun path(id: String, more: String? = null): String {
        val url = "https://example.com/".toHttpUrl().newBuilder().addPathSegments(Base.trimStart('/')).addPathSegment(id)
        more?.let(url::addPathSegment)
        return url.build().encodedPath
    }

    private companion object {
        const val Base = "/api/v1/managers"
        val Json = "application/json; charset=utf-8".toMediaType()
    }
}
