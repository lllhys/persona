package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.StandingAuthorization
import persona.core.contracts.StandingAuthorizationList
import persona.core.contracts.api.StandingAuthorizationsSource
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme

/**
 * Standing Authorizations: the list, an empty list, a failed load, one grant, and the revoke confirmation.
 * Compact and wide, Glass / Brief / Board, light and dark, English and Chinese. Native JVM captures.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class StandingAuthorizationsShotsTest {
    @get:Rule val rule = createComposeRule()

    private enum class Scene { List, Empty, Error, Detail, Confirm }

    private data class Shot(val scene: Scene, val wide: Boolean, val style: AppStyle, val dark: Boolean)

    private val grants = listOf(
        StandingAuthorization(
            id = "sa1", machineId = "mac", directory = "/code/weather",
            actions = listOf("merge", "push"), conditions = listOf("ci_green", "no_conflicts"),
            quote = "Merge after checks pass.", source = "Main conversation",
            grantedAt = "2026-11-11T07:36:00Z", reversible = true,
        ),
        StandingAuthorization(
            id = "sa2", machineId = "box", directory = "/work/notes",
            actions = listOf("deploy-test"), source = "settings",
            grantedAt = "2026-11-12T01:00:00Z", reversible = false,
        ),
    )
    private val machines = listOf(Machine(id = "mac", name = "MacBook"), Machine(id = "box", name = ""))

    private inner class Script : StandingAuthorizationsSource {
        var scene = Scene.List
        override suspend fun list(): StandingAuthorizationList = when (scene) {
            Scene.Empty -> StandingAuthorizationList(1, emptyList())
            Scene.Error -> throw IllegalStateException("offline")
            else -> StandingAuthorizationList(1, grants)
        }
        override suspend fun revoke(id: String) {}
    }

    @Test
    @Config(sdk = [35], qualifiers = "en-w1000dp-h2000dp-xhdpi")
    fun english() = matrix("en", "Cancel")

    @Test
    @Config(sdk = [35], qualifiers = "zh-w1000dp-h2000dp-xhdpi")
    fun chinese() = matrix("zh", "取消")

    private fun matrix(language: String, cancel: String) {
        val script = Script()
        var shot by mutableStateOf(Shot(Scene.List, false, AppStyle.Glass, false))
        rule.setContent {
            PersonaTheme(style = shot.style, darkTheme = shot.dark, reduceMotion = true) {
                Box(
                    Modifier
                        .width(if (shot.wide) 900.dp else 400.dp)
                        .height(1400.dp)
                        .background(PersonaTheme.palette.background)
                        .testTag("shot"),
                ) {
                    androidx.compose.runtime.key(shot) {
                        StandingAuthorizationsPage(script, machines, onBack = {})
                    }
                }
            }
        }
        val output = File("build/reports/tests/standing-authorizations-shots").also { it.mkdirs() }
        for (scene in Scene.entries) for (wide in listOf(false, true)) for (style in AppStyle.entries) for (dark in listOf(false, true)) {
            rule.runOnIdle {
                script.scene = scene
                shot = Shot(scene, wide, style, dark)
            }
            rule.waitForIdle()
            val pane = if (wide) "wide" else "compact"
            val tone = if (dark) "dark" else "light"
            val name = "$language-${scene.name.lowercase()}-$pane-${style.name.lowercase()}-$tone"
            when (scene) {
                Scene.List -> {
                    rule.waitUntil(5_000) { rule.onAllNodes(androidx.compose.ui.test.hasTestTag("standing-row-sa1")).fetchSemanticsNodes().isNotEmpty() }
                    rule.onNodeWithTag("standing-row-sa2").assertIsDisplayed()
                    rule.onNodeWithText("/code/weather", substring = true).assertDoesNotExist()
                    rule.onNodeWithText("Merge after checks pass.", substring = true).assertDoesNotExist()
                    save(output, name, rule.onNodeWithTag("shot"))
                }
                Scene.Empty -> {
                    rule.waitUntil(5_000) { rule.onAllNodes(androidx.compose.ui.test.hasTestTag("standing-empty")).fetchSemanticsNodes().isNotEmpty() }
                    save(output, name, rule.onNodeWithTag("shot"))
                }
                Scene.Error -> {
                    rule.waitUntil(5_000) { rule.onAllNodes(androidx.compose.ui.test.hasTestTag("standing-error")).fetchSemanticsNodes().isNotEmpty() }
                    save(output, name, rule.onNodeWithTag("shot"))
                }
                Scene.Detail -> {
                    openFirst()
                    rule.onNodeWithText("/code/weather", substring = true).assertIsDisplayed()
                    save(output, name, rule.onNodeWithTag("shot"))
                }
                Scene.Confirm -> {
                    openFirst()
                    rule.onNodeWithTag("standing-revoke").performScrollTo().performClick()
                    rule.waitForIdle()
                    if (language == "en") rule.onNodeWithText("Undo").assertDoesNotExist()
                    save(output, name, rule.onNode(isDialog()))
                    rule.onNodeWithText(cancel).performClick()
                    rule.waitForIdle()
                }
            }
        }
    }

    private fun openFirst() {
        rule.waitUntil(5_000) { rule.onAllNodes(androidx.compose.ui.test.hasTestTag("standing-row-sa1")).fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithTag("standing-row-sa1").performClick()
        rule.waitForIdle()
        rule.onNodeWithTag("standing-detail").assertIsDisplayed()
    }

    private fun save(dir: File, name: String, node: androidx.compose.ui.test.SemanticsNodeInteraction) {
        val image = node.captureToImage().asAndroidBitmap()
        assertTrue(image.width > 0 && image.height > 0)
        File(dir, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}
