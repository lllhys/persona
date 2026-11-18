package persona.feature.settings

import android.content.Context
import android.content.res.Configuration
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.util.Locale
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Machine
import persona.core.contracts.StandingAuthorization
import persona.core.contracts.StandingAuthorizationList
import persona.core.contracts.api.StandingAuthorizationsSource
import persona.core.hub.HubError
import persona.core.hub.HubFailure

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class StandingAuthorizationsTest {
    private fun grant(id: String, actions: List<String> = listOf("merge")) =
        StandingAuthorization(id = id, machineId = "mac", directory = "/code/weather", actions = actions, grantedAt = "2026-11-11T07:36:00Z")

    private class Source(
        private val onList: suspend () -> StandingAuthorizationList,
        private val onRevoke: suspend (String) -> Unit = {},
    ) : StandingAuthorizationsSource {
        override suspend fun list() = onList()
        override suspend fun revoke(id: String) = onRevoke(id)
    }

    @Test fun aRefreshKeepsTheLastListUntilTheNextAnswer() = runBlocking {
        val gate = CompletableDeferred<Unit>()
        var calls = 0
        val source = Source(onList = {
            calls += 1
            if (calls == 1) StandingAuthorizationList(1, listOf(grant("sa1")))
            else {
                gate.await()
                StandingAuthorizationList(2, listOf(grant("sa1"), grant("sa2")))
            }
        })
        val model = StandingAuthorizationsModel(source)
        model.load()
        assertEquals(listOf("sa1"), model.state.value.grants?.map { it.id })
        val job = launch { model.load() }
        yield()
        assertEquals(listOf("sa1"), model.state.value.grants?.map { it.id })
        assertTrue(model.state.value.loading)
        gate.complete(Unit)
        job.join()
        assertEquals(listOf("sa1", "sa2"), model.state.value.grants?.map { it.id })
        assertFalse(model.state.value.loading)
        assertNull(model.state.value.notice)
    }

    @Test fun aForbiddenReadClearsTheList() = runBlocking {
        var forbidden = false
        val source = Source(onList = {
            if (!forbidden) StandingAuthorizationList(1, listOf(grant("sa1")))
            else throw HubFailure(HubError.Server, "full access required", 403, "forbidden")
        })
        val model = StandingAuthorizationsModel(source)
        model.load()
        assertEquals(listOf("sa1"), model.state.value.grants?.map { it.id })
        forbidden = true
        model.load()
        assertNull(model.state.value.grants)
        assertTrue(model.state.value.forbidden)
        assertNull(model.state.value.loadError)
    }

    @Test fun aFailedRefreshKeepsTheRowsAndSaysSo() = runBlocking {
        var fail = false
        val source = Source(onList = {
            if (fail) throw IllegalStateException("offline")
            else StandingAuthorizationList(1, listOf(grant("sa1")))
        })
        val model = StandingAuthorizationsModel(source)
        model.load()
        fail = true
        model.load()
        assertEquals(listOf("sa1"), model.state.value.grants?.map { it.id })
        assertTrue(model.state.value.notice is StandingNotice.DidntRefresh)
        assertFalse(model.state.value.forbidden)
    }

    @Test fun aFailedRevokePutsTheRowBackAndSaysItIsStillInEffect() = runBlocking {
        val first = grant("sa1")
        val second = grant("sa2", listOf("push"))
        val source = Source(onList = { StandingAuthorizationList(1, listOf(first, second)) }, onRevoke = { error("no") })
        val model = StandingAuthorizationsModel(source)
        model.load()
        model.revoke(first)
        assertEquals(listOf("sa1", "sa2"), model.state.value.grants?.map { it.id })
        assertEquals(StandingNotice.RevokeFailed, model.state.value.notice)
    }

    @Test fun aRevokeRemovesTheRowAndALaterReadDoesNotBringItBack() = runBlocking {
        val grant = grant("sa1")
        var listed = listOf(grant)
        val source = Source(onList = { StandingAuthorizationList(1, listed) }, onRevoke = { listed = emptyList() })
        val model = StandingAuthorizationsModel(source)
        model.load()
        model.revoke(grant)
        assertEquals(emptyList<String>(), model.state.value.grants?.map { it.id })
        assertEquals(StandingNotice.Revoked, model.state.value.notice)
        listed = listOf(grant)
        model.load()
        assertEquals(emptyList<String>(), model.state.value.grants?.map { it.id })
        assertEquals(StandingNotice.Revoked, model.state.value.notice)
        model.dismissRevoked()
        assertNull(model.state.value.notice)
    }

    @Test fun theComputerNameIsTheOneOnThatHub() {
        val machines = listOf(
            Machine(id = "mac-mini", name = "First"),
            Machine(id = "mac-mini~g0a1b2c3d", name = "Second"),
        )
        assertEquals("Second", standingMachineName("mac-mini~g0a1b2c3d", machines))
        assertEquals("First", standingMachineName("mac-mini", machines))
        assertEquals("mac-mini~g0eeeeeeee", standingMachineName("mac-mini~g0eeeeeeee", machines))
    }

    @Test fun wordingMatchesTheListAndTheDetailInEnglishAndChinese() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val grant = StandingAuthorization(
            id = "sa1", machineId = "mac", directory = "/code/weather/",
            actions = listOf("merge", "push", "custom"),
            conditions = listOf("ci_green", "no_conflicts"),
            source = "Main conversation",
            grantedAt = "2026-11-11T07:36:00Z",
        )
        fun resources(language: String) = context.createConfigurationContext(
            Configuration(context.resources.configuration).apply { setLocale(Locale.forLanguageTag(language)) },
        ).resources
        val en = resources("en")
        assertEquals("Merge changes, push changes and “custom”", StandingAuthorizationText.actionsTitle(grant, en))
        assertEquals("Only when: All checks pass and No merge conflicts", StandingAuthorizationText.conditionsLine(grant, en))
        assertEquals("Main chat", StandingAuthorizationText.source("the main chat.", en))
        assertEquals("Settings", StandingAuthorizationText.source("设置", en))
        assertTrue(StandingAuthorizationText.source(
            "Suggested rule: the same answer 4 times in 14 days, confirmed with Always.", en,
        ).contains("4"))
        val since = StandingAuthorizationText.since(grant, en)
        assertTrue(since?.startsWith("Since ") == true)
        assertTrue(since!!.contains('\u00A0'))
        val zh = resources("zh-CN")
        assertEquals("合入改动、推送改动和“custom”", StandingAuthorizationText.actionsTitle(grant, zh))
        assertEquals("仅当：所有检查通过和没有合并冲突", StandingAuthorizationText.conditionsLine(grant, zh))
        assertEquals("主对话", StandingAuthorizationText.source("主会话", zh))
        assertEquals("设置", StandingAuthorizationText.source("settings", zh))
        assertTrue(StandingAuthorizationText.since(grant, zh)?.endsWith("起") == true)
    }
}
