package persona.feature.files

import android.content.Context
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onLast
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.AppFeedback
import persona.core.designsystem.AppStyle
import persona.core.designsystem.FeedbackAction
import persona.core.designsystem.FeedbackStyle
import persona.core.designsystem.LocalAppFeedback
import persona.core.designsystem.PersonaTheme
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import persona.core.navigation.ChatRoute
import java.io.File

/** A folder in the home whose hub renames and trashes (`file_edit` unless [edits] is off); records each change. */
internal class EditMachine : SessionFiles {
    val root = "/Users/me/code/weather"
    val asked = mutableListOf<String>()
    var edits = true
    var failing: Exception? = null
    override suspend fun directory(machineId: String, cwd: String, path: String) = SessionDirectory(root, listOf(
        SessionFileEntry("Sources", dir = true), SessionFileEntry("README.md", size = 1536), SessionFileEntry("Package.swift", size = 1023)))
    override suspend fun file(machineId: String, cwd: String, path: String) = SessionFileContent(path, FilesPaths.name(path), text = "")
    override suspend fun status(machineId: String, cwd: String) = SessionGitStatus(false)
    override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) = SessionGitDiff(path, "")
    override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int) = SessionGitLog()
    override suspend fun show(machineId: String, cwd: String, hash: String) = error("unused")
    override fun editsFiles(machineId: String) = edits
    override suspend fun rename(machineId: String, cwd: String, path: String, name: String): String {
        asked += "rename $path $name"
        failing?.let { throw it }
        return joinRemotePath(root, name)
    }
    override suspend fun trash(machineId: String, cwd: String, paths: List<String>): List<SessionTrashedFile> {
        asked += "trash ${paths.joinToString()}"
        failing?.let { throw it }
        return paths.map { SessionTrashedFile(it, "t1") }
    }
    override suspend fun restore(machineId: String, trashIds: List<String>): List<String> {
        asked += "restore ${trashIds.joinToString()}"
        return listOf("$root/README.md")
    }
}

/** The toasts the page asks the window for. */
internal class Toasts : AppFeedback {
    val shown = mutableListOf<Triple<String, FeedbackStyle?, FeedbackAction?>>()
    override fun failure(error: Throwable, operation: Boolean) = true
    override fun notice(message: String) = true.also { shown += Triple(message, FeedbackStyle.Info, null) }
    override fun notice(message: String, style: FeedbackStyle) = true.also { shown += Triple(message, style, null) }
    override fun notice(message: String, action: FeedbackAction) = true.also { shown += Triple(message, null, action) }
}

/**
 * Rename and Move to Trash in the file browser (iOS FilesPane / FolderEdits): what the row's menu offers, what is sent, the
 * toast with Undo, a refusal in the user's words, and no entries where the hub can't.
 */
// The windows are 320dp wide: on this JVM a JDialog holding a JTextField never goes idle at 360dp and wider (the
// machine and conversation rename dialogs too), so the rename dialog is driven, and drawn, at that width.
@RunWith(AndroidJUnit4::class)
class FileEditsTest {
    @get:Rule val rule = createComposeRule()
    private val machine = EditMachine()
    private val toasts = Toasts()

    private fun show() {
        rule.setContent {
            CompositionLocalProvider(LocalAppFeedback provides toasts) {
                PersonaTheme(darkTheme = false, reduceMotion = true) {
                    SessionFilesScreen(ChatRoute("mac", "s1"), machine.root, machine, onBackToChat = {}, onInsertPath = {})
                }
            }
        }
        rule.waitForIdle()
    }

    private fun menu(entry: String) {
        rule.onNodeWithContentDescription("Actions for $entry").performClick()
        rule.waitForIdle()
    }

    /** The clickable one of the nodes saying [text] (a dialog's title and its button may say the same). */
    private fun tap(text: String) {
        rule.onAllNodes(hasText(text) and hasClickAction()).onLast().performClick()
        rule.waitForIdle()
    }

    private fun rename(entry: String, to: String) {
        menu(entry)
        tap("Rename")
        rule.onNode(hasSetTextAction() and hasAnyAncestor(hasTestTag("files-rename-field"))).performTextReplacement(to)
        tap("Rename")
    }

    @Config(sdk = [35], qualifiers = "en-w320dp-h800dp")
    @Test fun renameAndTrashWithUndo() {
        show()
        menu("README.md")
        for (label in listOf("Insert path", "Copy path", "Rename", "Move to Trash")) assertTrue(label, rule.onAllNodes(hasText(label)).fetchSemanticsNodes().isNotEmpty())
        tap("Rename")
        rule.onNode(hasSetTextAction() and hasAnyAncestor(hasTestTag("files-rename-field"))).assertTextEquals("README.md")
        rule.onNodeWithText("Name").assertExists()
        rule.onNode(hasSetTextAction() and hasAnyAncestor(hasTestTag("files-rename-field"))).performTextReplacement("NOTES.md")
        tap("Rename")
        assertEquals(listOf("rename ${machine.root}/README.md NOTES.md"), machine.asked)

        menu("Sources")
        tap("Move to Trash")
        rule.onNodeWithText("Move “Sources” to the Trash?").assertExists()
        tap("Move to Trash")
        assertEquals("trash ${machine.root}/Sources", machine.asked.last())
        val (message, _, undo) = toasts.shown.last()
        assertEquals("Moved “Sources” to the Trash", message)
        assertEquals("Undo", undo!!.label)
        rule.runOnIdle { undo.run() }
        rule.waitForIdle()
        assertEquals("restore t1", machine.asked.last())
    }

    @Config(sdk = [35], qualifiers = "en-w320dp-h800dp")
    @Test fun outsideTheHomeTheMachinesRefusalIsSaid() {
        machine.failing = HubFailure(HubError.Server, "only the home can be written", 409, "read_only")
        show()
        rename("README.md", "b.md")
        val (message, toastStyle, undo) = toasts.shown.last()
        assertEquals("This can’t be changed from your phone. Only files in the home folder can (except ~/.persona and ~/Library).", message)
        assertEquals(FeedbackStyle.Error, toastStyle)
        assertNull(undo)
        // A name the machine can't take is said before anything is sent.
        rename("README.md", "a/b")
        assertEquals("A name can’t contain “/”", toasts.shown.last().first)
        assertEquals(1, machine.asked.size)
    }

    @Config(sdk = [35], qualifiers = "en-w320dp-h800dp")
    @Test fun refusalsSayTheHubsNameAndAServerErrorsRequestIdAsIOSDoes() {
        val resources = ApplicationProvider.getApplicationContext<Context>().resources
        val unavailable = "The service couldn’t complete this request. Try again later. (HTTP 502)"
        assertEquals("$unavailable (request r-1)", editRefusalText(resources,
            editRefusal(HubFailure(HubError.Server, "~/a is on another disk", 502, "internal", requestId = "r-1"), "a")))
        assertEquals(unavailable, editRefusalText(resources, editRefusal(HubFailure(HubError.Server, "~/a is on another disk", 502, "internal"), "a")))
        // Below 500 there is no request id to add, as on iOS.
        assertEquals("“b 2.txt” already exists there", editRefusalText(resources, editRefusal(
            HubFailure(HubError.Server, "taken", 409, "exists", requestId = "r-2", body = """{"error":"taken","code":"exists","name":"b 2.txt"}"""), "b.txt")))
        assertEquals("“b.txt” already exists there", editRefusalText(resources, editRefusal(
            HubFailure(HubError.Server, "taken", 409, "exists", body = """{"error":"taken","code":"exists"}"""), "b.txt")))
    }

    @Config(sdk = [35], qualifiers = "en-w320dp-h800dp")
    @Test fun aHubWithoutFileEditOffersNeither() {
        machine.edits = false
        show()
        menu("README.md")
        rule.onNodeWithText("Copy path").assertExists()
        rule.onNodeWithText("Rename").assertDoesNotExist()
        rule.onNodeWithText("Move to Trash").assertDoesNotExist()
    }
}

/**
 * Rename's and Move to Trash's dialogs in each style, light and dark, English and Chinese, as PNGs in
 * `build/file-edits-shots/` (or `-Dpersona.shots=<dir>`) to look at. The row's menu is the existing action sheet with two
 * more actions; its window can't be captured on the JVM, so [FileEditsTest] checks what it lists.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class FileEditsShotsTest {
    @get:Rule val rule = createComposeRule()
    private var style by mutableStateOf(AppStyle.Glass)
    private var dark by mutableStateOf(false)
    private var page by mutableStateOf("rename")

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/file-edits-shots").also { it.mkdirs() }
    private fun noop() = Unit
    private fun noop(name: String) = Unit

    private fun shots(language: String, texts: Map<String, String>) {
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background))
                if (page == "rename") RenameFileDialog("README.md", onRename = ::noop, onDismiss = ::noop)
                else TrashFileDialog("README.md", onTrash = ::noop, onDismiss = ::noop)
            }
        }
        for (name in listOf("rename", "trash")) {
            page = name
            for (s in AppStyle.entries) for (d in listOf(false, true)) {
                style = s; dark = d
                Snapshot.sendApplyNotifications(); rule.waitForIdle()
                rule.onNodeWithText(texts.getValue(name)).assertExists()
                val bitmap = rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
                File(out(), "file-edits-$name-$language-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                    .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            }
        }
    }

    @Config(sdk = [35], qualifiers = "en-w320dp-h800dp-xhdpi")
    @Test fun english() = shots("en", mapOf("rename" to "Name", "trash" to "Move “README.md” to the Trash?"))

    @Config(sdk = [35], qualifiers = "zh-w320dp-h800dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", mapOf("rename" to "名称", "trash" to "把「README.md」移到废纸篓？"))
}
