package persona.feature.settings

import android.content.Context
import android.content.res.Resources
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.*
import persona.core.designsystem.Status
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import java.time.Instant
import java.time.ZoneOffset

internal val ManagersNow: Instant = Instant.parse("2026-11-05T06:00:00Z")
internal val ManagersSample = listOf(
    HubManager("assistant", "助手", online = true, isDefault = true, builtin = true, host = "server", os = "linux", arch = "amd64",
        deploy = "child", version = "1.0.0", since = "2026-11-05T01:00:00Z"),
    HubManager("m-3fa21c", "工作", online = true, host = "MacBook", os = "darwin", arch = "arm64", deploy = "service", version = "1.0.0",
        since = "2026-11-05T05:18:00Z"),
    HubManager("m-8b04d2", "家里", online = false, host = "NAS", os = "linux", deploy = "docker", version = "0.9.8", lastSeen = "2026-11-05T03:00:00Z"),
    HubManager("m-c1e9f0", "测试", installing = "2026-11-05T06:07:00Z"),
)

/** A hub answering Settings › Managers from memory, as the mock hub does. */
internal class FakeManagers(list: List<HubManager> = ManagersSample) : HubManagers {
    var list = list.toMutableList()
    var fail: Exception? = null
    var unsupported = false
    var reads = 0
    val calls = mutableListOf<String>()
    var commands: ManagerCommands? = null

    override suspend fun roster(): ManagerRoster {
        reads++
        if (unsupported) throw ManagersUnsupported()
        fail?.let { throw it }
        return ManagerRoster(list.toList(), list.firstOrNull { it.isDefault }?.id.orEmpty())
    }
    override suspend fun create(name: String): ManagerCode {
        calls += "create $name"; fail?.let { throw it }
        val m = HubManager("m-new", name, installing = "2026-11-05T06:10:00Z")
        list += m
        return ManagerCode("mi_0123456789abcdef0123456789abcdef", "2026-11-05T06:10:00Z", m, commands)
    }
    /** Ids the hub no longer has (a never-installed one whose code ran out): their new code is 404 no_manager. */
    var gone = emptySet<String>()
    override suspend fun newCode(id: String): ManagerCode {
        calls += "code $id"; fail?.let { throw it }
        if (id in gone) throw HubFailure(HubError.Server, "no such manager", 404, "no_manager")
        return ManagerCode("mi_fedcba9876543210fedcba9876543210", "2026-11-05T06:10:00Z", commands = commands)
    }
    override suspend fun rename(id: String, name: String): HubManager {
        calls += "rename $id $name"; fail?.let { throw it }
        list = list.map { if (it.id == id) it.copy(name = name) else it }.toMutableList()
        return list.first { it.id == id }.copy(isDefault = false)
    }
    override suspend fun makeDefault(id: String): HubManager {
        calls += "default $id"; fail?.let { throw it }
        list = list.map { it.copy(isDefault = it.id == id) }.toMutableList()
        return list.first { it.id == id }
    }
    override suspend fun delete(id: String) {
        calls += "delete $id"; fail?.let { throw it }
        list.removeAll { it.id == id }
    }
}

/** This device's prefs as a hub keeps them: only `muted_managers` matters to 推送通知. */
internal class FakeDevices(var muted: List<String>? = emptyList()) : DeviceSettings {
    var fail: Exception? = null
    val saved = mutableListOf<List<String>>()
    override suspend fun devices() = PairedDevices(emptyList(), null)
    override suspend fun revoke(credentialId: String) = Unit
    override suspend fun preferences() = DeviceNotificationPreferences(mutedManagers = muted)
    override suspend fun savePreferences(preferences: DeviceNotificationPreferences) = preferences
    override suspend fun saveMutedManagers(ids: List<String>): List<String> {
        saved += ids
        fail?.let { throw it }
        return ids.also { muted = it }
    }
}

private fun refused(status: Int, code: String? = null) = HubFailure(HubError.Server, "refused", status, code)

// Robolectric: the words come from the app's resources, in Simplified Chinese (robolectric.properties).
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ManagersStateTest {
    private val res: Resources get() = ApplicationProvider.getApplicationContext<Context>().resources
    private val zone = ZoneOffset.UTC
    private val own = ManagersSample[0]
    private val work = ManagersSample[1]
    private val home = ManagersSample[2]
    private val test = ManagersSample[3]

    @Test fun whereEachRuns() {
        assertEquals("server · Linux · hub 自己的", ManagerText.where(res, own))
        assertEquals("MacBook · macOS · 服务", ManagerText.where(res, work))
        assertEquals("NAS · Linux · 容器", ManagerText.where(res, home))
        // Still installing: it said nothing yet.
        assertNull(ManagerText.where(res, test))
        assertEquals("hub 自己的", ManagerText.where(res, HubManager("x", deploy = "child")))
        assertEquals("Windows · 单独运行", ManagerText.where(res, HubManager("x", os = "windows", deploy = "external")))
        assertEquals("freebsd · 服务", ManagerText.where(res, HubManager("x", os = "freebsd", deploy = "systemd")))
        assertEquals("服务", ManagerText.deploy(res, HubManager("x", deploy = "launchd")))
        assertEquals("容器", ManagerText.deploy(res, HubManager("x", deploy = "container")))
        assertEquals("nomad", ManagerText.deploy(res, HubManager("x", deploy = "nomad")))
        // The hub's own is that whatever it said.
        assertEquals("hub 自己的", ManagerText.deploy(res, HubManager("x", builtin = true, deploy = "service")))
        assertNull(ManagerText.where(res, HubManager("x", host = " ", os = "", deploy = "")))
    }

    @Test fun statusLinesAndDots() {
        assertEquals("在线", ManagerText.statusLine(res, own, ManagersNow, zone))
        assertEquals("离线 · 最后同步于 3 小时前", ManagerText.statusLine(res, home, ManagersNow, zone))
        assertEquals("离线", ManagerText.statusLine(res, HubManager("x"), ManagersNow, zone))
        assertEquals("安装中 · 码 7 分钟后过期", ManagerText.statusLine(res, test, ManagersNow, zone))
        // Rounded up: 30 seconds left is still a minute.
        assertEquals("安装中 · 码 1 分钟后过期", ManagerText.statusLine(res, test, Instant.parse("2026-11-05T06:06:30Z"), zone))
        assertEquals("安装中 · 码已过期", ManagerText.statusLine(res, test, Instant.parse("2026-11-05T06:07:00Z"), zone))
        assertEquals(Status.Unread, ManagerText.status(own))
        assertEquals(Status.Offline, ManagerText.status(home))
        assertEquals(Status.Waiting, ManagerText.status(test))
        assertEquals("连上于" to "42 分钟前", ManagerText.seen(res, work, ManagersNow, zone))
        assertEquals("最后同步于" to "3 小时前", ManagerText.seen(res, home, ManagersNow, zone))
        assertNull(ManagerText.seen(res, test, ManagersNow, zone))
    }

    @Test fun theCountdownAndNames() {
        assertEquals("7:00", ManagerText.countdown("2026-11-05T06:07:00Z", ManagersNow))
        assertEquals("0:05", ManagerText.countdown("2026-11-05T06:00:05Z", ManagersNow))
        assertNull(ManagerText.countdown("2026-11-05T06:00:00Z", ManagersNow))
        assertNull(ManagerText.countdown("not a time", ManagersNow))
        assertTrue(ManagerText.validName("工作"))
        assertTrue(ManagerText.validName("字".repeat(40)))
        assertFalse(ManagerText.validName("字".repeat(41)))
        assertFalse(ManagerText.validName("   "))
        assertEquals(R.string.settings_managers_delete_builtin, ManagerText.whyNoDelete(own))
        assertEquals(R.string.settings_managers_delete_default, ManagerText.whyNoDelete(work.copy(isDefault = true)))
        assertNull(ManagerText.whyNoDelete(work))
    }

    @Test fun theHubsRefusalsInWords() {
        fun words(e: Exception) = ManagerText.failure(e).resolve(res)
        assertEquals("这是默认助手，先把别的设为默认再删除", words(refused(409, "default_manager")))
        assertEquals("这是 hub 自己运行的助手，不能删除或换地方装", words(refused(409, "builtin_manager")))
        assertEquals("这个助手已经不在了", words(refused(404, "no_manager")))
        assertEquals("它还没装好，装好后才能改名或设为默认", words(refused(409, "installing")))
        assertEquals("名字要 1 到 40 个字", words(refused(400)))
        assertEquals("这台设备没有修改权限", words(refused(403, "read_only")))
        // Anything else: what did not happen, and the app's usual reason.
        assertEquals("没有改成：连不上 hub", words(HubFailure(HubError.Timeout)))
    }

    @Test fun theModelReadsChangesAndOpensTheCodeSheet() = runBlocking {
        val api = FakeManagers()
        val model = ManagersModel(api)
        model.load()
        assertEquals(listOf("assistant", "m-3fa21c", "m-8b04d2", "m-c1e9f0"), model.state.value.managers.map { it.id })

        assertTrue(model.create(" 实验室 "))
        assertEquals("create 实验室", api.calls.last())
        val sheet = model.state.value.code!!
        assertEquals("m-new", sheet.managerId)
        assertEquals("实验室", sheet.name)
        assertTrue(model.state.value.managers.any { it.id == "m-new" })
        model.closeCode()
        assertNull(model.state.value.code)

        // A name the hub would refuse is not sent.
        assertFalse(model.create(""))
        assertEquals("名字要 1 到 40 个字", model.state.value.failure?.resolve(res))
        assertEquals("create 实验室", api.calls.last())

        assertTrue(model.rename("m-3fa21c", "公司"))
        assertEquals("公司", model.state.value.manager("m-3fa21c")?.name)
        assertTrue(model.makeDefault("m-3fa21c"))
        assertEquals(listOf("m-3fa21c"), model.state.value.managers.filter { it.isDefault }.map { it.id })

        assertTrue(model.newCode("m-8b04d2"))
        assertEquals(ManagerCodeSheet("m-8b04d2", "家里", api.newCode("m-8b04d2")), model.state.value.code)

        assertTrue(model.delete("m-c1e9f0"))
        assertNull(model.state.value.manager("m-c1e9f0"))

        api.fail = refused(409, "default_manager")
        assertFalse(model.delete("m-3fa21c"))
        assertEquals("这是默认助手，先把别的设为默认再删除", model.state.value.failure?.resolve(res))
        assertFalse(model.state.value.busy)
    }

    @Test fun anOldHubIsUnsupportedAndAFailedReadSaysSo() = runBlocking {
        val api = FakeManagers().apply { unsupported = true }
        val model = ManagersModel(api)
        model.load()
        assertTrue(model.state.value.unsupported)
        assertNull(model.state.value.roster)
        api.unsupported = false
        api.fail = HubFailure(HubError.Timeout)
        model.load()
        assertEquals("读取失败：连不上 hub", model.state.value.error?.resolve(res))
        assertFalse(model.state.value.unsupported)
    }

    @Test fun thePushSwitchMovesAtOnceAndGoesBackWhenRefused() = runBlocking {
        val devices = FakeDevices(listOf("m-8b04d2"))
        val model = ManagerPushModel(devices)
        assertNull(model.state.value.pushes(work.id))
        model.load()
        assertEquals(true, model.state.value.pushes(work.id))
        assertEquals(false, model.state.value.pushes(home.id))
        // Off: the whole list goes, this one added.
        assertTrue(model.set(work.id, false))
        assertEquals(listOf("m-8b04d2", "m-3fa21c"), devices.saved.last())
        assertEquals(false, model.state.value.pushes(work.id))
        // Already where it is: nothing sent.
        assertFalse(model.set(work.id, false))
        assertEquals(1, devices.saved.size)
        // Refused: back where it was, and why.
        devices.fail = refused(500)
        assertFalse(model.set(home.id, true))
        assertEquals(listOf("m-3fa21c"), devices.saved.last())
        assertEquals(false, model.state.value.pushes(home.id))
        assertTrue(model.state.value.failure!!.resolve(res).startsWith("推送通知没改成"))
        assertFalse(model.state.value.busy)
    }

    @Test fun aHubWithoutTheListHasNoSwitch() = runBlocking {
        val model = ManagerPushModel(FakeDevices(muted = null))
        model.load()
        assertNull(model.state.value.pushes(work.id))
        assertFalse(model.set(work.id, false))
    }

    /** A never-installed one whose code ran out is gone from the roster: New install code makes it again, same name. */
    @Test fun aNeverInstalledOnesNewCodeMakesItAgain() = runBlocking {
        val api = FakeManagers()
        val model = ManagersModel(api)
        model.load()
        // Still installing (测试 m-c1e9f0), its code ran out and the hub dropped it.
        api.gone = setOf("m-c1e9f0"); api.list.removeAll { it.id == "m-c1e9f0" }
        // The page still has it from the last read.
        assertTrue(model.newCode("m-c1e9f0"))
        assertEquals(listOf("code m-c1e9f0", "create 测试"), api.calls.takeLast(2))
        val sheet = model.state.value.code!!
        assertEquals("m-new", sheet.managerId)
        assertEquals("测试", sheet.name)
        assertTrue(sheet.fresh)
        // The list is read again: the new one is there.
        assertEquals("测试", model.state.value.manager("m-new")?.name)

        // Just made in the app, its code sheet still open after it ran out: the sheet's New install code does the same.
        api.gone = setOf("m-new"); api.list.removeAll { it.id == "m-new" }; model.load()
        assertNull(model.state.value.manager("m-new"))
        assertTrue(model.newCode("m-new"))
        assertEquals(listOf("code m-new", "create 测试"), api.calls.takeLast(2))
    }

    @Test fun anInstalledOnesNoManagerStaysAnError() = runBlocking {
        val api = FakeManagers()
        val model = ManagersModel(api)
        model.load()
        // Install elsewhere for 工作 (installed): the hub has no such manager any more, that is all.
        api.gone = setOf("m-3fa21c")
        assertFalse(model.newCode("m-3fa21c"))
        assertEquals("code m-3fa21c", api.calls.last())
        assertTrue(api.calls.none { it.startsWith("create") })
        assertEquals("这个助手已经不在了", model.state.value.failure?.resolve(res))
        assertNull(model.state.value.code)
    }
}
