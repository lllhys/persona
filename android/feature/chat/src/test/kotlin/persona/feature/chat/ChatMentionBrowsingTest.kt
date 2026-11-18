package persona.feature.chat

import android.content.Context
import android.provider.Settings
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.SessionKey
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme

/**
 * An Agent's chat and Persona's conversation, on the real page, listing their session's folder from @ and opening a
 * folder (#4339 C16, iOS `ComposerMentionMenu`), and the + menu's Mention a file or session.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ChatMentionBrowsingTest {
    @get:Rule val rule = createComposeRule()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val computer = MentionChatHarness(scope)
    @After fun close() { scope.cancel() }

    private fun mount(key: SessionKey, composer: ChatComposerState, style: AppStyle = AppStyle.Glass) {
        Settings.Global.putFloat(ApplicationProvider.getApplicationContext<Context>().contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 0f)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                Box(Modifier.fillMaxSize()) { computer.Page(key, composer) }
            }
        }
        val ready = if (key == computer.agent) "Found it" else "The web update"
        rule.waitUntil(20_000) { rule.onAllNodesWithText(ready, substring = true).fetchSemanticsNodes().isNotEmpty() }
    }

    private fun typed(composer: ChatComposerState, text: String) {
        rule.runOnUiThread { composer.value = TextFieldValue(text, TextRange(text.length)) }
        rule.waitForIdle()
    }
    private fun shown(text: String) { rule.waitUntil(10_000) { rule.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty() } }
    private fun top(text: String) = rule.onNodeWithText(text).fetchSemanticsNode().boundsInRoot.top

    @Test fun anAgentsChatListsItsSessionFoldersEntriesAfterItsFilesAndOpensAFolder() {
        val composer = ChatComposerState()
        mount(computer.agent, composer)
        typed(composer, "Look at @")
        shown("docs/")
        // The files its tools used (relative to its folder inside it), the folder's entries (folders first, hidden ones
        // left out), then the other sessions.
        val order = listOf("forecast.log", "Forecast.kt", "app/", "docs/", "scripts/", "README.md", "build.gradle.kts")
        val tops = order.map(::top)
        assertEquals("$order at $tops", tops.sorted(), tops)
        // (The sessions follow under the list's fold; it scrolls past six rows and a half.)
        rule.onNodeWithText("Photo sync").assertExists()
        rule.onNodeWithText("Release notes for 2.4").assertExists()
        rule.onAllNodesWithText(".github/").assertCountEquals(0)
        rule.onAllNodesWithText(".gitignore").assertCountEquals(0)
        rule.onAllNodesWithText("Folder").assertCountEquals(3)
        rule.onAllNodesWithText("Session folder").assertCountEquals(2)
        assertEquals(listOf(computer.cwd), computer.asked)
        // A folder stays typed, relative to the session's folder, and the list goes into it.
        rule.onNodeWithText("app/").performClick()
        shown("proguard-rules.pro")
        assertEquals("Look at @app/", composer.value.text)
        assertEquals(listOf(computer.cwd, "${computer.cwd}/app"), computer.asked)
        rule.onNodeWithText("src/").assertIsDisplayed()
        rule.onAllNodesWithText("Photo sync").assertCountEquals(0)
        rule.onAllNodesWithText("docs/").assertCountEquals(0)
        rule.onNodeWithText("proguard-rules.pro").performClick()
        rule.waitForIdle()
        assertEquals("Look at @app/proguard-rules.pro ", composer.value.text)
    }

    @Test fun personasConversationListsItsTasksFirstThenItsFoldersEntries() {
        val composer = ChatComposerState()
        mount(computer.persona, composer)
        typed(composer, "@")
        shown("Notes/")
        rule.onNodeWithText("Message a task").assertIsDisplayed()
        rule.onNodeWithText("Mention a file or session").assertIsDisplayed()
        // The tasks under their heading, then the folder's entries and the sessions under the other.
        val order = listOf("Message a task", "Sync HEIC photos", "Mention a file or session", "Notes/", "Reports/", "todo.md")
        val tops = order.map(::top)
        assertEquals("$order at $tops", tops.sorted(), tops)
        rule.onNodeWithText("Photo sync").assertExists()
        assertEquals(listOf(computer.personaCwd), computer.asked)
    }

    private fun plusMenuTypesAnAt(key: SessionKey) {
        val composer = ChatComposerState()
        mount(key, composer)
        rule.onNodeWithContentDescription("Add attachments and quick messages").performClick()
        rule.onNodeWithText("Mention a file or session").assertIsDisplayed()
        rule.onNodeWithText("Mention a file or session").performClick()
        rule.waitForIdle()
        assertEquals("@", composer.value.text)
        // The list is open for that @: the other sessions, and the session's folder read.
        shown("Photo sync")
        shown(if (key == computer.agent) "docs/" else "Notes/")
        assertEquals(listOf(if (key == computer.agent) computer.cwd else computer.personaCwd), computer.asked)
    }

    @Test fun thePlusMenuTypesAnAtInAnAgentsChat() = plusMenuTypesAnAt(computer.agent)
    @Test fun thePlusMenuTypesAnAtInPersonasConversation() = plusMenuTypesAnAt(computer.persona)
}
