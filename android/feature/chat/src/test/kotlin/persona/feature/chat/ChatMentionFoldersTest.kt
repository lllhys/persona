package persona.feature.chat

import androidx.compose.runtime.remember
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.SessionFileEntry
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme

/**
 * An Agent's chat browsing its session's folder from @ (#4339 C16, iOS `ComposerMentionMenu`), and the + menu's Mention a
 * file or session (iOS `AttachMoreSection.common`): the real composer against a fake computer.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class ChatMentionFoldersTest {
    @get:Rule val rule = createComposeRule()

    private val key = SessionKey("box", "s1")
    private val cwd = "/w/app"
    private val other = Session(machineId = "box", sessionId = "s2", title = "Photo sync", cwd = "/w/photos", agent = "codex", updatedAt = "2026-11-12T09:00:00Z")

    /** The session's computer: what each folder holds, and which folders were asked for. */
    private class Computer(val tree: Map<String, List<SessionFileEntry>>) {
        val asked = mutableListOf<String>()
        val folders = MentionFolders { dir, _ -> asked += dir; tree[dir] ?: error("not a folder") }
    }

    private val tree = mapOf(
        cwd to listOf(SessionFileEntry("src", dir = true), SessionFileEntry("docs", dir = true), SessionFileEntry("README.md"), SessionFileEntry(".gitignore")),
        "$cwd/src" to listOf(SessionFileEntry("main.kt"), SessionFileEntry("login", dir = true)),
        "$cwd/src/login" to listOf(SessionFileEntry("LoginView.kt")),
    )

    private fun mount(state: ChatComposerState, folders: MentionFolders?, used: List<String> = listOf("$cwd/src/Login.kt"), sessions: List<Session> = listOf(other),
                      withMentions: Boolean = true) {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                ChatComposer(key, remember { state }, working = false, sending = false, canClear = false, service = null,
                    onSend = {}, onCommand = {}, onNotice = {},
                    mentions = if (withMentions) ComposerMentions(used, cwd, sessions, key, folders = folders) else null)
            }
        }
    }

    private fun typed(text: String, caret: Int = text.length) = ChatComposerState().apply { value = TextFieldValue(text, TextRange(caret)) }
    private fun shown(text: String) { rule.waitUntil(5_000) { rule.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty() } }

    @Test fun atListsTheSessionFoldersEntriesAfterTheFilesAndAFolderPickIsBrowsed() {
        val computer = Computer(tree)
        val state = typed("Look at @")
        mount(state, computer.folders)
        shown("docs/")
        // The files the chat used first, then the folder's own entries (folders first, hidden ones left out), then the sessions.
        val top = listOf("Login.kt", "src/", "docs/", "README.md", "Photo sync").map { rule.onNodeWithText(it).fetchSemanticsNode().boundsInRoot.top }
        assertEquals(top.sorted(), top)
        rule.onAllNodesWithText(".gitignore").assertCountEquals(0)
        rule.onAllNodesWithText("Folder").assertCountEquals(2)
        rule.onAllNodesWithText("Session folder").assertCountEquals(1)
        assertEquals(listOf(cwd), computer.asked)
        // A folder stays typed with its slash and the list goes into it, relative to the session's folder.
        rule.onNodeWithText("src/").performClick()
        rule.waitForIdle()
        assertEquals("Look at @src/", state.value.text)
        shown("main.kt")
        assertEquals(listOf(cwd, "$cwd/src"), computer.asked)
        rule.onNodeWithText("login/").assertIsDisplayed()
        rule.onAllNodesWithText("Photo sync").assertCountEquals(0)
        rule.onAllNodesWithText("docs/").assertCountEquals(0)
        // Into the next one, then a file.
        rule.onNodeWithText("login/").performClick()
        shown("LoginView.kt")
        assertEquals("Look at @src/login/", state.value.text)
        rule.onNodeWithText("LoginView.kt").performClick()
        rule.waitForIdle()
        assertEquals("Look at @src/login/LoginView.kt ", state.value.text)
        assertEquals(listOf(cwd, "$cwd/src", "$cwd/src/login"), computer.asked)
    }

    @Test fun aTypedWordNarrowsTheEntriesAndAFilePickTypesItsName() {
        val computer = Computer(tree)
        val state = typed("@RE")
        mount(state, computer.folders, used = emptyList(), sessions = emptyList())
        shown("README.md")
        rule.onAllNodesWithText("src/").assertCountEquals(0)
        rule.onNodeWithText("README.md").performClick()
        rule.waitForIdle()
        assertEquals("@README.md ", state.value.text)
    }

    @Test fun theListSaysItIsReadingWhileTheFolderIsAndNothingElseMatches() {
        val gate = CompletableDeferred<List<SessionFileEntry>>()
        val state = typed("@")
        mount(state, MentionFolders { _, _ -> gate.await() }, used = emptyList(), sessions = emptyList())
        shown("Reading folder…")
        gate.complete(tree.getValue(cwd))
        shown("docs/")
        rule.onAllNodesWithText("Reading folder…").assertCountEquals(0)
    }

    @Test fun aFolderThatCannotBeReadListsNothingAndSaysSo() {
        val state = typed("@")
        mount(state, MentionFolders { _, _ -> error("offline") }, used = emptyList(), sessions = emptyList())
        shown("No file or session matches")
        rule.runOnUiThread { state.value = TextFieldValue("@nowhere/", TextRange(9)) }
        shown("Nothing matches in this folder")
    }

    @Test fun withoutAReaderTheListIsTheChatsFilesAndSessionsAsBefore() {
        val state = typed("@")
        mount(state, folders = null)
        shown("Login.kt")
        rule.onNodeWithText("Photo sync").assertIsDisplayed()
        rule.onAllNodesWithText("Session folder").assertCountEquals(0)
    }

    @Test fun thePlusMenuTypesAnAtAtTheCaretAndTheListOpens() {
        val computer = Computer(tree)
        val state = typed("Look at this", caret = 5)
        mount(state, computer.folders)
        rule.onAllNodesWithText("docs/").assertCountEquals(0)
        rule.onNodeWithContentDescription("Add attachments and quick messages").performClick()
        rule.onNodeWithText("Mention a file or session").assertIsDisplayed()
        rule.onNodeWithText("Mention a file or session").performClick()
        rule.waitForIdle()
        // The @ goes where the caret is, as the @ button's does, and the list is that word's.
        assertEquals("Look @at this", state.value.text)
        assertEquals(TextRange(6), state.value.selection)
        shown("docs/")
        rule.onNodeWithText("Photo sync").assertIsDisplayed()
        rule.onAllNodesWithText("Mention a file or session").assertCountEquals(0)
    }

    @Test fun aComposerWithoutAnAtListHasNoMentionInItsPlusMenu() {
        val state = typed("")
        mount(state, folders = null, withMentions = false)
        rule.onNodeWithContentDescription("Add attachments and quick messages").performClick()
        rule.onNodeWithText("Photos").assertIsDisplayed()
        rule.onAllNodesWithText("Mention a file or session").assertCountEquals(0)
    }
}
