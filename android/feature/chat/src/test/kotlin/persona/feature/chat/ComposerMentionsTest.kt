package persona.feature.chat

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.SessionFileEntry

/**
 * A chat's @ list browsing the session's folder (#4339 C16, iOS `MentionRows` and its `MentionMenuTests`): the folder's
 * own entries after the files the chat used, a word with a slash browsing a folder relative to the session's.
 */
class ComposerMentionsTest {
    private val cwd = "/Users/me/code/app"
    private val self = SessionKey("m", "self")
    private val folders = MentionFolders { _, _ -> emptyList() }

    private fun entry(name: String, dir: Boolean = false) = SessionFileEntry(name, dir)
    private fun session(id: String, title: String, cwd: String = "/Users/me/code/app", agent: String = "claude", assistant: Boolean = false, at: Int) =
        Session(machineId = "m", sessionId = id, title = title, cwd = cwd, agent = agent, assistant = assistant, updatedAt = "2026-11-12T10:00:0$at.000Z")

    private fun mentions(used: List<String> = emptyList(), sessions: List<Session> = emptyList(), cwd: String = this.cwd,
                         folders: MentionFolders? = this.folders) =
        ComposerMentions(used, cwd, sessions, self, folders = folders)

    @Test fun theFolderTheListReadsIsTheBrowsedOneElseTheSessions() {
        assertEquals(cwd, mentionFolder("", cwd))
        assertEquals("a word without a slash is the session's own folder", cwd, mentionFolder("src", cwd))
        assertEquals("/Users/me/code/app/src", mentionFolder("src/", "$cwd/"))
        assertEquals("/", mentionFolder("/", cwd))
        assertEquals("/etc", mentionFolder("/etc/ho", cwd))
        assertEquals("~", mentionFolder("~/", ""))
        assertEquals("~/code", mentionFolder("~/code/", ""))
        // New chat's page has no folder of its own: a relative path or a plain word reads nothing.
        assertNull(mentionFolder("", ""))
        assertNull(mentionFolder("src/", ""))
        val relative = mentionBrowse("src/Vi", "$cwd/")
        assertEquals(MentionBrowse("/Users/me/code/app/src", "src/", "Vi"), relative)
    }

    @Test fun filesThenTheSessionFoldersEntriesThenSessions() {
        val used = listOf("$cwd/Sources/Store.swift", "$cwd/README.md", "/tmp/out.log")
        val listing = listOf(entry("README.md"), entry(".gitignore"), entry("Sources", dir = true), entry("Package.swift"), entry("docs", dir = true))
        val sessions = listOf(session("s1", "Blog", cwd = "/Users/me/code/blog", at = 5), session("self", "This chat", at = 9),
            session("pa", "Persona", assistant = true, at = 8), session("s2", "Orders", cwd = "/home/me/shop", agent = "codex", at = 7))
        val rows = mentionRows(mentions(used, sessions), "", listing)
        // The chat's files newest first (relative inside the folder), then its folders, then its files, then the sessions.
        assertEquals(listOf("file:$cwd/Sources/Store.swift", "file:$cwd/README.md", "file:/tmp/out.log", "top:Sources", "top:docs", "top:Package.swift",
            "session:m/s2", "session:m/s1"), rows.map { it.key })
        assertEquals(listOf("@Sources/Store.swift ", "@README.md ", "@/tmp/out.log "), rows.filterIsInstance<MentionRow.File>().map { it.insert })
        // A folder keeps the list browsing it, a file ends in a space; hidden entries are left out, a file the chat used is not repeated.
        assertEquals(listOf("@Sources/", "@docs/", "@Package.swift "), rows.filterIsInstance<MentionRow.Entry>().map { it.insert })
        assertEquals(listOf(true, true, false), rows.filterIsInstance<MentionRow.Entry>().map { it.folder })
        assertEquals(true, rows.filterIsInstance<MentionRow.Entry>().all { it.top })
    }

    @Test fun typingFilters() {
        val listing = listOf(entry("README.md"), entry("Sources", dir = true), entry("docs", dir = true), entry("resources", dir = true))
        val sessions = listOf(session("s1", "Blog", cwd = "/Users/me/code/blog", at = 5), session("s2", "Orders", cwd = "/home/me/shop", agent = "codex", at = 7))
        fun names(query: String, used: List<String> = emptyList(), list: List<SessionFileEntry>? = listing, sessions: List<Session> = emptyList()) =
            mentionRows(mentions(used, sessions), query, list).map { it.key }
        // Names that start with the word first, then the rest, the folders before the files.
        assertEquals(listOf("top:Sources", "top:resources"), names("so"))
        // A session matches by its title, or by its agent and folder.
        assertEquals(listOf("session:m/s1"), names("BLOG", list = emptyList(), sessions = sessions))
        assertEquals(listOf("session:m/s2"), names("codex", list = emptyList(), sessions = sessions))
        // Hidden entries only when asked for.
        val hidden = listOf(entry(".env"), entry("env.sample"))
        assertEquals(listOf("top:env.sample"), names("env", list = hidden))
        assertEquals(listOf("top:.env"), names(".e", list = hidden))
        // A file the chat used is not listed again among the folder's.
        assertEquals(listOf("file:$cwd/README.md"), names("read", used = listOf("$cwd/README.md")))
    }

    @Test fun browsingARelativePathListsThatFolderAlone() {
        val listing = listOf(entry("main.swift"), entry("App", dir = true), entry("Models", dir = true), entry("app.json"))
        val sessions = listOf(session("s1", "App", at = 5))
        val source = mentions(used = listOf("$cwd/x.swift"), sessions = sessions)
        val rows = mentionRows(source, "Sources/", listing).filterIsInstance<MentionRow.Entry>()
        // Folders first, then files; what was typed stays in front of a picked name; no files used and no sessions while browsing.
        assertEquals(listOf("App/", "Models/", "main.swift", "app.json"), rows.map { if (it.folder) "${it.name}/" else it.name })
        assertEquals(4, mentionRows(source, "Sources/", listing).size)
        assertEquals(listOf("Sources/", "Sources/", "Sources/", "Sources/"), rows.map { it.typed })
        assertEquals("@Sources/App/", rows[0].insert)
        assertEquals("@Sources/main.swift ", rows[2].insert)
        assertEquals(listOf("fs:Sources/App", "fs:Sources/Models", "fs:Sources/main.swift", "fs:Sources/app.json"), rows.map { it.key })
        assertEquals(listOf("App/", "app.json"), mentionRows(source, "Sources/ap", listing).filterIsInstance<MentionRow.Entry>()
            .map { if (it.folder) "${it.name}/" else it.name })
        // Not read yet: no rows, the list says it is reading.
        assertEquals(emptyList<MentionRow>(), mentionRows(source, "Sources/", null))
    }

    @Test fun theSessionFoldersEntriesAreAtMostTwelveAndNeedAReaderAndAFolder() {
        val many = (1..20).map { entry("dir$it", dir = true) } + (1..20).map { entry("f$it.txt") }
        assertEquals(12, mentionRows(mentions(), "", many).filterIsInstance<MentionRow.Entry>().size)
        assertEquals("folders first", true, mentionRows(mentions(), "", many).filterIsInstance<MentionRow.Entry>().all { it.folder })
        // Without a reader (an older composer) or a folder of its own (New chat's page) there are none.
        assertEquals(emptyList<MentionRow>(), mentionRows(mentions(folders = null), "", many))
        assertEquals(emptyList<MentionRow>(), mentionRows(mentions(cwd = ""), "", many))
        // The browsed folder takes forty.
        val big = (1..60).map { entry("f$it") }
        assertEquals(40, mentionRows(mentions(), "f/", big).size)
    }

    @Test fun aWordWithASlashWithoutAReaderOnlyNarrowsTheFilesAsBefore() {
        val source = mentions(used = listOf("$cwd/src/main.kt", "$cwd/README.md"), folders = null)
        assertEquals(listOf("file:$cwd/src/main.kt"), mentionRows(source, "src/", listOf(entry("zzz"))).map { it.key })
    }
}
