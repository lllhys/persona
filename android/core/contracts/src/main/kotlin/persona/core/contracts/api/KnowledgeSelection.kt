package persona.core.contracts.api

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/**
 * Settings › Knowledge selection (docs/assistant/knowledge.md "Selection"). The selection document stays raw JSON so a
 * whole-document edit cannot discard fields or explicit nulls introduced by a newer hub.
 */
interface KnowledgeSelection {
    /** A saved selection invalidates the editor; HTTP-only implementations receive no pushes by themselves. */
    val changes: Flow<KnowledgeSelectionChange> get() = emptyFlow()

    /** `GET /api/v1/kb/selection`; [KnowledgeSelectionUnsupported] when the hub has no selection routes. */
    suspend fun selection(): JsonObject

    /** `PUT /api/v1/kb/selection`, replacing the whole document and returning the document the hub stored. */
    suspend fun put(document: JsonObject): JsonObject

    /** Keeps one fresh-read/apply/PUT sequence on one connection when the selected hub changes. This is not CAS. */
    suspend fun <T> withPinnedConnection(block: suspend KnowledgeSelection.() -> T): T = block()

    /** `GET /api/v1/kb/selection/candidates?source=`; 404 means this source offers no chat history. */
    suspend fun candidates(source: String): KnowledgeSelectionCandidates

    /** `GET /api/v1/kb/docs`, for choosing a document's per-item selection. */
    suspend fun documents(): KnowledgeList

    /** `GET /api/v1/kb/docs/{id}`, including the source identity in its metadata. */
    suspend fun document(id: String): KnowledgeDetail
}

/** The manager whose selection changed; null is the hub's default manager. */
data class KnowledgeSelectionChange(val manager: String? = null)

/** The hub predates knowledge selection, or the requested source has no history-chat listing. */
class KnowledgeSelectionUnsupported : Exception("Knowledge selection is unavailable")

/** Chats offered for a source's per-item settings. */
@Serializable
data class KnowledgeSelectionCandidates(
    val source: String,
    val cached: Boolean = false,
    @SerialName("listed_at") val listedAt: String? = null,
    val chats: List<KnowledgeSelectionCandidate> = emptyList(),
)

/** One chat and the selection currently applied to it. */
@Serializable
data class KnowledgeSelectionCandidate(
    val id: String,
    val name: String,
    val kind: String,
    /** The source a per-item setting names, which can differ from the connector that listed the chat. */
    val source: String,
    val bot: Boolean = false,
    val muted: Boolean = false,
    val collapsed: Boolean = false,
    val done: Boolean = false,
    val estimate: Int = 0,
    /** The raw per-item setting; null when this chat has none. */
    val item: JsonObject? = null,
    /** `whole`, `personal`, or `none`. */
    val read: String,
    val days: Int = 0,
)
