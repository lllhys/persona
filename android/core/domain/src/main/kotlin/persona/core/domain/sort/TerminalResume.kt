package persona.core.domain.sort

import persona.core.contracts.Machine
import persona.core.contracts.Session

/**
 * Resume in Terminal (#5521 / #5565, docs/integrations/engines.md "Agents run in a terminal"): what an Agent chat's ⋯
 * offers for an Agent that isn't running. Same rule as iOS `Session.terminalResume(on:)` and web `terminalResume`
 * (`web/src/lib/agent-resume.ts`).
 */
enum class TerminalResume {
    /** A stopped Agent: the node marks one so only where a terminal engine can host the resume. */
    Available,
    /** Not running, on a computer that enables no terminal engine (the empty engine set, #3302): offered disabled, saying so. */
    NoEngine,
    /**
     * Not offered: it runs (in Persona's terminal or in one of its own), it starts, an assistant conversation, a desktop
     * app's session, another source, or a row from an older node, which does not say.
     */
    Hidden,
}

/**
 * A stopped Agent (#4754): its conversation and terminal stay, it takes no messages; Resume on its terminal page runs it
 * again. A Codex thread nothing has open is marked the same way (#5521), though its row keeps its Codex history source.
 * Same as web `isStoppedAgent` (status `offline` or `interrupted` only; Android keeps the raw wire status).
 */
val Session.isStoppedAgent: Boolean
    get() {
        if (assistant || (!sourceKind.isNullOrEmpty() && sourceKind != "codex_history")) return false
        if (status != "offline" && status != "interrupted") return false
        val c = capabilities ?: return false
        return c.read && c.terminal && !c.send
    }

/** Whether the chat's ⋯ offers to bring [session] back in a terminal on [machine]. */
fun terminalResume(session: Session?, machine: Machine?): TerminalResume {
    if (session == null || session.assistant || session.starting) return TerminalResume.Hidden
    if (session.status != "offline" && session.status != "interrupted") return TerminalResume.Hidden
    val agentRow = (session.sourceKind.isNullOrEmpty() || session.sourceKind == "codex_history") &&
        (session.agent.isNullOrEmpty() || session.agent == "claude" || session.agent == "codex" || session.agent == "traex")
    if (agentRow && session.agentSurface != "desktop" && machine?.engineHosts?.isEmpty() == true) return TerminalResume.NoEngine
    return if (session.isStoppedAgent) TerminalResume.Available else TerminalResume.Hidden
}
