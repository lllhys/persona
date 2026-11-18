package persona.core.contracts

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.longOrNull

/**
 * One standing authorization as `GET /api/v1/assistant/standing-authorizations` lists it
 * (docs/assistant/unified-entry.md "Standing authorizations"). Every field may be missing or the wrong type;
 * a row without an id is left out, because it cannot be revoked.
 */
data class StandingAuthorization(
    val id: String,
    val machineId: String = "",
    val directory: String = "",
    val actions: List<String> = emptyList(),
    val conditions: List<String> = emptyList(),
    val quote: String = "",
    val source: String = "",
    val grantedAt: String? = null,
    val reversible: Boolean = false,
) {
    /** The folder by its name ("weather"). The detail page shows [directory]. */
    val folderName: String
        get() {
            val name = directory.trimEnd('/').substringAfterLast('/')
            return name.ifEmpty { directory }
        }
}

/** The list, including [revision] after every grant is gone. */
data class StandingAuthorizationList(
    val revision: Long = 0,
    val authorizations: List<StandingAuthorization> = emptyList(),
)

/**
 * Reads a standing-authorization list. A missing or odd field becomes the default; a row that is not an object,
 * or whose id is missing or blank, is dropped. Anything that is not an object is an empty list.
 */
fun decodeStandingAuthorizations(element: JsonElement): StandingAuthorizationList {
    val body = element as? JsonObject ?: return StandingAuthorizationList()
    val revision = (body["revision"] as? JsonPrimitive)?.let { it.longOrNull ?: it.doubleOrNull?.toLong() } ?: 0L
    val rows = (body["authorizations"] as? JsonArray).orEmpty().mapNotNull(::standingAuthorization)
    return StandingAuthorizationList(revision, rows)
}

private fun standingAuthorization(element: JsonElement): StandingAuthorization? {
    val body = element as? JsonObject ?: return null
    val id = body.text("id").takeIf(String::isNotBlank) ?: return null
    return StandingAuthorization(
        id = id,
        machineId = body.text("machine_id"),
        directory = body.text("directory"),
        actions = body.texts("actions"),
        conditions = body.texts("conditions"),
        quote = body.text("quote"),
        source = body.text("source"),
        grantedAt = body.text("granted_at").takeIf(String::isNotBlank),
        reversible = (body["reversible"] as? JsonPrimitive)?.booleanOrNull == true,
    )
}

private fun JsonObject.text(name: String): String =
    (this[name] as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull.orEmpty()

private fun JsonObject.texts(name: String): List<String> =
    (this[name] as? JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.takeIf { item -> item.isString }?.contentOrNull }
        ?: emptyList()
