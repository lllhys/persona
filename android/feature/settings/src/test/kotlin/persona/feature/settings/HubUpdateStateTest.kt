package persona.feature.settings

import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import persona.core.contracts.api.*
import persona.core.designsystem.JLoadable
import persona.core.hub.HubError
import persona.core.hub.HubFailure

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class HubUpdateStateTest {
    private val res = RuntimeEnvironment.getApplication().resources
    private val current = HubUpdateStatus(version = "1.42.0", build = 420, channel = "release", canUpdate = true, state = "idle")
    private val newer = current.copy(state = "available", latest = HubRelease("1.43.0", 431, newer = true, notes = "更快的同步"))

    private class FakeUpdates(var status: HubUpdateStatus, var history: List<HubUpdateRecord> = emptyList()) : HubUpdates {
        val calls = mutableListOf<String>()
        var fail: Exception? = null
        var historyReads = 0
        var next: HubUpdateStatus? = null
        override suspend fun status(): HubUpdateStatus { calls += "status"; fail?.let { throw it }; return status }
        override suspend fun check(): HubUpdateStatus { calls += "check"; fail?.let { throw it }; return next ?: status }
        override suspend fun start(now: Boolean): HubUpdateStatus { calls += "start now=$now"; fail?.let { throw it }; return next ?: status }
        override suspend fun settings(auto: Boolean?, noticeSeen: Boolean?): HubUpdateStatus {
            calls += "settings auto=$auto seen=$noticeSeen"; fail?.let { throw it }; return next ?: status
        }
        override suspend fun history(): List<HubUpdateRecord> { historyReads++; return history }
    }

    @Test fun loadsTheStatusAndTheHistory() = runBlocking {
        val api = FakeUpdates(newer, listOf(HubUpdateRecord("1.41.0", "1.42.0", 420, result = "updated")))
        val model = HubUpdateModel(api)
        model.load()
        val s = model.state.value
        assertEquals(newer, s.status)
        assertEquals(1, s.history.size)
        assertNull(s.error)
    }

    @Test fun aHubFromBeforeUpdatesSaysSo() = runBlocking {
        val api = FakeUpdates(current).apply { fail = HubUpdatesUnsupported() }
        val model = HubUpdateModel(api)
        model.load()
        assertTrue(model.state.value.unsupported)
        assertNull(model.state.value.status)
    }

    @Test fun aFailedReadSaysWhy() = runBlocking {
        val api = FakeUpdates(current).apply { fail = HubFailure(HubError.Server, "数据库没连上", 500) }
        val model = HubUpdateModel(api)
        model.load()
        assertEquals("读不到 Hub 的版本：数据库没连上", model.state.value.error?.resolve(res))
    }

    @Test fun startsAndShowsTheHubsRefusal() = runBlocking {
        val api = FakeUpdates(newer)
        val model = HubUpdateModel(api)
        model.load()
        api.next = newer.copy(state = "waiting_idle")
        model.update(now = false)
        assertEquals("waiting_idle", model.state.value.status?.state)
        model.update(now = true)
        api.fail = HubFailure(HubError.Server, "正在更新", 409)
        model.update(now = false)
        assertEquals(listOf("status", "start now=false", "start now=true", "start now=false"), api.calls)
        assertEquals("没能开始更新：正在更新", model.state.value.actionError?.resolve(res))
        assertFalse(model.state.value.sending)
    }

    @Test fun settingsAndTheNotice() = runBlocking {
        val api = FakeUpdates(current.copy(auto = true, autoNotice = true))
        val model = HubUpdateModel(api)
        model.load()
        api.next = current.copy(auto = false)
        model.setAuto(false)
        model.noticeSeen()
        assertEquals(listOf("status", "settings auto=false seen=null", "settings auto=null seen=true"), api.calls)
        assertFalse(model.state.value.status!!.auto)
    }

    @Test fun anUpdateThatEndsReadsTheHistoryAgain() = runBlocking {
        val api = FakeUpdates(current.copy(state = "confirming"))
        val model = HubUpdateModel(api)
        model.load()
        val reads = api.historyReads
        model.receive(current.copy(state = "confirming"))
        assertEquals(reads, api.historyReads)
        api.history = listOf(HubUpdateRecord("1.42.0", "1.43.0", 431, result = "updated"))
        model.receive(current.copy(version = "1.43.0", state = "updated"))
        assertEquals(reads + 1, api.historyReads)
        assertEquals("1.43.0", model.state.value.history.single().toVersion)
        // The same state again is not another end.
        model.receive(current.copy(version = "1.43.0", state = "updated"))
        assertEquals(reads + 1, api.historyReads)
    }

    @Test fun checkFailureIsShownAndCheckingEnds() = runBlocking {
        val api = FakeUpdates(current)
        val model = HubUpdateModel(api)
        model.load()
        api.fail = HubFailure(HubError.Server, "查不了新版本：超时", 502)
        model.check()
        assertEquals("检查失败：查不了新版本：超时", model.state.value.actionError?.resolve(res))
        assertFalse(model.state.value.checking)
    }

    // A hub_update push that lands while the page is still asking is newer than the answer that follows it.
    @Test fun aPushDuringTheFirstReadIsKept() = runBlocking {
        lateinit var model: HubUpdateModel
        val pushed = current.copy(state = "downloading")
        val api = object : HubUpdates {
            override suspend fun status(): HubUpdateStatus { model.apply(pushed); return current }
            override suspend fun check() = current
            override suspend fun start(now: Boolean) = current
            override suspend fun settings(auto: Boolean?, noticeSeen: Boolean?) = current
            override suspend fun history() = emptyList<HubUpdateRecord>()
        }
        model = HubUpdateModel(api)
        model.load()
        assertEquals("downloading", model.state.value.status?.state)
    }

    // The same words as iOS HubUpdateStatus / the web's lib/hub-update.ts.
    @Test fun texts() {
        assertEquals("发布版", HubUpdateText.channel("release", res))
        assertEquals("Docker", HubUpdateText.channel("docker", res))
        assertEquals("开发版", HubUpdateText.channel("dev", res))
        assertEquals("等助手和 Agent 空闲", HubUpdateText.state(current.copy(state = "waiting_idle"), res))
        assertEquals("等 2 个在忙的结束", HubUpdateText.state(current.copy(state = "waiting_idle", busy = listOf(HubUpdateBusy(), HubUpdateBusy())), res))
        assertEquals("已是最新", HubUpdateText.state(current, res))
        assertEquals("1.42.0", HubUpdateText.summary(current, res))
        assertEquals("有新版本", HubUpdateText.summary(newer, res))
        assertEquals("有新镜像", HubUpdateText.summary(newer.copy(channel = "docker"), res))
        assertEquals("正在下载", HubUpdateText.summary(newer.copy(state = "downloading"), res))
        assertNull(HubUpdateText.summary(null, res))
        assertEquals("没有自动装", HubUpdateText.record("held", res))
        assertEquals("没更新成功", HubUpdateText.record("failed", res))
    }

    @Test fun whatTheUpdateGroupShows() {
        assertEquals(HubUpdateView.Docker, HubUpdateText.view(current.copy(channel = "docker", canUpdate = false), res))
        assertEquals(HubUpdateView.Cannot("这个 Hub 不会自己更新：开发版。"), HubUpdateText.view(current.copy(canUpdate = false, whyNot = "开发版"), res))
        assertEquals(HubUpdateView.Cannot("这个 Hub 不会自己更新。"), HubUpdateText.view(current.copy(canUpdate = false), res))
        assertEquals(HubUpdateView.Newer(false), HubUpdateText.view(newer, res))
        assertEquals(HubUpdateView.Newer(true), HubUpdateText.view(newer.copy(state = "downloading"), res))
        assertEquals(HubUpdateView.Working, HubUpdateText.view(current.copy(state = "restarting"), res))
        assertEquals(HubUpdateView.Current, HubUpdateText.view(current, res))
    }

    @Test fun warningsUnderANewerRelease() {
        assertNull(HubUpdateText.latestWarning(current, res))
        assertNull(HubUpdateText.latestWarning(newer, res))
        assertEquals("这一版上次装完起不来，已经回滚过；自动更新不会再装它。",
            HubUpdateText.latestWarning(newer.copy(latest = newer.latest!!.copy(bad = true)), res))
        val held = newer.copy(latest = newer.latest!!.copy(held = "代码比 hub 旧"))
        // Held matters only while auto-update is on: off, nothing installs by itself anyway.
        assertNull(HubUpdateText.latestWarning(held, res))
        assertEquals("自动更新不会装这一版：代码比 hub 旧。确认没问题可以手动更新。", HubUpdateText.latestWarning(held.copy(auto = true), res))
    }

    @Test fun updateTimesUseThePagesLanguageForChecksReleasesAndHistory() {
        val context = RuntimeEnvironment.getApplication()
        val now = java.time.Instant.parse("2026-01-02T12:00:00Z")
        val at = "2026-01-02T09:00:00Z"
        val status = newer.copy(lastCheck = at, latest = newer.latest!!.copy(released = at))
        val record = HubUpdateRecord("1.0", "1.1", result = "updated", started = now.toString(), ended = at)
        for (language in listOf("zh", "en", "ja")) {
            val config = android.content.res.Configuration(context.resources.configuration).apply {
                setLocale(java.util.Locale.forLanguageTag(language))
            }
            val resources = context.createConfigurationContext(config).resources
            val ago = resources.getQuantityString(R.plurals.settings_src_hours_ago, 3, 3)
            assertEquals(language, ago, HubUpdateText.lastCheck(status, resources, now))
            assertEquals(language, ago, HubUpdateText.recordTime(record, resources, now))
            assertEquals(language, ago, HubUpdateText.recordTime(record.copy(ended = null, started = at), resources, now))
            assertEquals(language, resources.getString(R.string.settings_hub_update_released, ago),
                HubUpdateText.released(status, resources, now))
            assertNull(HubUpdateText.lastCheck(status.copy(lastCheck = "invalid"), resources, now))
            assertNull(HubUpdateText.recordTime(record.copy(ended = "invalid"), resources, now))
        }
    }

    @Test fun aDockerHubsSteps() {
        val docker = current.copy(channel = "docker", canUpdate = false, image = HubImage("ghcr.io/example/persona", "1.42.0", pinned = true))
        val (none, plain) = HubUpdateText.dockerSteps(docker, res)
        assertNull(none)
        assertEquals(listOf(HubUpdateText.DockerCommand), plain.map { it.copy })
        val withNew = docker.copy(latest = HubRelease("1.43.0", newer = true))
        val (headline, pinned) = HubUpdateText.dockerSteps(withNew, res)
        assertEquals("新镜像 1.43.0", headline)
        assertEquals(listOf("ghcr.io/example/persona:1.43.0", HubUpdateText.DockerCommand), pinned.map { it.copy })
        val (_, latestTag) = HubUpdateText.dockerSteps(withNew.copy(image = docker.image!!.copy(tag = "latest", pinned = false)), res)
        assertEquals(listOf(HubUpdateText.DockerCommand), latestTag.map { it.copy })
    }

    @Test fun theLoadableFollowsStatusErrorAndReading() {
        assertEquals(JLoadable.Loading, HubUpdateState().loadable(null))
        assertEquals(JLoadable.Failed("down"), HubUpdateState(error = SettingsUiText.Raw("down")).loadable("down"))
        // A retry shows the spinner, not the error it is retrying.
        assertEquals(JLoadable.Loading, HubUpdateState(loading = true, error = SettingsUiText.Raw("down")).loadable("down"))
        assertEquals(JLoadable.Loaded(current), HubUpdateState(status = current).loadable(null))
        assertEquals(JLoadable.Loaded(current, refreshing = true), HubUpdateState(status = current, checking = true).loadable(null))
    }

    @Test fun aFailedReadOverAStatusStaysAsALineOverIt() {
        assertEquals(JLoadable.Loaded(current, failure = "down"), HubUpdateState(status = current, error = SettingsUiText.Raw("down")).loadable("down"))
        assertEquals(JLoadable.Loaded(current), HubUpdateState(status = current, loading = true, error = SettingsUiText.Raw("down")).loadable("down"))
    }

    @Test fun loadingIsOnWhileTheStatusIsReadAndOffAfterAFailure() = runBlocking {
        lateinit var model: HubUpdateModel
        val seen = mutableListOf<Boolean>()
        val api = object : HubUpdates {
            override suspend fun status(): HubUpdateStatus { seen += model.state.value.loading; throw HubFailure(HubError.Server, "数据库没连上", 500) }
            override suspend fun check() = current
            override suspend fun start(now: Boolean) = current
            override suspend fun settings(auto: Boolean?, noticeSeen: Boolean?) = current
            override suspend fun history() = emptyList<HubUpdateRecord>()
        }
        model = HubUpdateModel(api)
        model.load()
        assertEquals(listOf(true), seen)
        assertFalse(model.state.value.loading)
        assertEquals(JLoadable.Failed("读不到 Hub 的版本：数据库没连上"), model.state.value.loadable(model.state.value.error?.resolve(res)))
    }

    @Test fun aRetryThatSucceedsReplacesTheFailureWithTheStatus() = runBlocking {
        val api = FakeUpdates(newer).apply { fail = HubFailure(HubError.Server, "数据库没连上", 500) }
        val model = HubUpdateModel(api)
        model.load()
        assertNotNull(model.state.value.error)
        api.fail = null
        model.load()
        assertNull(model.state.value.error)
        assertEquals(JLoadable.Loaded(newer), model.state.value.loadable(null))
    }

    @Test fun aFailedReloadKeepsTheStatusAlreadyShown() = runBlocking {
        val api = FakeUpdates(newer)
        val model = HubUpdateModel(api)
        model.load()
        api.fail = HubFailure(HubError.Server, "数据库没连上", 500)
        model.load()
        val state = model.state.value
        assertEquals(newer, state.status)
        assertEquals(JLoadable.Loaded(newer, failure = "读不到 Hub 的版本：数据库没连上"), state.loadable(state.error?.resolve(res)))
    }
}
