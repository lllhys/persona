package persona.core.contracts

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// The line a card carries once the owner gave the same answer to its kind of card, for the same scope, three times
// within 14 days (#4929, docs/assistant/unified-entry.md "A rule after the same answer three times";
// docs/protocol/protocol.md `RuleSuggestion`). Optional on an open plan, on a to-do's decision and on a question a
// supervisor passed up; a hub from before it sends none, and the card is drawn as before.

/** What a suggested rule would cover; empty for a `delegate` suggestion, whose setting is not per directory. */
@Serializable
data class RuleScope(
    @SerialName("machine_id") val machineId: String? = null,
    /** The computer's name, for display. */
    val machine: String? = null,
    val directory: String? = null,
    /** A standing authorization's action (`merge`, `push`, `deploy-test`) and the conditions it would be granted under. */
    val action: String? = null,
    val conditions: List<String>? = null,
    /** What the standing authorization would keep as the owner's words: their latest answer. */
    val quote: String? = null,
)

/**
 * A suggested rule: [id] names the kind and scope and is what Always and Not now take, [text] is the hub's line in the
 * user's language, [creates] what Always stores (`tasks.trust`, `standing_authorization`, or "" when it stores no rule
 * and [setting] names the setting to turn on instead), [count] how many identical answers were counted.
 */
@Serializable
data class RuleSuggestion(
    val id: String,
    val kind: String = "",
    val text: String = "",
    val scope: RuleScope = RuleScope(),
    val creates: String = "",
    val setting: String? = null,
    val count: Int = 0,
) {
    /** Something to draw and to answer: the hub's line with the id its two actions take. */
    val isShown: Boolean get() = id.isNotBlank() && text.isNotBlank()

    companion object {
        const val TASK_TRUST = "tasks.trust"
        const val STANDING_AUTHORIZATION = "standing_authorization"
    }
}

/** What Always made (`POST /api/v1/assistant/rule-suggestions/{id}/always`): [creates] and [setting] as on [RuleSuggestion]. */
@Serializable
data class RuleSuggestionMade(
    val kind: String = "",
    val creates: String = "",
    val setting: String? = null,
)
