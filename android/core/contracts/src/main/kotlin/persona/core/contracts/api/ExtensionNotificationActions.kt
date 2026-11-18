package persona.core.contracts.api

import java.security.SecureRandom
import persona.core.contracts.generated.ExtensionNotifyAction

const val EXTENSION_NOTIFICATION_REMOTE_INPUT = "persona.extension.notification.text"

/** The authenticated pairing, hub-group namespace and manager a notification action belongs to. */
data class ExtensionNotificationScope(
    val pairingScope: String,
    val namespace: String?,
    val manager: String,
)

/** One opaque capability. The action and its input never appear in a public deep link. */
data class ExtensionNotificationActionRecord(
    val token: String,
    val tag: String,
    val extension: String,
    val ui: String?,
    val state: String?,
    val action: ExtensionNotifyAction?,
    val scope: ExtensionNotificationScope,
    val createdAt: Long,
    val dedupeKey: String,
)

data class ExtensionNotificationInspection(
    val record: ExtensionNotificationActionRecord,
    val status: ExtensionNotificationInspectionStatus,
)

enum class ExtensionNotificationInspectionStatus { Current, Expired, WrongScope }

sealed interface ExtensionNotificationClaim {
    data class Claimed(val record: ExtensionNotificationActionRecord, val values: Map<String, String>) : ExtensionNotificationClaim
    data class AlreadyHandled(val record: ExtensionNotificationActionRecord) : ExtensionNotificationClaim
    data class Expired(val record: ExtensionNotificationActionRecord) : ExtensionNotificationClaim
    data class WrongScope(val record: ExtensionNotificationActionRecord) : ExtensionNotificationClaim
    data class Invalid(val record: ExtensionNotificationActionRecord?) : ExtensionNotificationClaim
    data object Missing : ExtensionNotificationClaim
}

/**
 * Process-local, bounded notification capabilities. Registering a replacement for the same alert retires its old tokens.
 * Claims are atomic; separate buttons of one alert have separate dedupe keys.
 */
class ExtensionNotificationActionStore(
    private val capacity: Int = 64,
    private val ttlMillis: Long = 900_000,
    private val dedupeMillis: Long = 300_000,
    private val now: () -> Long = System::currentTimeMillis,
    private val token: () -> String = { randomToken() },
    currentScope: () -> ExtensionNotificationScope? = { null },
) {
    private val lock = Any()
    private val records = LinkedHashMap<String, ExtensionNotificationActionRecord>()
    private val claimed = HashSet<String>()
    private val handled = LinkedHashMap<String, Long>()
    private var scopeReader = currentScope

    init {
        require(capacity > 0 && ttlMillis > 0 && dedupeMillis >= 0)
    }

    fun register(
        tag: String,
        extension: String,
        ui: String?,
        state: String?,
        action: ExtensionNotifyAction?,
        scope: ExtensionNotificationScope,
        currentScope: () -> ExtensionNotificationScope? = scopeReader,
    ): ExtensionNotificationActionRecord? = registerBatch(tag, extension, ui, state, listOf(action), scope, currentScope)?.singleOrNull()

    /** Retires every capability for the exact system notification tag, including degraded replacements. */
    fun retire(tag: String): Unit = synchronized(lock) {
        retireLocked(tag)
    }

    private fun retireLocked(tag: String) {
        records.entries.removeAll { it.value.tag == tag }
        claimed.retainAll(records.keys)
    }

    /** Validates and installs a body plus its buttons as one replacement; no earlier button is retired by a later one. */
    fun registerBatch(
        tag: String,
        extension: String,
        ui: String?,
        state: String?,
        actions: List<ExtensionNotifyAction?>,
        scope: ExtensionNotificationScope,
        currentScope: () -> ExtensionNotificationScope? = scopeReader,
    ): List<ExtensionNotificationActionRecord>? = synchronized(lock) {
        scopeReader = currentScope
        // The system replaces this exact tag even when the new payload is invalid or has no actions.
        retireLocked(tag)
        val declared = actions.filterNotNull()
        if (actions.isEmpty() || declared.size > 3 || declared.map { it.id }.toSet().size != declared.size ||
            !scope.valid() || currentScope() != scope || tag.isBlank() || tag.length > 256 ||
            !extension.safeSegment() || (ui != null && !ui.safeSegment()) || state.utf8Size() > 16_384 ||
            declared.any { ui == null || !it.valid() }) return@synchronized null
        val createdAt = now()
        val fresh = mutableSetOf<String>()
        val batch = actions.map { action ->
            var value = token()
            while (value in records || value in fresh || !value.validToken()) value = token()
            fresh += value
            ExtensionNotificationActionRecord(value, tag, extension, ui, state, action, scope, createdAt,
                listOf(scope.pairingScope, scope.namespace.orEmpty(), scope.manager, tag, action?.id.orEmpty())
                    .joinToString("|") { "${it.length}:$it" })
        }
        handled.entries.removeAll { createdAt - it.value >= dedupeMillis }
        batch.forEach { records[it.token] = it }
        while (records.size > capacity) records.remove(records.entries.first().key)
        batch
    }

    fun peek(token: String): ExtensionNotificationActionRecord? = synchronized(lock) { records[token] }

    fun inspect(token: String): ExtensionNotificationInspection? = synchronized(lock) {
        val record = records[token] ?: return@synchronized null
        ExtensionNotificationInspection(record, inspectionStatus(record))
    }

    fun claim(token: String, actionId: String, values: Map<String, String>): ExtensionNotificationClaim = synchronized(lock) {
        val record = records[token] ?: return@synchronized ExtensionNotificationClaim.Missing
        when (inspectionStatus(record)) {
            ExtensionNotificationInspectionStatus.Expired -> return@synchronized ExtensionNotificationClaim.Expired(record)
            ExtensionNotificationInspectionStatus.WrongScope -> return@synchronized ExtensionNotificationClaim.WrongScope(record)
            ExtensionNotificationInspectionStatus.Current -> Unit
        }
        val action = record.action
        if (action == null || action.id != actionId || !values.validFor(action)) {
            return@synchronized ExtensionNotificationClaim.Invalid(record)
        }
        if (token in claimed || handled[record.dedupeKey]?.let { now() - it < dedupeMillis } == true) {
            return@synchronized ExtensionNotificationClaim.AlreadyHandled(record)
        }
        claimed += token
        handled[record.dedupeKey] = now()
        while (handled.size > capacity * 2) handled.remove(handled.entries.first().key)
        ExtensionNotificationClaim.Claimed(record, if (action.input == null) emptyMap() else mapOf("text" to values["text"].orEmpty()))
    }

    private fun inspectionStatus(record: ExtensionNotificationActionRecord): ExtensionNotificationInspectionStatus = when {
        now() - record.createdAt >= ttlMillis -> ExtensionNotificationInspectionStatus.Expired
        scopeReader() != record.scope -> ExtensionNotificationInspectionStatus.WrongScope
        else -> ExtensionNotificationInspectionStatus.Current
    }

    private fun ExtensionNotificationScope.valid(): Boolean {
        val parts = pairingScope.split('/')
        return parts.size == 2 && parts.all { it.safeSegment() } && namespace?.safeSegment() != false && manager.safeSegment()
    }

    private fun ExtensionNotifyAction.valid(): Boolean = id.isNotEmpty() && id !in ReservedActionIds &&
        id.codePointCount(0, id.length) <= 128 && title.isNotBlank() && title.codePointCount(0, title.length) <= 40 &&
        (role == null || role == "normal" || role == "destructive") && input?.placeholder.utf8Size() <= 16_384

    private fun Map<String, String>.validFor(action: ExtensionNotifyAction): Boolean = when {
        action.input == null -> isEmpty()
        else -> keys == setOf("text") && this["text"].utf8Size() <= 16_384
    }

    companion object {
        val shared = ExtensionNotificationActionStore()
        private val secureRandom = SecureRandom()
        private val ReservedActionIds = setOf("default", "dismiss",
            "com.apple.UNNotificationDefaultActionIdentifier", "com.apple.UNNotificationDismissActionIdentifier")

        private fun randomToken(): String {
            val bytes = ByteArray(16)
            secureRandom.nextBytes(bytes)
            return bytes.joinToString("") { "%02x".format(it.toInt() and 0xff) }
        }
    }
}

private fun String?.utf8Size(): Int = this?.toByteArray(Charsets.UTF_8)?.size ?: 0
private fun String.safeSegment(): Boolean = isNotEmpty() && length <= 128 && this != "." && this != ".." &&
    all { it in 'a'..'z' || it in 'A'..'Z' || it in '0'..'9' || it == '.' || it == '_' || it == '-' }
private fun String.validToken(): Boolean = length == 32 && all { it in '0'..'9' || it in 'a'..'f' }
