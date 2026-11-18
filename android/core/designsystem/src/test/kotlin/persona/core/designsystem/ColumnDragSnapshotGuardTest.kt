package persona.core.designsystem

import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.File

/** Release code must not reflect into Compose. R8 can drop an internal method the name points at. */
class ColumnDragSnapshotGuardTest {
    @Test fun mainSourceDoesNotReflectIntoCompose() {
        val root = repositoryFile("android/core/designsystem/src/main/kotlin/persona/core/designsystem/SidebarColumnDrag.kt")
            .parentFile ?: error("design-system package directory is missing")
        val hits = root.walkTopDown().filter { it.extension == "kt" }.flatMap { file ->
            file.readLines().mapIndexedNotNull { index, line ->
                val code = line.substringBefore("//")
                if (code.contains("ui_graphics") || code.contains("getMethod(")) {
                    "${file.name}:${index + 1}: ${line.trim()}"
                } else {
                    null
                }
            }
        }.toList()
        assertEquals(hits.joinToString("\n"), 0, hits.size)
    }

    private fun repositoryFile(path: String): File {
        val start = System.getProperty("user.dir") ?: error("no working directory")
        return generateSequence(File(start)) { it.parentFile }
            .map { File(it, path) }.first { it.isFile }
    }
}
