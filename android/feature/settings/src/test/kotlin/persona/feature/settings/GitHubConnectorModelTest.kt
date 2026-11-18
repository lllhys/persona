package persona.feature.settings

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.api.GitHubAuth
import persona.core.contracts.api.GitHubInboxOptions

class GitHubConnectorModelTest {
    @Test
    fun chooseAppliesTheSourceAndAFailureKeepsThePreviousOne() = runTest(UnconfinedTestDispatcher()) {
        val session = GitHubAuthSession(this) { it.message ?: "failed" }
        var applied = 0
        session.apply = { machine ->
            if (machine == "bad") throw IllegalStateException("nope")
            GitHubAuth(source = if (machine == null) "device" else "machine", machine = machine)
        }
        session.onApplied = { applied++ }

        session.choose(null)
        assertEquals("device", session.auth?.source)
        assertNull(session.error)
        assertNull(session.choosing)
        assertEquals(1, applied)

        session.choose("studio")
        assertEquals("studio", session.auth?.machine)
        assertEquals("machine", session.auth?.source)
        assertEquals(2, applied)

        session.choose("bad")
        assertEquals("studio", session.auth?.machine)
        assertEquals("nope", session.error)
        assertEquals(2, applied)
        assertNull(session.choosing)
    }

    @Test
    fun aSecondChooseWhileOneIsInFlightDoesNothing() = runTest(UnconfinedTestDispatcher()) {
        val gate = CompletableDeferred<Unit>()
        var calls = 0
        val session = GitHubAuthSession(this) { it.message ?: "failed" }
        session.apply = {
            calls++
            if (calls == 1) gate.await()
            GitHubAuth(source = "device")
        }
        session.choose(null)
        assertEquals("device", session.choosing)
        session.choose("studio")
        assertEquals(1, calls)
        gate.complete(Unit)
        assertNull(session.choosing)
        assertEquals(1, calls)
        assertEquals("device", session.auth?.source)
    }

    @Test
    fun inboxSaveTrimsDedupesRemovesAndKeepsAnError() = runTest(UnconfinedTestDispatcher()) {
        var fail = false
        var calls = 0
        val editor = GitHubInboxEditor(GitHubInboxOptions(), this, { it.message ?: "failed" }) {
            calls++
            if (fail) throw IllegalStateException("nope")
        }
        editor.setCi("mine")
        assertEquals(0, calls)

        editor.draft = "  acme/app "
        editor.add()
        assertEquals(listOf("acme/app"), editor.options.mutedRepos)
        assertEquals("", editor.draft)
        assertEquals(1, calls)

        editor.draft = "acme/app"
        editor.add()
        editor.draft = "noslash"
        editor.add()
        assertEquals("noslash", editor.draft)
        assertEquals(1, calls)

        editor.remove("acme/app")
        assertEquals(emptyList<String>(), editor.options.mutedRepos)
        assertEquals(2, calls)

        fail = true
        editor.setCi("off")
        assertEquals("off", editor.options.ci)
        assertEquals("nope", editor.error)
        assertEquals(3, calls)

        fail = false
        editor.setCi("all")
        assertEquals("all", editor.options.ci)
        assertNull(editor.error)
    }

    @Test
    fun inboxSaveCoalescesAChangeMadeWhileOneIsInFlight() = runTest(UnconfinedTestDispatcher()) {
        val gate = CompletableDeferred<Unit>()
        var calls = 0
        var active = 0
        var maxActive = 0
        val seen = mutableListOf<String>()
        val editor = GitHubInboxEditor(GitHubInboxOptions(), this, { it.message ?: "failed" }) { options ->
            calls++
            active++
            maxActive = maxOf(maxActive, active)
            seen += options.ci
            if (calls == 1) gate.await()
            active--
        }
        editor.setCi("off")
        editor.setCi("all")
        editor.setCi("default_branch")
        assertEquals(1, calls)
        assertEquals("default_branch", editor.options.ci)
        gate.complete(Unit)
        assertEquals(2, calls)
        assertEquals(1, maxActive)
        assertEquals(listOf("off", "default_branch"), seen)
    }

    @Test
    fun aSlowRefreshDoesNotRestoreTheSourceAfterChoose() = runTest(UnconfinedTestDispatcher()) {
        val gate = CompletableDeferred<GitHubAuth>()
        val session = GitHubAuthSession(this) { it.message ?: "failed" }
        session.fetch = { gate.await() }
        session.apply = { machine ->
            GitHubAuth(source = if (machine == null) "device" else "machine", machine = machine)
        }
        session.refresh()
        assertEquals(true, session.loading)
        session.choose("studio")
        assertEquals("machine", session.auth?.source)
        assertEquals("studio", session.auth?.machine)
        assertEquals(false, session.loading)
        gate.complete(GitHubAuth(source = "device"))
        assertEquals("machine", session.auth?.source)
        assertEquals("studio", session.auth?.machine)
        assertNull(session.error)
    }

    @Test
    fun rowKeysStayDistinctWhenAMachineIsNamedDeviceAndReposRepeat() {
        assertEquals("github-source-m-device", githubMachineRowKey("device"))
        assertFalse(githubMachineRowKey("device") == GITHUB_DEVICE_ROW_KEY)
        assertEquals(
            listOf("acme/app", "acme/web"),
            githubMutedRepos(listOf("acme/app", "acme/app", "acme/web")),
        )
        assertEquals("github-device-steps-null-null", githubDeviceStepsKey(null, null))
        assertEquals("github-device-steps-code-device_code", githubDeviceStepsKey("code", "device_code"))
        assertFalse(githubDeviceStepsKey(null, null) == githubDeviceStepsKey("code", "device_code"))
        assertFalse(githubDeviceStepsKey("code", "device_code") == githubDeviceStepsKey("done", "device_code"))
    }
}
