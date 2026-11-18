package persona.core.data

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Compiled against the release source set: no native/shadow class is available to this test. */
class AgentListReleaseIsolationTest {
    @Test fun releaseIsAlwaysKotlinEvenWhenTheDebugPropertyRequestsShadow() {
        System.setProperty("persona.sort", "shadow")
        try {
            assertTrue(AgentListAdapters.create() is KotlinAgentListAdapter)
            assertEquals(AgentListAdapters::class.java.methods.single { it.name == "create" }.parameterCount, 0)
        } finally {
            System.clearProperty("persona.sort")
        }
        for (name in listOf(
            "persona.core.data.ShadowAgentListAdapter",
            "persona.core.data.ShadowAgentListDiagnostics",
            "persona.core.data.AgentListMismatch",
        )) {
            try {
                Class.forName(name)
                throw AssertionError("Release must not contain $name")
            } catch (_: ClassNotFoundException) {}
        }
    }

    @Test fun releaseSourceSetHasNoNativeOrShadowImplementation() {
        val module = generateSequence(File(".").canonicalFile) { it.parentFile }
            .firstOrNull { File(it, "build.gradle.kts").isFile && File(it, "src/main/kotlin/persona/core/data").isDirectory }
            ?: error("Cannot locate the data module")
        val source = File(module, "src/release/kotlin/persona/core/data/AgentListAdapters.kt").readText()
        assertFalse(source.contains("Shadow"))
        assertFalse(source.contains("rustcore"))
        assertFalse(source.contains("rustcore"))
        assertFalse(File(module, "src/release/kotlin/persona/core/data/ShadowAgentListAdapter.kt").exists())
    }
}
