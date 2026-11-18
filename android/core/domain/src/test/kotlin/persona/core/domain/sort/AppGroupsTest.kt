package persona.core.domain.sort

import persona.core.contracts.Session
import persona.core.domain.zhAny
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

/** By App (#9199): port of iOS `AppGroupsTests`. */
class AppGroupsTest {
    private fun session(
        id: String, agent: String, surface: String?, state: String = "idle", updated: Long,
        machine: String = "m", cwd: String = "/w", extra: Session.() -> Session = { this },
    ): AgentSession {
        // parseTime drops times before 2000; use a 2026 base like the other list tests.
        val at = Instant.parse("2026-01-01T00:00:00Z").plusSeconds(updated).toString()
        return AgentSession(
            Session(
                machineId = machine, sessionId = id, title = id, cwd = cwd, status = state, state = state,
                agent = agent, agentSurface = surface, updatedAt = at,
            ).extra(),
        )
    }

    private fun placed(
        id: String, agent: String = "claude", cwd: String, updated: Long, machine: String = "m",
        extra: Session.() -> Session = { this },
    ) = session(id, agent, "desktop", updated = updated, machine = machine, cwd = cwd, extra = extra)

    @Test fun desktopAppsFirstThenOtherWithOfflineAgentsNewestFirst() {
        val sessions = listOf(
            session("term-claude", "claude", "cli", updated = 50),
            session("codex-old", "codex", "desktop", state = "offline", updated = 10),
            session("claude-offline", "claude", "desktop", state = "offline", updated = 40),
            session("codex-new", "codex", "desktop", state = "working", updated = 30),
            session("claude-new", "claude", "desktop", state = "working", updated = 20),
            session("unknown-client", "codex", null, updated = 60),
            session("trae-app", "trae", "desktop", updated = 5),
            session("amp-app", "amp", "desktop", updated = 6),
        )
        val groups = AppGroups.build(sessions)
        assertEquals(
            listOf("app/desktop/claude code", "app/desktop/codex", "app/desktop/amp", "app/desktop/trae", AppGroups.OTHER_ID),
            groups.map { it.id },
        )
        assertEquals(
            listOf(
                listOf("claude-offline", "claude-new"),
                listOf("codex-new", "codex-old"),
                listOf("amp-app"),
                listOf("trae-app"),
                listOf("unknown-client", "term-claude"),
            ),
            groups.map { g -> g.sessions.map { it.session.sessionId } },
        )
        assertEquals("Claude Code", groups[0].kindName)
        assertEquals("Claude desktop app", groups[0].title.english())
        assertEquals("Codex App", groups[1].title.english())
        assertEquals("TRAE desktop app", groups[3].title.english())
        assertEquals("Other", groups[4].title.english())
        assertEquals(null, groups[4].kindName)
    }

    @Test fun anAppsAgentsGoByProject() {
        val sessions = listOf(
            placed("gateway-old", cwd = "/Users/me/code/gateway", updated = 10),
            placed("app-new", cwd = "/Users/me/code/app", updated = 50),
            placed("gateway-new", cwd = "/Users/me/code/gateway/", updated = 40),
            placed("app-worktree", cwd = "/Users/me/code/app/.claude/worktrees/fix", updated = 20) {
                copy(repoRoot = "/Users/me/code/app")
            },
            placed("at-home", cwd = "/Users/me", updated = 60),
            placed("no-dir", cwd = "", updated = 5),
            placed("gateway-elsewhere", cwd = "/Users/me/code/gateway", updated = 30, machine = "other-mac"),
        )
        val app = AppGroups.build(sessions)[0]
        assertTrue(app.showsProjects)
        assertEquals(listOf("app", "gateway", "gateway", "Other"), app.projects.map { it.title.english() }.zhAny())
        assertEquals(
            listOf(
                listOf("app-new", "app-worktree"),
                listOf("gateway-new", "gateway-old"),
                listOf("gateway-elsewhere"),
                listOf("at-home", "no-dir"),
            ),
            app.projects.map { p -> p.sessions.map { it.session.sessionId } },
        )
        assertTrue(app.projects.last().isOther)
        assertEquals(app.id + "/project/other#", app.projects.last().id)
        assertEquals("m", app.projects.first().machineId)
        assertEquals(null, app.projects.last().machineId)
        assertEquals(app.projects.size, app.projects.map { it.id }.toSet().size)
        assertEquals(sessions.size, app.sessions.size)
    }

    @Test fun codexAppChatsGoByTheAppsProjects() {
        fun chat(id: String, root: String?, name: String, updated: Long) = placed(id, agent = "codex", cwd = root ?: "", updated = updated) {
            copy(
                spaceKind = "codex_app",
                spaceId = root?.let { "codex-project:$it" } ?: "codex-chats:/Users/me/.codex",
                spaceName = name,
                repoRoot = root ?: "",
            )
        }
        val app = AppGroups.build(
            listOf(
                chat("csv", "/Users/me/code/billing", "billing", 30),
                chat("pages", "/Users/me/code/billing", "billing", 10),
                chat("scratch", "/Users/me/Documents/Codex/2026-11-06/w-i", "w-i", 50),
                chat("no-folder", null, "Codex chats", 40),
                chat("at-home", "/Users/me", "me", 20),
            ),
        )[0]
        assertEquals(listOf("billing", "Other"), app.projects.map { it.title.english() }.zhAny())
        assertEquals(
            listOf(listOf("csv", "pages"), listOf("scratch", "no-folder", "at-home")),
            app.projects.map { p -> p.sessions.map { it.session.sessionId } },
        )
        assertTrue(AppGroups.isCodexChatFolder("/Users/me/Documents/Codex/2026-10-27/pei-z"))
        assertTrue(AppGroups.isCodexChatFolder("C:\\Users\\me\\Documents\\Codex\\2026-10-27\\pei-z"))
        assertFalse(AppGroups.isCodexChatFolder("/Users/me/Documents/Codex/notes/pei-z"))
        assertFalse(AppGroups.isCodexChatFolder("/Users/me/Documents/ChatGPT/botmux"))
    }

    @Test fun codexAppProjectsGoInTheAppsOrder() {
        fun chat(id: String, root: String?, place: Int?, updated: Long) = placed(id, agent = "codex", cwd = root ?: "", updated = updated) {
            copy(
                spaceKind = "codex_app",
                spaceId = root?.let { "codex-project:$it" } ?: "codex-chats:/Users/me/.codex",
                spaceName = root?.substringAfterLast('/') ?: "Codex chats",
                repoRoot = root ?: "",
                spaceIndex = place,
            )
        }
        val codex = AppGroups.build(
            listOf(
                chat("site-chat", "/Users/me/code/site", 2, 50),
                chat("tools-chat", "/Users/me/code/tools", 1, 10),
                chat("tools-sub", "/Users/me/code/tools", 1, 5),
                chat("unlisted-new", "/Users/me/.codex/.chatgpt-projects/g-p-1", null, 60),
                chat("unlisted-old", "/Users/me/code/old", null, 20),
                chat("loose", null, null, 70),
            ),
        )[0]
        assertEquals(listOf("tools", "site", "g-p-1", "old", "Other"), codex.projects.map { it.title.english() }.zhAny())
        assertEquals(listOf("tools-chat", "tools-sub"), codex.projects.first().sessions.map { it.session.sessionId })
        val claude = AppGroups.build(
            listOf(
                placed("numbered-old", cwd = "/Users/me/code/a", updated = 10) {
                    copy(spaceKind = "herdr", spaceId = "w1", spaceIndex = 1)
                },
                placed("latest", cwd = "/Users/me/code/b", updated = 30),
            ),
        )[0]
        assertEquals(listOf("b", "a"), claude.projects.map { it.name })
    }

    @Test fun projectsFromRepositoriesAndHubs() {
        val tree = placed("tree", cwd = "/Users/me/code/app/.claude/worktrees/fix", updated = 3).session
        assertEquals("app", AppGroups.projectOf(tree)?.second)
        val underHomeRepo = placed("notes", cwd = "/Users/me/notes", updated = 2) {
            copy(repoRoot = "/Users/me", worktree = "/Users/me")
        }.session
        assertEquals("notes", AppGroups.projectOf(underHomeRepo)?.second)
        val a = AppGroups.projectOf(placed("hub1", cwd = "/Users/me/code/app", updated = 1).session)
        val b = AppGroups.projectOf(placed("hub2", cwd = "/Users/me/code/app", updated = 1, machine = "other").session)
        assertTrue(a?.first != b?.first)
    }

    @Test fun terminalEnginesHaveSectionsByProject() {
        fun cli(kind: String?, pane: String? = null, repo: String? = null): Session.() -> Session = {
            copy(agentSurface = "cli", spaceKind = kind, paneId = pane, repoRoot = repo)
        }
        val sessions = listOf(
            placed("herdr-app", cwd = "/Users/me/code/app", updated = 50, extra = cli("herdr", "w1:p1")),
            placed("herdr-gateway", agent = "codex", cwd = "/Users/me/code/gateway", updated = 40, extra = cli("herdr", "w1:p2")),
            placed("herdr-app-sub", cwd = "/Users/me/code/app/web", updated = 30, extra = cli("herdr", "w2:p1", "/Users/me/code/app")),
            placed("herdr-home", cwd = "/Users/me", updated = 5, extra = cli("herdr", "w3:p1")),
            placed("tern-x", cwd = "/srv/x", updated = 20, extra = cli("tern", "tern:b1")),
            placed("orca-y", cwd = "/srv/y", updated = 25, extra = cli("orca", "orca:p1")),
            placed("desk", cwd = "/Users/me/code/app", updated = 10),
            placed("tmux", cwd = "/Users/me/code/app", updated = 60, extra = cli(null, "tmux:%1")),
            placed("runtime", agent = "paseo", cwd = "/srv/z", updated = 15, extra = cli("paseo")),
        )
        val groups = AppGroups.build(sessions)
        assertEquals(
            listOf("app/desktop/claude code", "app/engine/herdr", "app/engine/orca", "app/engine/tern", AppGroups.OTHER_ID),
            groups.map { it.id },
        )
        val herdr = groups[1]
        assertEquals(3, herdr.projects.size)
        assertEquals("herdr", herdr.engine)
        assertEquals(null, herdr.kindName)
        assertEquals("herdr", herdr.title.english())
        assertTrue(herdr.showsProjects)
        assertEquals(listOf("app", "gateway", ""), herdr.projects.map { it.name })
        assertEquals(listOf("herdr-app", "herdr-app-sub"), herdr.projects[0].sessions.map { it.session.sessionId })
        assertTrue(herdr.projects[0].id.startsWith("app/engine/herdr/project/"))
        assertEquals("Orca", groups[2].title.english())
        assertEquals("Tern", groups[3].title.english())
        assertEquals("orca", groups[2].engine)
        assertEquals(listOf("tmux", "runtime"), groups[4].sessions.map { it.session.sessionId })
        assertEquals(null, groups[4].engine)
        assertFalse(groups[4].showsProjects)
    }

    @Test fun otherIsNotByProject() {
        val other = AppGroups.build(listOf(session("t", "claude", "cli", updated = 1)))[0]
        assertEquals(null, other.kindName)
        assertTrue(other.projects.isEmpty())
        assertFalse(other.showsProjects)
    }

    @Test fun noProjectsNoHeadings() {
        val app = AppGroups.build(
            listOf(
                placed("a", cwd = "/Users/me", updated = 1),
                placed("b", cwd = "/home/me/", updated = 2),
            ),
        )[0]
        assertFalse(app.showsProjects)
        assertEquals(1, app.projects.size)
        for (home in listOf("/", "~", "/root", "/Users/me", "/home/me", "C:\\Users\\me", "C:\\")) {
            assertTrue(home, AppGroups.isHome(home))
        }
        for (project in listOf("/Users/me/code", "/srv/app", "/home/me/work", "C:\\Users\\me\\code")) {
            assertFalse(project, AppGroups.isHome(project))
        }
    }

    @Test fun claudeWhenTheAgentIsUnnamedAndCodexByItsSourceKind() {
        val unnamed = session("unnamed", "", "desktop", updated = 2)
        val byKind = session("by-kind", "", "desktop", updated = 1) { copy(sourceKind = "codex_history") }
        val groups = AppGroups.build(listOf(unnamed, byKind))
        assertEquals(listOf("app/desktop/claude code", "app/desktop/codex"), groups.map { it.id })
        assertEquals(listOf(listOf("unnamed"), listOf("by-kind")), groups.map { g -> g.sessions.map { it.session.sessionId } })
    }

    @Test fun agentsUpdatedAtTheSameTimeKeepOneOrder() {
        val a = session("a", "claude", "desktop", updated = 7)
        val b = session("b", "claude", "desktop", updated = 7)
        assertEquals(
            AppGroups.build(listOf(a, b)).first().sessions.map { it.session.sessionId },
            AppGroups.build(listOf(b, a)).first().sessions.map { it.session.sessionId },
        )
    }

    @Test fun anAppWithNoAgentHasNoSection() {
        assertTrue(AppGroups.build(emptyList()).isEmpty())
        assertEquals(listOf("app/desktop/claude code"), AppGroups.build(listOf(session("a", "claude", "desktop", updated = 1))).map { it.id })
        assertEquals(listOf(AppGroups.OTHER_ID), AppGroups.build(listOf(session("a", "codex", "cli", updated = 1))).map { it.id })
    }

    @Test fun byAppIsOfferedAfterTodoAndTheDefaultStays() {
        assertEquals(AgentGrouping.Repo, AgentListSettings().grouping)
        assertEquals(AgentGrouping.App, AgentGrouping.fromPreference("App"))
        val offered = AgentGrouping.selectable
        assertTrue(offered.indexOf(AgentGrouping.App) > offered.indexOf(AgentGrouping.Todo))
        // By Machine sits between By to-do and By App (#9319).
        assertEquals(offered.indexOf(AgentGrouping.Machine) + 1, offered.indexOf(AgentGrouping.App))
        assertEquals(offered.indexOf(AgentGrouping.App) + 1, offered.indexOf(AgentGrouping.Recent))
    }

    @Test fun listProjectionBuildsAppSectionsAndProjects() {
        val rows = listOf(
            placed("app-new", cwd = "/Users/me/code/app", updated = 50),
            placed("gateway", cwd = "/Users/me/code/gateway", updated = 40),
            session("term", "claude", "cli", updated = 30),
        )
        val list = buildAgentList(rows, settings = AgentListSettings(grouping = AgentGrouping.App))
        assertEquals(
            listOf("app/desktop/claude code", "app/desktop/claude code/project/m//Users/me/code/app#",
                "app/desktop/claude code/project/m//Users/me/code/gateway#", AppGroups.OTHER_ID),
            list.groups.filter { it.id != "pinned" }.map { it.id },
        )
        assertEquals("app", list.groups[1].title.english())
        assertEquals(listOf("app-new"), list.groups[1].rows.map { it.session.sessionId })
        assertEquals("app/desktop/claude code", list.groups[1].under)
    }
}
