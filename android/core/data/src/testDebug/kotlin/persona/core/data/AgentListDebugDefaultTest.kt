package persona.core.data

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

/** Reads source-set bytecode rather than inheriting a JNI class another test may already have loaded. */
class AgentListDebugDefaultTest {
    @Test fun defaultSelectionCannotReferenceNativeOrShadow() {
        val module = generateSequence(File(".").canonicalFile) { it.parentFile }
            .firstOrNull { File(it, "src/debug/kotlin/persona/core/data/AgentListAdapters.kt").isFile }
            ?: error("Cannot locate the data module")
        val debug = File(module, "src/debug/kotlin/persona/core/data/AgentListAdapters.kt").readText()
        val selection = File(module, "src/debug/kotlin/persona/core/data/AgentListAdapterSelection.kt").readText()
        assertTrue("debug factory defaults to Kotlin", selection.contains("else -> KotlinAgentListAdapter()"))
        assertTrue("shadow is opt-in", selection.contains("\"shadow\" -> ShadowAgentListAdapter()"))
        assertTrue("no eager shadow", !debug.contains("ShadowAgentListAdapter()"))
        assertTrue("no native in common factory", !File(module, "src/main/kotlin/persona/core/data/AgentListAdapter.kt").readText().contains("rustcore"))
        assertTrue("no native in Release", !File(module, "src/release/kotlin/persona/core/data/AgentListAdapters.kt").readText().contains("rustcore"))
    }
}
