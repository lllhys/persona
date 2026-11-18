package persona.feature.files

import persona.core.contracts.api.*
import kotlinx.coroutines.ExperimentalCoroutinesApi
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import kotlinx.coroutines.cancelChildren
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

/** A machine with a few folders under /Users/me; records what the pane asked for. */
private class FakeMachine : SessionFiles {
    val asked = mutableListOf<String>()
    val folders = setOf("/", "/Users", "/Users/me", "/Users/me/code", "/Users/me/code/weather", "/Users/me/code/weather/Sources",
        "/Users/me/code/weather/Sources/App")
    var failing: Exception? = null
    var gitFailing: Exception? = null
    var logFailing: Exception? = null
    /** Holds a folder read until completed: a request still on its way when the connection comes back. */
    var gate: kotlinx.coroutines.CompletableDeferred<Unit>? = null

    private fun expand(path: String) = if (path == "~" || path.startsWith("~/")) "/Users/me" + path.removePrefix("~") else path

    override suspend fun directory(machineId: String, cwd: String, path: String): SessionDirectory {
        asked += "fs $path"
        gate?.await()
        failing?.let { throw it }
        val real = expand(path)
        require(real in folders) { "not a folder: $real" }
        return SessionDirectory(real, folders.filter { it != real && FilesPaths.parent(it) == real }.map { SessionFileEntry(FilesPaths.name(it), dir = true) })
    }

    override suspend fun file(machineId: String, cwd: String, path: String): SessionFileContent {
        asked += "file $path"
        failing?.let { throw it }
        val real = expand(path)
        return if (real in folders) SessionFileContent(real, FilesPaths.name(real), isDir = true, entries = listOf("App/"))
        else SessionFileContent(real, FilesPaths.name(real), size = 5, text = "hello")
    }

    var gitStatus = SessionGitStatus(true, root = "/Users/me/code/weather")
    var pulling: suspend () -> SessionGitPull = { error("no pull") }
    /** Record status reads too (the pull tests count them). */
    var askedStatus = false
    override suspend fun status(machineId: String, cwd: String): SessionGitStatus {
        if (askedStatus) asked += "status"
        gitFailing?.let { throw it }
        return gitStatus
    }
    override suspend fun pull(machineId: String, cwd: String): SessionGitPull {
        asked += "pull $cwd"
        return pulling()
    }
    override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) = SessionGitDiff(path, "")
    override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int): SessionGitLog {
        asked += "log $skip"
        logFailing?.let { throw it }
        return SessionGitLog()
    }
    override fun comparesVersions(machineId: String) = true
    override suspend fun blob(machineId: String, cwd: String, path: String, rev: String): SessionGitBlob {
        asked += "blob $rev $path"
        return if (rev == "HEAD") SessionGitBlob(missing = true) else SessionGitBlob("/cache/blobs/${rev.lowercase()}.png", 10)
    }
    override suspend fun show(machineId: String, cwd: String, hash: String): SessionGitShow {
        asked += "show $hash"
        return SessionGitShow(SessionGitCommit(hash, subject = "Add README"), files = listOf(SessionGitFile("README.md", index = "M")),
            diff = "diff --git a/README.md b/README.md\n@@ -1 +1 @@\n-a\n+b")
    }
    var edits = true
    var editFailing: Exception? = null
    var restoreFailing: Exception? = null
    /** Holds a change until completed: the page may close while it is on its way. */
    var editGate: kotlinx.coroutines.CompletableDeferred<Unit>? = null
    override fun editsFiles(machineId: String) = edits
    override suspend fun rename(machineId: String, cwd: String, path: String, name: String): String {
        asked += "rename $path $name"
        editGate?.await()
        editFailing?.let { throw it }
        return joinRemotePath(FilesPaths.parent(path) ?: "/", name)
    }
    override suspend fun trash(machineId: String, cwd: String, paths: List<String>): List<SessionTrashedFile> {
        asked += "trash ${paths.joinToString()}"
        editGate?.await()
        editFailing?.let { throw it }
        return paths.mapIndexed { i, path -> SessionTrashedFile(path, "t$i") }
    }
    override suspend fun restore(machineId: String, trashIds: List<String>): List<String> {
        asked += "restore ${trashIds.joinToString()}"
        restoreFailing?.let { throw it }
        return trashIds
    }
}

@OptIn(ExperimentalCoroutinesApi::class)
class FilesControllerTest {
    private val cwd = "/Users/me/code/weather"

    private fun TestScope.controller(machine: FakeMachine) =
        FilesController(machine, "macbook", cwd, this, describe = { it.message ?: "?" })

    @Test fun theBrowserStartsInTheSessionFolderAndGoesUpToTheRoot() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        assertEquals(cwd, files.state.value.here)
        assertEquals(listOf("weather" to cwd), files.state.value.crumbs)
        assertEquals(listOf("Sources"), files.state.value.directory!!.entries.map { it.name })

        // Above the session folder: the breadcrumb's top moves up with it, one folder at a time, to the root.
        files.up()
        runCurrent()
        assertEquals("/Users/me/code", files.state.value.root)
        assertEquals(listOf("weather"), files.state.value.directory!!.entries.map { it.name })
        repeat(3) { files.up(); runCurrent() }
        assertEquals("/", files.state.value.here)
        assertEquals(listOf("/" to "/"), files.state.value.crumbs)
        assertNull(files.state.value.parent)
        files.up() // nothing above the root
        runCurrent()
        assertEquals("/", files.state.value.here)

        // Back into the tree: the crumbs grow from the new top, back walks up to it and then leaves.
        files.directory("/Users")
        runCurrent()
        files.directory("/Users/me")
        runCurrent()
        assertEquals(listOf("/" to "/", "Users" to "/Users", "me" to "/Users/me"), files.state.value.crumbs)
        assertTrue(files.back())
        runCurrent()
        assertEquals("/Users", files.state.value.here)
        assertTrue(files.back())
        runCurrent()
        assertFalse(files.back())
        assertFalse(files.back(FilesPane.Git))
    }

    @Test fun goingUpInsideTheTreeKeepsTheTop() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        files.directory("$cwd/Sources/App")
        runCurrent()
        assertTrue(files.state.value.belowRoot)
        files.up()
        runCurrent()
        assertEquals("$cwd/Sources", files.state.value.here)
        assertEquals(cwd, files.state.value.root)
        // Git's pane does not walk folders: back leaves from there.
        assertFalse(files.back(FilesPane.Git))
    }

    @Test fun aFileFromTheConversationOpensAloneAndBackLeaves() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("Sources/App/main.swift")
        runCurrent()
        assertEquals(listOf("file $cwd/Sources/App/main.swift"), machine.asked.filter { it.startsWith("file") })
        assertTrue(machine.asked.none { it.startsWith("fs") }) // no browser under it
        assertEquals("hello", files.state.value.content?.text)
        assertTrue(files.state.value.direct)
        assertFalse(files.back())
    }

    @Test fun aFileFromTheConversationKeepsItsLine() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("Sources/App/main.swift", initialLine = 12)
        runCurrent()
        assertEquals(FileSelection.File("$cwd/Sources/App/main.swift", 12), files.state.value.selection)
        assertEquals("hello", files.state.value.content?.text)

        // No line, or one that is not a line: the file opens at its top.
        val other = controller(FakeMachine())
        other.start("Sources/App/main.swift", initialLine = 0)
        runCurrent()
        assertNull((other.state.value.selection as FileSelection.File).line)
    }

    @Test fun homePathsTravelAsWritten() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("~/notes.txt")
        runCurrent()
        assertTrue("file ~/notes.txt" in machine.asked)
    }

    @Test fun aFolderFromTheConversationOpensInTheBrowserAsItsTop() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("Sources/")
        runCurrent()
        assertEquals("$cwd/Sources", files.state.value.root)
        assertEquals("$cwd/Sources", files.state.value.here)
        assertNull(files.state.value.selection)
        assertFalse(files.back())

        // Without a slash the hub says it is a folder: the same, after one read.
        val other = FakeMachine()
        val again = controller(other)
        again.start("Sources")
        runCurrent()
        assertEquals(listOf("file $cwd/Sources", "fs $cwd/Sources"), other.asked.filter { !it.startsWith("show") })
        assertEquals("$cwd/Sources", again.state.value.root)
        assertNull(again.state.value.selection)
        assertFalse(again.state.value.direct)
        assertEquals(listOf("App"), again.state.value.directory!!.entries.map { it.name })
        again.directory("$cwd/Sources/App")
        runCurrent()
        assertTrue(again.back())
        runCurrent()
        assertFalse(again.back())
    }

    @Test fun theHomeOpensResolved() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start("~")
        runCurrent()
        // The hub expands "~": the breadcrumb shows where that is.
        assertEquals("/Users/me", files.state.value.root)
        assertEquals("/Users/me", files.state.value.here)
    }

    @Test fun aCommitsFilesOpenOverItWithoutAskingAgain() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        files.push(FileSelection.Commit("abc"))
        runCurrent()
        assertEquals("Add README", files.state.value.commit?.commit?.subject)
        files.push(FileSelection.CommitFile("abc", SessionGitFile("README.md", index = "M")))
        runCurrent()
        assertEquals(2, files.state.value.stack.size)
        assertNotNull(files.state.value.commit)
        assertEquals(1, machine.asked.count { it == "show abc" })
        assertTrue(files.back())
        runCurrent()
        assertTrue(files.state.value.selection is FileSelection.Commit)
        assertNotNull(files.state.value.commit)
        assertEquals(1, machine.asked.count { it == "show abc" })
        assertTrue(files.back())
        assertNull(files.state.value.selection)
        assertFalse(files.back())
    }

    @Test fun backToAFileUnderAnotherShowsItAtOnceAndReloadAsksAgain() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        files.push(FileSelection.File("$cwd/README.md"))
        runCurrent()
        files.push(FileSelection.File("$cwd/docs/guide.md")) // a link in the README
        runCurrent()
        assertTrue(files.back())
        // No request, no loading frame: the README as it was.
        assertEquals("$cwd/README.md", files.state.value.content?.path)
        assertEquals(1, machine.asked.count { it == "file $cwd/README.md" })
        files.reload()
        runCurrent()
        assertEquals(2, machine.asked.count { it == "file $cwd/README.md" })
    }

    @Test fun aFolderOpenedFromAPageOpensInTheBrowser() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        files.push(FileSelection.File("$cwd/README.md"))
        runCurrent()
        // A README's link to a folder below the session folder: the browser goes there and back walks up.
        files.push(FileSelection.File("$cwd/Sources"))
        runCurrent()
        assertNull(files.state.value.selection)
        assertEquals("$cwd/Sources", files.state.value.here)
        assertEquals(cwd, files.state.value.root)
        assertTrue(files.back())
    }

    @Test fun failuresSayWhyAndKeepWhatWasShown() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        machine.failing = IllegalStateException("macOS 不让 Persona 读「桌面」文件夹里的文件")
        files.directory()
        runCurrent()
        assertEquals("macOS 不让 Persona 读「桌面」文件夹里的文件", files.state.value.directoryError)
        assertNotNull(files.state.value.directory) // the listing from before stays under the error
        files.preview(FileSelection.File("$cwd/a.txt"))
        runCurrent()
        assertEquals("macOS 不让 Persona 读「桌面」文件夹里的文件", files.state.value.previewError)
        machine.failing = null
        files.reload()
        runCurrent()
        assertNull(files.state.value.previewError)
        assertEquals("hello", files.state.value.content?.text)
    }

    @Test fun aComparisonReadsEachVersionFromGitOrTheTree() = runTest {
        val machine = FakeMachine()
        val files = controller(machine)
        files.start()
        runCurrent()
        files.push(FileSelection.Compare("art/icon.png", "art/icon.png", "INDEX", "WORKTREE"))
        runCurrent()
        assertTrue("blob INDEX art/icon.png" in machine.asked)
        assertEquals(CompareSides(CompareSide("INDEX", "/cache/blobs/index.png"), CompareSide("WORKTREE", "/Users/me/code/weather/art/icon.png")),
            files.state.value.compare)
        // Added since HEAD: no old version. Deleted in the tree: no new one, and nothing to ask for it.
        files.preview(FileSelection.Compare("art/icon.png", "art/icon.png", "HEAD", "WORKTREE", deleted = true))
        runCurrent()
        assertEquals(CompareSides(CompareSide("HEAD", null), CompareSide("WORKTREE", null)), files.state.value.compare)
        files.back()
        assertNull(files.state.value.compare)
    }

    private val tracking = SessionGitStatus(true, root = "/Users/me/code/weather", branch = "main", upstream = "origin/main", behind = 2,
        commits = listOf(SessionGitCommit("aaa")))

    private fun TestScope.pulling(machine: FakeMachine) =
        FilesController(machine, "macbook", cwd, this, describe = { it.message ?: "?" }, pulled = { if (it) "已拉取最新代码" else "已经是最新代码" })

    @Test fun aPullShowsTheStatusItReturns() = runTest {
        val machine = FakeMachine().apply { gitStatus = tracking; askedStatus = true }
        val gate = kotlinx.coroutines.CompletableDeferred<Unit>()
        val fresh = tracking.copy(behind = 0, commits = listOf(SessionGitCommit("fff"), SessionGitCommit("aaa")))
        machine.pulling = { gate.await(); SessionGitPull(fresh, updated = true) }
        val files = pulling(machine)
        files.refreshGit()
        runCurrent()
        files.pull()
        runCurrent()
        assertTrue(files.state.value.pulling)
        files.pull() // already running
        files.refreshGit() // waits for the pull
        runCurrent()
        assertEquals(listOf("status", "pull $cwd"), machine.asked)
        gate.complete(Unit)
        runCurrent()
        val state = files.state.value
        assertFalse(state.pulling)
        assertFalse(state.pullFailed)
        assertEquals("已拉取最新代码", state.pullMessage)
        assertEquals(fresh, state.git)
        assertTrue(state.older.isEmpty())
        assertEquals(listOf("status", "pull $cwd"), machine.asked) // the answer is the status: no second read
    }

    @Test fun aPullThatBringsNothingSaysSo() = runTest {
        val machine = FakeMachine().apply { gitStatus = tracking.copy(behind = 0); askedStatus = true }
        machine.pulling = { SessionGitPull(machine.gitStatus, updated = false) }
        val files = pulling(machine)
        files.refreshGit()
        runCurrent()
        files.pull()
        runCurrent()
        assertEquals("已经是最新代码", files.state.value.pullMessage)
        assertFalse(files.state.value.pullFailed)
    }

    @Test fun aRefusedPullShowsTheHubsReasonAndRereadsTheStatus() = runTest {
        val machine = FakeMachine().apply { gitStatus = tracking; askedStatus = true }
        machine.pulling = { throw IllegalStateException("工作区有未提交的改动") }
        val files = pulling(machine)
        files.refreshGit()
        runCurrent()
        files.pull()
        runCurrent()
        val state = files.state.value
        assertTrue(state.pullFailed)
        assertEquals("工作区有未提交的改动", state.pullMessage)
        assertEquals(tracking, state.git)
        assertEquals(listOf("status", "pull $cwd", "status"), machine.asked)

        // No upstream (or a detached HEAD, or one deleted on the remote): nothing to pull from.
        for (nothing in listOf(tracking.copy(upstream = null), tracking.copy(behind = 0, gone = true))) {
            machine.gitStatus = nothing
            files.refreshGit()
            runCurrent()
            machine.asked.clear()
            files.pull()
            runCurrent()
            assertTrue(machine.asked.isEmpty())
        }
    }
    @Test fun comingBackOnlineReadsAgainWhatFailed() = runTest {
        val machine = FakeMachine()
        val online = MutableStateFlow(ConnectionState.Online)
        val files = FilesController(machine, "macbook", cwd, this, describe = { it.message ?: "?" }, connection = online)
        files.start()
        runCurrent()
        // The network drops: the next folder and Git's status fail.
        online.value = ConnectionState.Connecting
        machine.failing = IllegalStateException("unreachable")
        machine.gitFailing = IllegalStateException("unreachable")
        files.directory("$cwd/Sources")
        files.refreshGit()
        runCurrent()
        assertEquals("unreachable", files.state.value.directoryError)
        assertTrue(files.state.value.gitError)

        // Back online: both are read again without a tap on "Retry".
        machine.failing = null
        machine.gitFailing = null
        online.value = ConnectionState.Online
        runCurrent()
        assertNull(files.state.value.directoryError)
        assertEquals(listOf("App"), files.state.value.directory!!.entries.map { it.name })
        assertFalse(files.state.value.gitError)

        // A file that failed while away opens once the connection is back; nothing is asked when nothing failed.
        online.value = ConnectionState.WaitingForNetwork
        machine.failing = IllegalStateException("unreachable")
        files.preview(FileSelection.File("$cwd/README.md"))
        runCurrent()
        assertEquals("unreachable", files.state.value.previewError)
        machine.failing = null
        online.value = ConnectionState.Online
        runCurrent()
        assertNull(files.state.value.previewError)
        assertEquals("hello", files.state.value.content?.text)
        machine.asked.clear()
        machine.askedStatus = true
        online.value = ConnectionState.Connecting
        online.value = ConnectionState.Online
        runCurrent()
        assertTrue(machine.asked.isEmpty())
        coroutineContext.cancelChildren()
    }
    @Test fun aLoadedComparisonIsNotReadAgainOnReconnect() = runTest {
        val machine = FakeMachine()
        val online = MutableStateFlow(ConnectionState.Online)
        val files = FilesController(machine, "macbook", cwd, this, describe = { it.message ?: "?" }, connection = online)
        files.start()
        runCurrent()
        files.push(FileSelection.Compare("art/icon.png", "art/icon.png", "INDEX", "WORKTREE"))
        runCurrent()
        val loaded = files.state.value.compare
        assertNotNull(loaded)
        machine.asked.clear()
        online.value = ConnectionState.Connecting
        runCurrent()
        online.value = ConnectionState.Online
        runCurrent()
        // Nothing failed: the comparison on screen stays, and neither version is fetched again.
        assertEquals(loaded, files.state.value.compare)
        assertTrue(machine.asked.none { it.startsWith("blob") })
        coroutineContext.cancelChildren()
    }
    @Test fun aReadStillOnItsWayWhenTheConnectionReturnsIsAskedAgain() = runTest {
        val machine = FakeMachine()
        val online = MutableStateFlow(ConnectionState.Connecting)
        val files = FilesController(machine, "macbook", cwd, this, describe = { it.message ?: "?" }, connection = online)
        val stuck = kotlinx.coroutines.CompletableDeferred<Unit>()
        machine.gate = stuck
        files.start()
        runCurrent()
        assertTrue(files.state.value.directoryLoading)
        // Back online before the first read gave up: it is asked again, and its late failure does not show.
        machine.gate = null
        online.value = ConnectionState.Online
        runCurrent()
        machine.failing = IllegalStateException("unreachable")
        stuck.complete(Unit)
        runCurrent()
        assertNull(files.state.value.directoryError)
        assertEquals(listOf("Sources"), files.state.value.directory!!.entries.map { it.name })
        coroutineContext.cancelChildren()
    }

    @Test fun olderHistoryIsAskedAgainOnReconnectButAPullIsNot() = runTest {
        val machine = FakeMachine().apply {
            gitStatus = SessionGitStatus(true, root = cwd, branch = "main", upstream = "origin/main",
                commits = List(30) { SessionGitCommit("c$it") })
            pulling = { throw IllegalStateException("unreachable") }
        }
        val online = MutableStateFlow(ConnectionState.Online)
        val files = FilesController(machine, "macbook", cwd, this, describe = { it.message ?: "?" }, connection = online)
        files.start()
        runCurrent()
        online.value = ConnectionState.Connecting
        // The failed pull reads the status again (which starts the history over), then older history fails too.
        files.pull()
        runCurrent()
        assertTrue(files.state.value.pullFailed)
        machine.logFailing = IllegalStateException("unreachable")
        files.more()
        runCurrent()
        assertTrue(files.state.value.moreError)
        machine.logFailing = null
        machine.asked.clear()
        online.value = ConnectionState.Online
        runCurrent()
        assertFalse(files.state.value.moreError)
        assertTrue("log 30" in machine.asked)
        assertTrue(machine.asked.none { it.startsWith("pull") })
        coroutineContext.cancelChildren()
    }

    private fun TestScope.editing(machine: FakeMachine): Pair<FilesController, MutableList<FileEdit>> {
        val files = controller(machine)
        files.start()
        runCurrent()
        machine.asked.clear()
        return files to mutableListOf()
    }

    @Test fun aRenameSendsThePathAndTheNewNameThenReadsTheFolderAgain() = runTest {
        val machine = FakeMachine()
        val (files, reports) = editing(machine)
        assertTrue(files.editsFiles)
        files.rename("$cwd/Sources", "  Lib  ", reports::add)
        runCurrent()
        assertEquals(listOf("rename $cwd/Sources Lib", "fs $cwd"), machine.asked)
        assertEquals(listOf<FileEdit>(FileEdit.Renamed), reports)
        coroutineContext.cancelChildren()
    }

    @Test fun aNameTheMachineCantTakeIsSaidWithoutAsking() = runTest {
        val machine = FakeMachine()
        val (files, reports) = editing(machine)
        for (bad in listOf("", "  ", ".", "..", "a/b")) files.rename("$cwd/Sources", bad, reports::add)
        // The same name changes nothing, so nothing is sent and nothing is said.
        files.rename("$cwd/Sources", "Sources", reports::add)
        runCurrent()
        assertTrue(machine.asked.isEmpty())
        assertEquals(listOf(NameProblem.Empty, NameProblem.Empty, NameProblem.Dots, NameProblem.Dots, NameProblem.Slash),
            reports.map { (it as FileEdit.Invalid).problem })
        assertNull(nameProblem(".env"))
        coroutineContext.cancelChildren()
    }

    @Test fun aTrashCarriesUndoWhichPutsItBack() = runTest {
        val machine = FakeMachine()
        val (files, reports) = editing(machine)
        files.trash(listOf("$cwd/Sources"), reports::add)
        runCurrent()
        assertEquals(listOf("trash $cwd/Sources", "fs $cwd"), machine.asked)
        val trashed = reports.single() as FileEdit.Trashed
        assertEquals(listOf("t0"), trashed.trashIds)
        machine.asked.clear()
        files.restore(trashed.trashIds, "Sources", reports::add)
        runCurrent()
        assertEquals(listOf("restore t0", "fs $cwd"), machine.asked)
        assertEquals(FileEdit.Restored, reports.last())
        // An Undo that fails says so, as Undo's failure, and leaves the folder as it is.
        machine.asked.clear()
        machine.restoreFailing = HubFailure(HubError.Server, "taken", 409, "exists")
        files.restore(listOf("t0"), "Sources", reports::add)
        runCurrent()
        assertEquals(listOf("restore t0"), machine.asked)
        assertEquals(FileEdit.Failed(EditRefusal.Exists("Sources"), restoring = true), reports.last())
        coroutineContext.cancelChildren()
    }

    @Test fun refusalsAreSaidInTheUsersWordsAndTheFolderStays() = runTest {
        val machine = FakeMachine()
        val (files, reports) = editing(machine)
        val cases = listOf(
            HubFailure(HubError.Server, "only the home", 409, "read_only") to EditRefusal.ReadOnly,
            HubFailure(HubError.Server, "taken", 409, "exists") to EditRefusal.Exists("Lib"),
            HubFailure(HubError.Server, "not there", 404, "not_found") to EditRefusal.Gone,
            HubFailure(HubError.Server, "personad on box is too old", 409, "node_outdated") to EditRefusal.NodeOutdated("personad on box is too old"),
            HubFailure(HubError.Server, "HTTP 404", 404) to EditRefusal.HubTooOld,
            HubFailure(HubError.Server, "~/a is on another disk", 502, "internal") to EditRefusal.Unavailable(502),
        )
        for ((failure, refusal) in cases) {
            machine.editFailing = failure
            files.rename("$cwd/Sources", "Lib", reports::add)
            runCurrent()
            assertEquals(FileEdit.Failed(refusal), reports.last())
        }
        // A dropped connection is the connection's failure, said as a folder that can't be read says it.
        val offline = HubFailure(HubError.Timeout)
        machine.editFailing = offline
        files.trash(listOf("$cwd/Sources"), reports::add)
        runCurrent()
        assertEquals(FileEdit.Failed(EditRefusal.Other(offline)), reports.last())
        assertTrue(machine.asked.none { it.startsWith("fs ") })
        coroutineContext.cancelChildren()
    }

    @Test fun aChangeRunsToItsEndWhenThePageCloses() = runTest {
        val machine = FakeMachine()
        val (files, reports) = editing(machine)
        machine.editGate = kotlinx.coroutines.CompletableDeferred()
        files.trash(listOf("$cwd/Sources"), reports::add)
        runCurrent()
        // Leaving the page cancels its scope; the trash on its way still ends, and its Undo is still reported.
        coroutineContext.cancelChildren()
        machine.editGate!!.complete(Unit)
        runCurrent()
        assertEquals(listOf("t0"), (reports.single() as FileEdit.Trashed).trashIds)
    }

    @Test fun refusalsKeepTheHubsNameAndRequestId() = runTest {
        val machine = FakeMachine()
        val (files, reports) = editing(machine)
        // Undo into a folder where the name is taken meanwhile: the hub names the one there, not the name the phone asked about.
        machine.restoreFailing = HubFailure(HubError.Server, "taken", 409, "exists", body = """{"error":"taken","code":"exists","name":"Sources 2"}""")
        files.restore(listOf("t0"), "Sources", reports::add)
        runCurrent()
        assertEquals(FileEdit.Failed(EditRefusal.Exists("Sources 2"), restoring = true), reports.last())
        // A server error keeps the hub's request id, to find its log line.
        machine.editFailing = HubFailure(HubError.Server, "~/a is on another disk", 502, "internal", requestId = "r-1")
        files.rename("$cwd/Sources", "Lib", reports::add)
        runCurrent()
        assertEquals(FileEdit.Failed(EditRefusal.Unavailable(502, "r-1")), reports.last())
        coroutineContext.cancelChildren()
    }

    @Test fun editsAreOfferedOnlyWhereTheHubHasThem() = runTest {
        val machine = FakeMachine().apply { edits = false }
        assertFalse(controller(machine).editsFiles)
    }
}
