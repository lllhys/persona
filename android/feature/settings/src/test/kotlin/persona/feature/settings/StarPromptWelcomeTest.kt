package persona.feature.settings

import android.content.Context
import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasScrollToNodeAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isEnabled
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.SessionsRepository
import persona.core.designsystem.AppStyle
import persona.core.designsystem.JToastHost
import persona.core.designsystem.LocalStarPrompt
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.StarPromptStore
import persona.core.designsystem.rememberToastState
import persona.core.domain.PERSONA_REPO_URL
import java.io.File

/**
 * The GitHub star toast when first setup finishes (#5761): the checklist's Done owes it, and the shell's toast host shows
 * it after the checklist has gone, whether the checklist was drawn inside the host's tree (the pairing flow, as `Root.kt`'s
 * `PersonaRootContent`) or outside it (a relaunch during setup, `PlatformRoot`'s `OnboardingScreen`). Later owes nothing,
 * and a second setup shows nothing.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class StarPromptWelcomeTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val context: Context get() = rule.activity
    private val welcome get() = text(R.string.settings_star_welcome)
    private fun text(id: Int) = rule.activity.getString(id)

    private val sessions = object : SessionsRepository {
        override val machines = MutableStateFlow(listOf(Machine("mac", "MacBook Pro", online = true)))
        override val sessions = MutableStateFlow(emptyList<Session>())
        override suspend fun refresh() {}
    }

    @After fun clear() {
        listOf(StarPromptStore.NAME, "settings-readiness").forEach {
            context.getSharedPreferences(it, Context.MODE_PRIVATE).edit().clear().commit()
        }
    }

    /** The shell's root as `Root.kt` draws it: the checklist until it is done, then the shell, one toast host over both. */
    private fun root(store: StarPromptStore, checklistInside: Boolean) {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalStarPrompt provides store) {
                    var checklist by remember { mutableStateOf(true) }
                    if (checklist && !checklistInside) Checklist { checklist = false }
                    else Box(Modifier.fillMaxSize().testTag("root")) {
                        val toast = rememberToastState()
                        StarPromptWelcome(toast::show)
                        if (checklist) Checklist { checklist = false } else Text("Shell", Modifier.align(Alignment.Center))
                        JToastHost(toast, Modifier.align(Alignment.TopCenter))
                    }
                }
            }
        }
    }

    @Composable
    private fun Checklist(onDone: () -> Unit) {
        ReadinessScreen(sessions, notificationsOnly = false, onDiagnostics = {}, onDone = onDone, onBack = null, onLater = onDone)
    }

    /** Taps the checklist's [label] at its foot once the phone's checks are read (each has Open settings; it is disabled before). */
    private fun finish(label: String) {
        val open = hasText(text(R.string.settings_ready_open)) and hasClickAction()
        rule.waitUntil(5_000) { rule.onAllNodes(open).fetchSemanticsNodes().isNotEmpty() }
        rule.waitForIdle()
        val button = hasText(label) and hasClickAction() and isEnabled()
        rule.onNode(hasScrollToNodeAction()).performScrollToNode(button)
        rule.onNode(button).performClick()
        rule.waitForIdle()
    }

    @Test fun theToastOutlivesTheChecklistInsideTheShell() = survives(checklistInside = true)

    @Test fun theToastWaitsForTheShellAfterARelaunchDuringSetup() = survives(checklistInside = false)

    @Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
    @Test fun inChinese() = survives(checklistInside = true, language = "zh")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h900dp-xhdpi")
    @Test fun inJapanese() = survives(checklistInside = true, language = "ja")

    private fun survives(checklistInside: Boolean, language: String = "en") {
        val store = StarPromptStore(context)
        root(store, checklistInside)
        finish(text(R.string.settings_done))
        rule.onNodeWithText("Shell").assertIsDisplayed()
        rule.onNodeWithText(welcome).assertIsDisplayed()
        assertTrue(store.state.value.welcomed)
        assertFalse("shown, so no longer owed", store.welcome.value)
        val out = File(System.getProperty("persona.shots") ?: "build/star-prompt-shots").also { it.mkdirs() }
        val bitmap = rule.onNodeWithTag("root").captureToImage().asAndroidBitmap()
        File(out, "star-toast-$language-${if (checklistInside) "pairing" else "relaunch"}-glass-light.png").outputStream()
            .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        rule.onNodeWithText(text(R.string.settings_star_on_github)).performClick()
        rule.waitForIdle()
        assertTrue(store.state.value.done)
        assertEquals(PERSONA_REPO_URL, shadowOf(rule.activity).nextStartedActivity.dataString)
    }

    @Test fun laterOwesNoToast() {
        val store = StarPromptStore(context)
        root(store, checklistInside = true)
        finish(text(R.string.settings_ready_skip_rest))
        rule.onNodeWithText("Shell").assertIsDisplayed()
        rule.onNodeWithText(welcome).assertDoesNotExist()
        assertFalse(store.state.value.welcomed)
    }

    @Test fun aSecondSetupShowsNothing() {
        val store = StarPromptStore(context)
        assertTrue(store.onboarded())
        store.welcomeShown()
        root(store, checklistInside = true)
        finish(text(R.string.settings_done))
        rule.onNodeWithText("Shell").assertIsDisplayed()
        rule.onNodeWithText(welcome).assertDoesNotExist()
    }
}
