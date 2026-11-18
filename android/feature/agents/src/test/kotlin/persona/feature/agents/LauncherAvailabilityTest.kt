package persona.feature.agents

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.AgentLauncher
import persona.core.contracts.api.LauncherHealth
import persona.core.contracts.api.UnavailableLauncher

/** Blocked launchers in New Agent (#4530), as iOS `LauncherHealthTests`: what is grayed, why, and where the form starts. */
class LauncherAvailabilityTest {
    private fun launcher(name: String, state: String? = null, kind: String? = null, error: String? = null) =
        AgentLauncher(name, health = state?.let { LauncherHealth(it, kind, error) })

    @Test fun healthIsReadFromTheLaunchersList() {
        val json = Json { ignoreUnknownKeys = true }
        val l = json.decodeFromString<AgentLauncher>("""{"name":"work_gw","model":"glm","health":{"state":"blocked","kind":"not_in_terminal",
            "checked_at":"2026-11-11T10:00:00Z","error":"bash: work_gw: command not found","resets_at":"2026-11-11T16:00:00Z","x":1}}""")
        assertEquals(LauncherHealth("blocked", "not_in_terminal", "bash: work_gw: command not found", "2026-11-11T10:00:00Z", "2026-11-11T16:00:00Z"), l.health)
        assertNull(json.decodeFromString<AgentLauncher>("""{"name":"claude"}""").health)
    }

    @Test fun onlyBlockedIsGrayedWithOneLinePerClass() {
        for (state in listOf("ok", "working", "failing", "repairing", "")) assertNull(state, launcher("a", state, "auth").block)
        assertNull(launcher("a").block)
        val reasons = mapOf("not_in_terminal" to R.string.agents_launcher_blocked_not_in_terminal, "auth" to R.string.agents_launcher_blocked_auth,
            "model_route" to R.string.agents_launcher_blocked_model_route, "cli_outdated" to R.string.agents_launcher_blocked_cli_outdated,
            "timeout" to R.string.agents_launcher_blocked_timeout, "weird" to R.string.agents_launcher_blocked_other, "" to R.string.agents_launcher_blocked_other)
        for ((kind, reason) in reasons) assertEquals(kind, reason, launcher("a", "blocked", kind).block!!.reason)
        assertEquals(R.string.agents_launcher_blocked_other, launcher("a", "blocked").block!!.reason)
        val terminal = launcher("a", "blocked", "not_in_terminal", "  oops \n").block!!
        assertTrue(terminal.notInTerminal)
        assertEquals("oops", terminal.detail)
        assertEquals(R.string.agents_launcher_help_title_terminal, terminal.title)
        assertNull(launcher("a", "blocked", "auth", "  ").block!!.detail)
        assertEquals(R.string.agents_launcher_help_title_computer, launcher("a", "blocked", "auth").block!!.title)
    }

    /**
     * The providers New Agent lists off on this computer (#8109): under the type each runs, after its launchers; never one
     * the computer lists already; under whichever type is shown when the computer has no launcher of its type; found by
     * the launcher search.
     */
    @Test fun providersLeftOutHereGoUnderTheTypeTheyRun() {
        fun left(name: String, agent: String? = null, title: String? = null) =
            UnavailableLauncher(name, title, "launcher", agent, code = "launcher_own_service", reason = "why $name")
        val launchers = listOf(AgentLauncher("claude"), AgentLauncher("codex", agentType = "codex"))
        val deepseek = left("deepseek", "claude", "DeepSeek")
        val plain = left("glm") // no agent named: Claude Code
        val codexGw = left("cx-gw", "codex")
        val listed = left("claude", "claude")
        val all = listOf(deepseek, plain, codexGw, listed)
        assertEquals(listOf(deepseek, plain), leftOutLaunchers(all, launchers, "claude"))
        assertEquals(listOf(codexGw), leftOutLaunchers(all, launchers, "codex"))
        // a computer with no Codex launcher: its Codex provider is said under the type shown, not left missing
        assertEquals(listOf(deepseek, plain, codexGw), leftOutLaunchers(all, listOf(AgentLauncher("claude")), "claude"))
        // nothing listed: nothing to leave out as listed already
        assertEquals(all, leftOutLaunchers(all, emptyList(), "claude"))
        // the search: by name or by the name it shows
        assertEquals(listOf(deepseek), leftOutLaunchers(all, launchers, "claude", "seek"))
        assertEquals(listOf(deepseek), leftOutLaunchers(all, launchers, "claude", "DEEPSEEK"))
        assertTrue(leftOutLaunchers(all, launchers, "claude", "nothing").isEmpty())
        assertTrue(leftOutLaunchers(emptyList(), launchers, "claude").isEmpty())
    }

    @Test fun theFormStartsOnOneThatIsNotBlocked() {
        val blocked = launcher("work_gw", "blocked", "not_in_terminal")
        val list = listOf(launcher("claude", "failing"), blocked, launcher("sonnet"))
        assertEquals("sonnet", initialLauncher("sonnet", list))
        assertEquals("claude", initialLauncher("work_gw", list))
        assertEquals("claude", initialLauncher("gone", list))
        assertEquals("claude", initialLauncher(null, list))
        val allBlocked = listOf(blocked, launcher("team_gw", "blocked", "auth"))
        assertEquals("team_gw", initialLauncher("team_gw", allBlocked))
        assertEquals("work_gw", initialLauncher(null, allBlocked))
        assertEquals("", initialLauncher("x", emptyList()))
        // New Agent's own start, with nothing to follow
        assertEquals("claude", defaultLauncher(NewAgentModelConfig.Unsupported, AgentDefaults(launcher = "work_gw"), list))
    }
}
