package persona.core.contracts

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlin.math.abs
import kotlin.math.roundToInt
import kotlin.math.roundToLong

// The fixed facts of a morning or evening routine, carried by a `brief` feed item. Every section is optional for
// compatibility with older hubs; one malformed row is skipped without discarding the rest of the card.

/** A waiting card, a task to verify, or a question left for a later briefing. */
data class BriefRow(
    val id: String,
    val title: String = "",
    val detail: String? = null,
    val from: String? = null,
    val waitedS: Long = 0,
    val nudged: Boolean = false,
    val askedAt: String? = null,
    val verdict: String? = null,
    val outcome: String? = null,
    val recommend: String? = null,
    val machineId: String? = null,
    val sessionId: String? = null,
) {
    val isDecision: Boolean get() = outcome?.trim() == "decide"
    val take: BriefTake? get() = when {
        isDecision -> if (recommend?.isNotBlank() == true) BriefTake.Recommendation else null
        outcome.isNullOrBlank() -> BriefTake.Accept
        else -> BriefTake.GotIt
    }

    /** Prefer the row's own chat; an older hub's waiting row can use its still-pending card. */
    fun chat(waiting: Boolean, pending: List<Interaction>): SessionKey? {
        if (!machineId.isNullOrBlank() && !sessionId.isNullOrBlank()) return SessionKey(machineId, sessionId)
        if (!waiting) return null
        val card = pending.firstOrNull { it.id == id && it.sessionId.isNotBlank() } ?: return null
        return card.sessionKey
    }
}

enum class BriefTake { Accept, GotIt, Recommendation }

data class BriefDone(val id: String, val title: String = "", val at: String? = null)

data class BriefFailed(
    val id: String,
    val title: String = "",
    val kind: String = "",
    val ref: String? = null,
    val at: String? = null,
    val why: String? = null,
    val skipped: Boolean = false,
)

data class BriefProgress(val id: String, val title: String = "", val stage: String? = null, val stageKind: String? = null)

/** The settings page of this app a recommended mechanism is turned on at. */
enum class BriefSetupPage { SessionTriage, SupervisorModel, StandingAuthorizations }

/**
 * One line of the week's recommended setup (#5085, docs/assistant/assistant.md "Recommended setup"): [text] is the hub's
 * whole line (its count, its cost, where it is turned on), drawn as it came; [id] names the mechanism.
 */
data class BriefSetup(val id: String, val text: String, val count: Int = 0) {
    /**
     * Where this app turns the mechanism on, for those it has a page for: Session triage's three switches, the
     * background supervisor's model purpose (#5805), and standing authorizations (#9247). Null for the others (the
     * remembered kinds of task, the delegate, leaders and the reviews have no page here) and for a mechanism a newer hub names.
     */
    val page: BriefSetupPage? get() = when (id) {
        "triage_decide", "triage_auto_continue", "triage_wake" -> BriefSetupPage.SessionTriage
        "supervisor_tier" -> BriefSetupPage.SupervisorModel
        "standing_authorizations" -> BriefSetupPage.StandingAuthorizations
        else -> null
    }
}

data class BriefBody(
    val title: String = "",
    val hours: Int = 12,
    val waitingUnknown: Boolean = false,
    val waiting: List<BriefRow> = emptyList(),
    val done: List<BriefDone> = emptyList(),
    val doneTotal: Int = 0,
    val failed: List<BriefFailed> = emptyList(),
    val failedTotal: Int = 0,
    val accept: List<BriefRow> = emptyList(),
    val later: List<BriefRow> = emptyList(),
    val laterTotal: Int = 0,
    val progress: List<BriefProgress> = emptyList(),
    val progressTotal: Int = 0,
    /** The recommended-setup lines, on the first briefing of a week that names something; empty otherwise and from an older hub. */
    val setup: List<BriefSetup> = emptyList(),
) {
    val pastMore: Int get() = (doneTotal + failedTotal - done.size - failed.size).coerceAtLeast(0)
    val laterMore: Int get() = (laterTotal - later.size).coerceAtLeast(0)
    val progressMore: Int get() = (progressTotal - progress.size).coerceAtLeast(0)
}

private fun JsonObject.text(name: String): String? =
    (this[name] as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull

private fun JsonObject.whole(name: String): Int? = (this[name] as? JsonPrimitive)?.doubleOrNull
    ?.takeIf { it.isFinite() && abs(it) < 1e12 }?.roundToInt()
private fun JsonObject.long(name: String): Long? = (this[name] as? JsonPrimitive)?.doubleOrNull
    ?.takeIf { it.isFinite() && abs(it) < 1e12 }?.roundToLong()
private fun JsonObject.rows(name: String): List<JsonObject> = (this[name] as? JsonArray).orEmpty().mapNotNull { it as? JsonObject }

private fun briefRow(body: JsonObject): BriefRow? {
    val id = body.text("id")?.takeIf(String::isNotBlank) ?: return null
    return BriefRow(id, body.text("title").orEmpty(), body.text("detail"), body.text("from"), body.long("waited_s") ?: 0,
        (body["nudged"] as? JsonPrimitive)?.contentOrNull == "true", body.text("asked_at"), body.text("verdict"),
        body.text("outcome"), body.text("recommend"), body.text("machine_id"), body.text("session_id"))
}

/** Decode only this item's brief body; another kind never gets a synthetic card. */
val FeedItem.brief: BriefBody? get() {
    if (kind != "brief") return null
    val body = body as? JsonObject ?: return null
    val waiting = body.rows("waiting").mapNotNull(::briefRow)
    val accept = body.rows("accept").mapNotNull(::briefRow)
    val later = body.rows("later").mapNotNull(::briefRow)
    val done = body.rows("done").mapNotNull { row ->
        row.text("id")?.takeIf(String::isNotBlank)?.let { BriefDone(it, row.text("title").orEmpty(), row.text("at")) }
    }
    val failed = body.rows("failed").mapNotNull { row ->
        row.text("id")?.takeIf(String::isNotBlank)?.let {
            BriefFailed(it, row.text("title").orEmpty(), row.text("kind").orEmpty(), row.text("ref"), row.text("at"),
                row.text("why"), (row["skipped"] as? JsonPrimitive)?.contentOrNull == "true")
        }
    }
    val progress = body.rows("progress").mapNotNull { row ->
        row.text("id")?.takeIf(String::isNotBlank)?.let {
            BriefProgress(it, row.text("title").orEmpty(), row.text("stage"), row.text("stage_kind"))
        }
    }
    val setup = body.rows("setup").mapNotNull { row ->
        val id = row.text("id")?.takeIf(String::isNotBlank) ?: return@mapNotNull null
        row.text("text")?.takeIf(String::isNotBlank)?.let { BriefSetup(id, it, row.whole("count") ?: 0) }
    }
    return BriefBody(body.text("title").orEmpty(), body.whole("hours") ?: 12,
        (body["waiting_unknown"] as? JsonPrimitive)?.contentOrNull == "true", waiting, done,
        maxOf(body.whole("done_total") ?: 0, done.size), failed, maxOf(body.whole("failed_total") ?: 0, failed.size),
        accept, later, maxOf(body.whole("later_total") ?: 0, later.size), progress,
        maxOf(body.whole("progress_total") ?: 0, progress.size), setup)
}
