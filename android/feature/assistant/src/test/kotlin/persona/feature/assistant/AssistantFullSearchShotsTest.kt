package persona.feature.assistant

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File
import java.time.Instant

/**
 * Full-window search on a regular window (#9171): empty query and filled results, Glass / Brief / Board, light and
 * dark. PNGs under `build/full-search-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w700dp-h900dp-xhdpi")
class AssistantFullSearchShotsTest {
    @get:Rule val rule = createComposeRule()

    private val minutesAgo = { m: Long -> Instant.now().minusSeconds(m * 60).toString() }
    private val chats = listOf(
        Session(machineId = "hub", sessionId = "main", title = "Main chat", status = "idle",
            updatedAt = minutesAgo(2), lastText = "Today's plan is ready.", assistant = true),
        Session(machineId = "hub", sessionId = "trip", title = "Weekend trip", status = "idle",
            updatedAt = minutesAgo(20), lastText = "Pack the bags by Friday.", assistant = true),
        Session(machineId = "hub", sessionId = "report", title = "Weekly report", status = "done",
            updatedAt = minutesAgo(40), lastText = "Second draft is in.", assistant = true),
    )
    private val words = ConversationWords("Chat", "Hand to Persona", "Hand to Persona: %1\$s")

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/full-search-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) {
            if (bitmap.getPixel(x, y) != background) other++
        }
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun emptySearchInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("shot")) {
                    AssistantFullSearch(
                        chats = chats, known = chats, searchOn = false, management = null, hubKey = "test",
                        words = words, mainName = "Persona", mainKey = chats.first().key,
                        onOpen = {}, onDismiss = {}, overlay = false,
                    )
                }
            }
        }
        rule.onNodeWithTag("full-search").assertIsDisplayed()
        rule.onNodeWithTag("full-search-field").assertIsDisplayed()
        rule.onNodeWithTag("full-search-close").assertIsDisplayed()
        rule.onNodeWithText("Recent").assertIsDisplayed()
        rule.onNodeWithText("Weekend trip").assertIsDisplayed()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            androidx.compose.runtime.snapshots.Snapshot.sendApplyNotifications()
            capture("full-search-empty-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }

    @Test fun filledSearchInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("shot")) {
                    AssistantFullSearch(
                        chats = chats, known = chats, searchOn = false, management = null, hubKey = "test",
                        words = words, mainName = "Persona", mainKey = chats.first().key,
                        onOpen = {}, onDismiss = {}, initialQuery = "bags", overlay = false,
                    )
                }
            }
        }
        rule.onNodeWithText("Weekend trip").assertIsDisplayed()
        rule.onNodeWithText("Pack the bags by Friday.").assertIsDisplayed()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            androidx.compose.runtime.snapshots.Snapshot.sendApplyNotifications()
            capture("full-search-filled-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }
}
