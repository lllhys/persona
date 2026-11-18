package persona.core.domain.tasks

/**
 * Why a task's Agent did not start, in a form the screen can say (#8237, iOS `StartFailureWords`). The hub's reason is
 * often a raw error ("rpc: connection closed"); that stays for the task's details. The hand-off card and the Needs you
 * row say a known cause in a few plain words, keep a sentence the hub already wrote for people, and otherwise say to
 * open the task.
 */
enum class StartCause { ConnectionLost, Offline, TimedOut, NotAllowed, NeedsUpdate }

sealed interface StartFailurePlain {
    data class Known(val cause: StartCause) : StartFailurePlain
    /** A sentence the hub already wrote for people, kept as written. */
    data class Sentence(val text: String) : StartFailurePlain
    /** Nothing readable: the screen says to open the task for the reason. */
    data object Details : StartFailurePlain
}

/** The reason for people. Blank text is [StartFailurePlain.Details]. */
fun startFailurePlain(raw: String): StartFailurePlain {
    val text = raw.trim()
    if (text.isEmpty()) return StartFailurePlain.Details
    startCause(text)?.let { return StartFailurePlain.Known(it) }
    return if (readsAsSentence(text)) StartFailurePlain.Sentence(text) else StartFailurePlain.Details
}

/**
 * The known cause in the raw text, by the words Go, the node and the hub's own sentences use. Null when none is known.
 */
fun startCause(raw: String): StartCause? {
    val text = raw.lowercase()
    val tokens = text.split(Regex("\\P{L}+")).filter { it.isNotEmpty() }.toSet()
    fun has(terms: List<String>) = terms.any { text.contains(it) }
    if (has(listOf("connection closed", "connection reset", "connection lost", "broken pipe", "node exited", "连接断了", "接続が切れ")) // i18n-ignore: matches the hub's own sentences
        || "eof" in tokens) return StartCause.ConnectionLost
    if (has(listOf("deadline exceeded", "timed out", "timeout", "超时", "タイムアウト"))) return StartCause.TimedOut // i18n-ignore: matches the hub's own sentences
    if (has(listOf("permission denied", "unauthorized", "unauthorised", "forbidden", "access denied", "publickey", "authentication failed"))) {
        return StartCause.NotAllowed
    }
    if (has(listOf("unknown method", "version mismatch", "protocol version", "incompatible"))) return StartCause.NeedsUpdate
    if (has(listOf("offline", "not connected", "no route", "unreachable", "connection refused", "no such host", "could not resolve",
            "离线", "不在线", "オフライン"))) return StartCause.Offline // i18n-ignore: matches the hub's own sentences
    return null
}

/** Written for people: Chinese or Japanese, or an English sentence that starts with a capital and ends with a stop. */
fun readsAsSentence(text: String): Boolean {
    if (text.any { ch -> val c = ch.code; c in 0x3040..0x30FF || c in 0x4E00..0x9FFF }) return true
    val first = text.firstOrNull() ?: return false
    return first in 'A'..'Z' && ' ' in text && text.last() in ".!?"
}
