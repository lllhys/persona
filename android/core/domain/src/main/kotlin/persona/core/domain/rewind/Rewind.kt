package persona.core.domain.rewind

import persona.core.contracts.AgentState
import persona.core.contracts.Item
import persona.core.contracts.Session
import persona.core.contracts.api.DeliveryState
import persona.core.contracts.api.RewindTarget

/**
 * "Rewind to here" (#2436, web `lib/rewind.ts`): go back in a Claude session to before one of your messages. The node drives
 * Claude Code's own /rewind in the session's terminal (node/rewind.go), so what the app says is which message: its
 * text, which of the messages with that text it is counting from the end, and how many came after it. Both Claude's
 * list and the transcript are read from the end, so older pages not loaded yet change nothing; the node checks all
 * three against its screen and stops, changing nothing, when they disagree. A session whose node declares
 * capabilities.rewind (a runtime's, a Codex thread) names the message the same way; the runtime or Codex goes back itself.
 */
object Rewind {
    /** One message of yours as Claude lists it: the lines of one transcript entry are one message. */
    data class Prompt(val id: String, val raw: String)

    private fun entryOf(item: Item) = item.id.substringBefore('#')

    /** Your messages in order. Photos alone are a message too (Claude lists them), with no text; a queued one is not yet. */
    fun prompts(items: List<Item>): List<Prompt> {
        val out = ArrayList<Prompt>()
        var entry = ""
        for (item in items) {
            if (item.role != "user" || item.queued == true) continue
            if (!(item.kind == "text" || (item.kind == "image" && item.toolUseId == null))) continue
            if (out.isNotEmpty() && entryOf(item) == entry) {
                if (item.kind == "text" && !item.text.isNullOrEmpty()) {
                    val last = out.last()
                    out[out.lastIndex] = last.copy(raw = if (last.raw.isEmpty()) item.text!! else last.raw + "\n" + item.text)
                }
                continue
            }
            entry = entryOf(item)
            out += Prompt(item.id, if (item.kind == "text") item.text.orEmpty() else "")
        }
        return out
    }

    private fun squash(s: String) = s.replace(Regex("\\s+"), "")

    /** Which message [messageId] (its first item's id) stands for, in the node's words; null when it has no words. */
    fun target(items: List<Item>, messageId: String): RewindTarget? {
        val all = prompts(items)
        val at = all.indexOfFirst { it.id == messageId }
        if (at < 0 || all[at].raw.isBlank()) return null
        val same = squash(all[at].raw)
        return RewindTarget(all[at].raw, 1 + all.drop(at + 1).count { squash(it.raw) == same }, all.size - 1 - at)
    }

    /**
     * Why "Rewind to here" is off for a session right now: [ClaudeOnly] where the node says nothing and it is no Claude in a
     * terminal, [Unsupported] where the node says it can't (capabilities.rewind false).
     */
    enum class Block { ClaudeOnly, Unsupported, Offline, NeedsYou, Working }

    /** Whether any of these outbox messages is still on its way: a failed one (refused, rejected) waits for you, not the session (web `outgoing`). */
    fun outgoing(states: List<DeliveryState>): Boolean = states.any { it != DeliveryState.Rejected && it != DeliveryState.Refused }

    /**
     * Why the session cannot rewind now, null when it can ([busy]: something of yours is on its way). The node's
     * capabilities.rewind wins where it says (a runtime's session or a Codex thread it can rewind, #6294; web `rewindBlock`);
     * without it, only Claude in a terminal.
     */
    fun block(session: Session?, busy: Boolean): Block? {
        if (session == null || session.assistant) return Block.ClaudeOnly
        when (session.capabilities?.rewind) {
            false -> return Block.Unsupported
            null -> if (session.sourceKind != null || (session.agent != null && session.agent != "claude")) return Block.ClaudeOnly
            true -> Unit
        }
        val state = session.agentState
        return when {
            state == AgentState.Offline || session.starting -> Block.Offline
            state == AgentState.NeedsYou || session.pendingInteractions > 0 -> Block.NeedsYou
            busy || state == AgentState.Working -> Block.Working
            else -> null
        }
    }

    /**
     * "Code and conversation" is offered: the node's capabilities.rewind_code where it declares rewind (false where its
     * runtime or Codex puts no files back); a Claude terminal (nothing declared) keeps both (web `rewindCode`).
     */
    fun restoresCode(session: Session?): Boolean = session?.capabilities?.rewindCode ?: (session?.capabilities?.rewind != true)

    /**
     * "Branch to new chat" is offered on a reply that is a transcript line of its own (`<uuid>#<n>`): lines without a uuid are
     * `line<seq>`, which the node cannot find (web `moreActions`).
     */
    fun forkable(itemId: String): Boolean = itemId.contains('#') && !itemId.startsWith("line")
}
