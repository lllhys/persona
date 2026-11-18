@file:OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)

package persona.core.contracts

import kotlinx.serialization.KeepGeneratedSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * What the node reads from a session's transcript (GET /api/v1/sessions/{mid}/{sid}/info, docs/protocol/protocol.md):
 * what it runs, how far it has got and where its files are. Counts the transcript has not recorded stay zero.
 */
@KeepGeneratedSerializer
@Serializable(with = SessionInfoSerializer::class)
data class SessionInfo(
    @SerialName("session_id") val sessionId: String = "",
    @SerialName("transcript_path") val transcriptPath: String? = null,
    val cwd: String? = null,
    val model: String? = null,
    val launcher: String? = null,
    @SerialName("git_branch") val gitBranch: String? = null,
    /** The agent's own version (Claude Code, Codex or a compatible CLI). */
    val version: String? = null,
    @SerialName("permission_mode") val permissionMode: String? = null,
    val effort: String? = null,
    @SerialName("context_tokens") val contextTokens: Long = 0,
    @SerialName("output_tokens") val outputTokens: Long = 0,
    val compactions: Int = 0,
    val turns: Int = 0,
    /**
     * Nobody gives its counts (a runtime's session with no transcript on the computer, #8261): the zeros above stand in
     * for older apps and are not shown.
     */
    @SerialName("usage_unknown") val usageUnknown: Boolean = false,
    @WireTime @SerialName("started_at") val startedAt: String? = null,
    @WireTime @SerialName("last_activity") val lastActivity: String? = null,
)

object SessionInfoSerializer : WireSerializer<SessionInfo>(SessionInfo.generatedSerializer())
