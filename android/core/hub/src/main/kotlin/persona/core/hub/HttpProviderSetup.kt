package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.NewProvider
import persona.core.contracts.api.ProviderPreset
import persona.core.contracts.api.ProviderSetup
import persona.core.contracts.api.HubLauncher
import persona.core.contracts.api.ImportedProvider
import persona.core.contracts.api.LauncherCheck
import persona.core.contracts.api.LauncherCheckAnswer
import persona.core.contracts.api.LauncherImportResult
import persona.core.contracts.api.KeptPurpose
import persona.core.contracts.api.ProviderAdded
import persona.core.contracts.api.ProviderPasteResult
import persona.core.contracts.api.ProviderDiscovery
import persona.core.contracts.api.ProviderProbe
import persona.core.contracts.api.ProviderTest
import persona.core.contracts.api.ProviderUpdate
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/** "Add model provider" over the pairing's connection (docs/integrations/launchers.md "API"). */
class HttpProviderSetup internal constructor(
    /** The last argument is the read timeout in seconds; null keeps the client's 15. */
    private val request: suspend (String, String, RequestBody?, Long?) -> Response,
) : ProviderSetup {
    internal constructor(request: suspend (String, String, RequestBody?) -> Response) :
        this({ path: String, method: String, body: RequestBody?, _: Long? -> request(path, method, body) })
    constructor(connection: OkHttpHubConnection) : this({ path: String, method: String, body: RequestBody?, timeout: Long? ->
        connection.request(path, method, body, readTimeoutSeconds = timeout)
    })

    // A hub from before the extension base has no such route (its GET /launchers/{name} answers 404 for "presets").
    override suspend fun presets(): List<ProviderPreset> = try {
        val presets = JsonCodec.parseToJsonElement(call("/api/v1/launchers/presets", "GET")).jsonObject["presets"] as? JsonArray
        presets?.let { JsonCodec.decodeFromJsonElement(ListSerializer(ProviderPreset.serializer()), it) }.orEmpty()
    } catch (failure: HubFailure) {
        if (failure.status == 404) emptyList() else throw failure
    }

    override suspend fun add(provider: NewProvider) { create(provider) }

    override suspend fun create(provider: NewProvider): ProviderAdded {
        val answer = JsonCodec.parseToJsonElement(
            call("/api/v1/launchers", "POST", JsonCodec.encodeToString(NewProvider.serializer(), provider).toRequestBody(json)))
            as? JsonObject ?: JsonObject(emptyMap())
        val launcher = answer["launcher"] as? JsonObject
        fun text(o: JsonObject?, key: String) = (o?.get(key) as? JsonPrimitive)?.contentOrNull?.takeIf { it.isNotBlank() }
        return ProviderAdded(
            name = text(launcher, "name") ?: provider.name,
            displayName = text(launcher, "display_name"),
            // An older hub, or none asked: no assigned at all (not an empty list, which says nothing was changed).
            assigned = (answer["assigned"] as? JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.contentOrNull },
            kept = (answer["kept"] as? JsonArray)?.let { JsonCodec.decodeFromJsonElement(ListSerializer(KeptPurpose.serializer()), it) }.orEmpty(),
        )
    }

    override suspend fun test(probe: ProviderProbe): ProviderTest =
        JsonCodec.decodeFromString(ProviderTest.serializer(), call("/api/v1/launchers/test", "POST", probe.body(), PROBE_READ_TIMEOUT_SECONDS))

    override suspend fun discover(probe: ProviderProbe): ProviderDiscovery =
        JsonCodec.decodeFromString(ProviderDiscovery.serializer(), call("/api/v1/launchers/discover", "POST", probe.copy(model = null).body(), PROBE_READ_TIMEOUT_SECONDS))

    override suspend fun parse(text: String): ProviderPasteResult {
        val body = buildJsonObject { put("text", text) }.toString().toRequestBody(json)
        return JsonCodec.decodeFromString(ProviderPasteResult.serializer(), call("/api/v1/launchers/import/parse", "POST", body, IMPORT_READ_TIMEOUT_SECONDS))
    }

    private fun ProviderProbe.body(): RequestBody {
        fun String?.kept() = this?.trim()?.takeIf { it.isNotEmpty() }
        // Headers go as typed, "" too: it means none for this request over the stored ones.
        val trimmed = ProviderProbe(preset.kept(), apiKey.kept(), baseUrl.kept(), auth.kept(), model.kept(), name.kept(), headers?.trim(), api.kept())
        return JsonCodec.encodeToString(ProviderProbe.serializer(), trimmed).toRequestBody(json)
    }

    override suspend fun update(name: String, changes: ProviderUpdate) {
        call(path(name), "PUT", JsonCodec.encodeToString(ProviderUpdate.serializer(), changes).toRequestBody(json))
    }

    override suspend fun launchers(): List<HubLauncher> {
        val list = JsonCodec.parseToJsonElement(call("/api/v1/launchers", "GET")).jsonObject["launchers"] as? JsonArray
        return list?.let { JsonCodec.decodeFromJsonElement(ListSerializer(HubLauncher.serializer()), it) }.orEmpty()
    }

    override suspend fun launcher(name: String): HubLauncher {
        val one = JsonCodec.parseToJsonElement(call(path(name), "GET")).jsonObject["launcher"] ?: throw HubFailure(HubError.Protocol)
        return JsonCodec.decodeFromJsonElement(HubLauncher.serializer(), one)
    }

    override suspend fun remove(name: String) { call(path(name), "DELETE") }

    override suspend fun restore(name: String) { call(path(name, "restore"), "POST", "{}".toRequestBody(json)) }

    override suspend fun check(name: String, machine: String?): LauncherCheckAnswer {
        val url = HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1/launchers").addPathSegment(name)
            .addPathSegment("check").apply { machine?.takeIf { it.isNotBlank() }?.let { addQueryParameter("machine", it) } }.build()
        val target = url.encodedPath + (url.encodedQuery?.let { "?$it" } ?: "")
        val answer = JsonCodec.parseToJsonElement(call(target, "POST", "{}".toRequestBody(json), CHECK_READ_TIMEOUT_SECONDS)).jsonObject
        val check = answer["check"] ?: throw HubFailure(HubError.Protocol)
        return LauncherCheckAnswer((answer["machine"] as? JsonPrimitive)?.contentOrNull?.takeIf { it.isNotBlank() },
            JsonCodec.decodeFromJsonElement(LauncherCheck.serializer(), check))
    }

    override suspend fun importPreview(machine: String, alias: String?): List<ImportedProvider> {
        val url = HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1/launchers/import")
            .addQueryParameter("machine", machine).apply { alias?.takeIf { it.isNotBlank() }?.let { addQueryParameter("alias", it) } }.build()
        val list = JsonCodec.parseToJsonElement(call("${url.encodedPath}?${url.encodedQuery}", "GET", timeout = IMPORT_READ_TIMEOUT_SECONDS)).jsonObject["providers"] as? JsonArray
        return list?.let { JsonCodec.decodeFromJsonElement(ListSerializer(ImportedProvider.serializer()), it) }.orEmpty()
    }

    override suspend fun import(machine: String, providers: List<String>, alias: String?): LauncherImportResult {
        val body = buildJsonObject {
            put("machine", machine)
            put("providers", JsonArray(providers.map(::JsonPrimitive)))
            alias?.takeIf { it.isNotBlank() }?.let { put("alias", it) }
        }
        return JsonCodec.decodeFromString(LauncherImportResult.serializer(), call("/api/v1/launchers/import", "POST", body.toString().toRequestBody(json), IMPORT_READ_TIMEOUT_SECONDS))
    }

    private fun path(name: String, vararg more: String): String {
        require(name.isNotBlank())
        return HttpUrl.Builder().scheme("https").host("example.com").addPathSegments("api/v1/launchers").addPathSegment(name)
            .apply { more.forEach(::addPathSegment) }.build().encodedPath
    }

    private suspend fun call(
        path: String, method: String, body: RequestBody? = null,
        timeout: Long? = null,
    ): String =
        request(path, method, body, timeout).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            it.body?.string().orEmpty()
        }

    private companion object {
        /** "Test connection" / "Fetch model list": the hub gives the gateway 30 s. */
        const val PROBE_READ_TIMEOUT_SECONDS = 40L
        /** "Test availability": the hub gives the launcher's CLI 100 s to answer (as iOS). */
        const val CHECK_READ_TIMEOUT_SECONDS = 110L
        /** ~/.zshrc preview and import: the hub gives each call to the machine 30 s (as iOS). */
        const val IMPORT_READ_TIMEOUT_SECONDS = 45L
        val json = "application/json; charset=utf-8".toMediaType()
    }
}
