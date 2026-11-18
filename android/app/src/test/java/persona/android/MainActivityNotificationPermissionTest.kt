package persona.android

import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/** Notification permission is an explicit readiness action, never a startup, pairing or deep-link side effect. */
class MainActivityNotificationPermissionTest {
    private val permissionRequests = listOf(
        Regex("""(?s)\.launch\s*\([^)]*POST_NOTIFICATIONS"""),
        Regex("""(?s)\brequestPermissions\s*\([^)]*POST_NOTIFICATIONS"""),
    )

    private fun repositoryRoot(start: File): File {
        val root = generateSequence(start.canonicalFile) { it.parentFile }
            .firstOrNull { File(it, "android/app/src/main").isDirectory }
        assertNotNull("The Android app sources must exist in the current checkout", root)
        return requireNotNull(root)
    }

    @Test
    fun startupPairingAndDeepLinkSourcesDoNotRequestNotificationPermission() {
        val root = repositoryRoot(File("."))
        val appSources = listOf("main", "debug").flatMap { sourceSet ->
            File(root, "android/app/src/$sourceSet").walkTopDown()
                .filter { it.isFile && it.extension == "kt" }
                .toList()
        }
        val pairing = File(root, "android/feature/settings/src/main/kotlin/persona/feature/settings/PairingScreen.kt")
        assertTrue("PairingScreen.kt must exist", pairing.isFile)

        // Readiness and phone tools live outside these roots and keep their explicit user-action permission flows.
        val offenders = (appSources + pairing).filter { source ->
            val content = source.readText()
            permissionRequests.any { it.containsMatchIn(content) }
        }
        assertTrue(
            "Startup, Application, pairing and deep-link sources must not request notification permission: " +
                offenders.joinToString { it.relativeTo(root).path },
            offenders.isEmpty(),
        )
    }

    @Test
    fun sourceGuardDistinguishesARequestFromAReadOnlyGrantCheck() {
        assertTrue(permissionRequests.any { it.containsMatchIn("ask.launch(Manifest.permission.POST_NOTIFICATIONS)") })
        assertFalse(permissionRequests.any {
            it.containsMatchIn("checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)")
        })
    }

    @Test
    fun mainActivityHasNoRuntimePermissionLauncher() {
        val root = repositoryRoot(File("."))
        val content = File(root, "android/app/src/main/java/persona/android/MainActivity.kt").readText()
        for (forbidden in listOf("RequestPermission", "requestPermissions(")) {
            assertFalse("MainActivity must leave runtime permissions to their explicit screens: $forbidden", forbidden in content)
        }
    }
}
