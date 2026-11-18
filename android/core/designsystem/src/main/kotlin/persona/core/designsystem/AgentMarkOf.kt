package persona.core.designsystem

import persona.core.contracts.Session

/**
 * The mark of a session's agent (#6390), the same reading as iOS `AgentKind` and the web's `sessionAgent`: a Codex
 * thread from an app-server or history is Codex whatever it reports; "" (an older hub) is Claude Code; an agent the app
 * does not know by its id (a runtime's; Paseo's other providers, all "paseo") takes the mark its launcher or its label
 * names ([knownAgentMark], #8143), else it is named by [Session.agentLabel] and drawn as its letter. Persona's own
 * conversations get none.
 */
val Session.agentMark: AgentMarkSpec?
    get() {
        if (assistant) return null
        val surface = when (agentSurface) {
            "desktop" -> AgentSurface.Desktop
            "cli" -> AgentSurface.Cli
            else -> null
        }
        return when {
            isCodex -> AgentMarkSpec(AgentMarkKind.Codex, "Codex", surface)
            agent?.lowercase() in TRAE -> AgentMarkSpec(AgentMarkKind.Trae, "TRAE", surface)
            isClaude -> AgentMarkSpec(AgentMarkKind.Claude, "Claude Code", surface)
            else -> AgentMarkSpec(knownAgentMark(launcher) ?: knownAgentMark(agentLabelHint), agentLabel, surface)
        }
    }

/**
 * The mark the design system has for an agent named [name] (an agent's id, a launcher's name, a provider's label), by
 * its first word in any case: "claude", "codex", "TRAE CLI", "trae-cli", "traecli"; null when it has none, and the
 * agent's letter is drawn. The same names as the web's `MARKED_AGENTS` that Android has a mark for.
 */
fun knownAgentMark(name: String?): AgentMarkKind? {
    val word = name?.trim()?.lowercase()?.takeWhile(Char::isLetterOrDigit)?.takeIf(String::isNotEmpty) ?: return null
    return MARKS[word]
}

private val TRAE = setOf("trae", "trae-cli", "traecli")

private val MARKS = mapOf(
    "claude" to AgentMarkKind.Claude, "claudecode" to AgentMarkKind.Claude,
    "codex" to AgentMarkKind.Codex,
    "trae" to AgentMarkKind.Trae, "traecli" to AgentMarkKind.Trae,
)
