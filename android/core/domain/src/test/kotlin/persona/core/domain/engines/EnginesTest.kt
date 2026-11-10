package persona.core.domain.engines

import persona.core.domain.zhAny

import persona.core.domain.zh
import persona.core.contracts.DetectedEngine
import persona.core.contracts.Machine
import org.junit.Assert.*
import org.junit.Test

class EnginesTest {
    private fun machine(
        engine: String = "herdr",
        engines: List<DetectedEngine> = emptyList(),
        online: Boolean = true,
        kind: String = "ssh",
        id: String = "m1",
    ) = Machine(id = id, kind = kind, online = online, engine = engine, engines = engines)

    private val herdr = DetectedEngine("herdr", "herdr", available = true, version = "0.9.3")
    private val core = DetectedEngine("herdr-core", "Herdr App", available = false)
    private val orca = DetectedEngine("orca", "Orca", available = false, note = "打开 Orca")

    @Test fun pairingAsksAboutTheLocalMachineElseTheFirstAndOnlyWhenEnginesAreReported() {
        val a = machine(id = "a")
        val local = machine(id = "b", kind = "local", engines = listOf(herdr))
        assertEquals("b", (Engines.pairingMachine(listOf(a, local))?.id).zhAny())
        assertEquals("a", (Engines.pairingMachine(listOf(a, machine(id = "c")))?.id).zhAny())
        assertNull(Engines.pairingMachine(emptyList()))
        assertEquals("b", (Engines.pairingStep(listOf(a, local))?.id).zhAny())
        // An older hub reports no engines: no step that could not be saved.
        assertNull(Engines.pairingStep(listOf(a, local.copy(engines = emptyList()))))
        assertNull(Engines.pairingStep(emptyList()))
    }

    @Test fun pickerStartsOnTheCurrentEngineHerdrByDefault() {
        // Never chosen: herdr, even when only another engine is detected.
        assertEquals("herdr", (Engines.initialChoice(machine(engines = listOf(herdr.copy(available = false), core.copy(available = true), orca)))).zhAny())
        assertEquals("herdr", (Engines.initialChoice(machine(engine = ""))).zhAny())
        assertEquals("orca", (Engines.initialChoice(machine(engine = "orca", engines = listOf(herdr, core.copy(available = true), orca)))).zhAny())
        assertEquals("herdr-core", (Engines.initialChoice(machine(engine = "herdr-core", engines = listOf(herdr.copy(available = false))))).zhAny())
    }

    @Test fun optionsAreTheHubsListElseTheKnownEngines() {
        val future = DetectedEngine("future", "Future", available = true)
        assertEquals(listOf("herdr", "future"), (Engines.options(machine(engines = listOf(herdr, future))).map { it.id }).zhAny())
        val fallback = Engines.options(machine())
        assertEquals(listOf("herdr", "herdr-core", "orca", "tern", "paseo", "cindy"), (fallback.map { it.id }).zhAny())
        assertEquals(listOf("herdr", "Herdr App", "Orca", "Tern", "Paseo", "Cindy"), (fallback.map { it.name }).zhAny())
        assertTrue(fallback.none { it.available })
    }

    @Test fun unknownEnginesOnlyWhenAvailable() {
        val m = machine(engines = listOf(DetectedEngine("future", "Future", available = true), DetectedEngine("gone", "Gone")))
        assertTrue(Engines.canSelect("future", m))
        assertFalse(Engines.canSelect("gone", m))
        assertFalse(Engines.canSelect("nothing", m))
        // Known engines stay selectable offline and without detection (applied on connect).
        assertTrue(Engines.canSelect("orca", machine(online = false)))
        assertTrue(Engines.canSelect("paseo", machine(online = false)))
        assertTrue(Engines.canSelect("tern", machine(online = false)))
        assertTrue(Engines.canSelect("cindy", machine(online = false)))
        assertEquals("Paseo 里的 Agent", (Engines.detail("paseo")).zhAny())
        assertEquals("Tern 里的终端", (Engines.detail("tern")).zhAny())
        assertEquals("Cindy 里的 Agent", (Engines.detail("cindy")).zhAny())
    }

    @Test fun detectionNamesAndNotes() {
        val m = machine(engines = listOf(herdr, core))
        assertEquals("检测到可用，版本 0.9.3", (Engines.detection("herdr", m)).zhAny())
        assertEquals("没检测到", (Engines.detection("herdr-core", m)).zhAny())
        assertEquals("暂无检测信息", (Engines.detection("orca", m)).zhAny())
        assertEquals("离线，暂无检测信息", (Engines.detection("herdr", machine(online = false, engines = listOf(herdr)))).zhAny())
        assertEquals("Herdr App", (Engines.currentName(machine(engine = "herdr-core"))).zhAny())
        assertEquals("Mine", (Engines.currentName(machine(engine = "x", engines = listOf(DetectedEngine("x", "Mine"))))).zhAny())
        assertEquals("x", (Engines.currentName(machine(engine = "x"))).zhAny())
        assertEquals("桌面 App 里开的终端", (Engines.detail("herdr-core")?.zh()).zhAny())
        assertNull(Engines.detail("x"))
        assertTrue(Engines.footer(machine(online = false)).zh().startsWith("机器离线，选择会在连接后生效"))
        assertTrue(Engines.footer(machine()).zh().startsWith("只显示所选引擎里的会话"))
        assertEquals("已切换引擎", (Engines.saved(machine()).zh()).zhAny())
        assertEquals("已保存，连上后生效", (Engines.saved(machine(online = false)).zh()).zhAny())
    }
}
