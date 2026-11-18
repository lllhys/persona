package persona.feature.chat

import android.content.ClipData
import android.content.ClipDescription
import android.content.Context
import android.content.pm.ActivityInfo
import android.content.res.Configuration
import android.net.Uri
import android.view.DragEvent
import android.view.InputDevice
import android.view.MotionEvent
import android.view.View
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performKeyInput
import androidx.compose.ui.test.pressKey
import androidx.compose.ui.test.withKeyDown
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.core.content.FileProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.io.IOException
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatComposerService
import persona.core.contracts.api.ComposerDelivery
import persona.core.contracts.api.DraftUploadState
import persona.core.designsystem.LocalLargeWindowInput
import persona.core.designsystem.PersonaTheme

@RunWith(AndroidJUnit4::class)
class ComposerDropInstrumentedTest {
    @get:Rule val rule = createAndroidComposeRule<ComposerInstrumentationActivity>()

    private val instrumentation = InstrumentationRegistry.getInstrumentation()
    private val created = mutableListOf<File>()

    @After fun removeSources() {
        created.forEach(File::delete)
    }

    private fun source(name: String, words: String): Uri {
        val file = File(File(rule.activity.cacheDir, "chat-captures").apply { mkdirs() }, name)
        file.writeText(words)
        created += file
        return FileProvider.getUriForFile(rule.activity, "${rule.activity.packageName}.chat.files", file)
    }

    private fun show(
        state: ChatComposerState,
        service: ChatComposerService?,
        large: () -> Boolean = { true },
        enabled: Boolean = true,
        onSend: () -> Unit = {},
    ) {
        rule.setContent {
            CompositionLocalProvider(LocalLargeWindowInput provides large()) {
                PersonaTheme(darkTheme = false, reduceMotion = true) {
                    LargeWindowComposer(SessionKey("box", "s1"), state, working = false, sending = false,
                        canClear = false, service = service, onSend = onSend, onCommand = {}, onNotice = {},
                        enabled = enabled)
                }
            }
        }
    }

    private fun drop(
        description: ClipDescription,
        data: ClipData,
        expectHighlight: Boolean = true,
        onHighlighted: () -> Unit = {},
    ): Boolean {
        val center = rule.onNodeWithTag("composer-drop-target").fetchSemanticsNode().boundsInRoot.center
        val location = IntArray(2)
        rule.runOnUiThread { rule.activity.window.decorView.getLocationOnScreen(location) }
        val x = location[0] + center.x
        val y = location[1] + center.y
        val downTime = android.os.SystemClock.uptimeMillis()
        val content = rule.activity.findViewById<View>(android.R.id.content)
        sendTouch(downTime, MotionEvent.ACTION_DOWN, x, y)
        instrumentation.waitForIdleSync()
        var started = false
        instrumentation.runOnMainSync {
            started = content.startDragAndDrop(data, View.DragShadowBuilder(content), null,
                View.DRAG_FLAG_GLOBAL or View.DRAG_FLAG_GLOBAL_URI_READ)
        }
        if (!started) return false
        sendTouch(downTime, MotionEvent.ACTION_MOVE, x + 20, y + 20)
        sendTouch(downTime, MotionEvent.ACTION_MOVE, x, y)
        rule.waitForIdle()
        if (expectHighlight) {
            rule.onNodeWithTag("composer-drop-target").assert(SemanticsMatcher.expectValue(
                SemanticsProperties.StateDescription, rule.activity.getString(R.string.chat_composer_drop_target)))
        }
        onHighlighted()
        sendTouch(downTime, MotionEvent.ACTION_UP, x, y)
        instrumentation.waitForIdleSync()
        return true
    }

    private fun sendTouch(downTime: Long, action: Int, x: Float, y: Float) {
        val event = MotionEvent.obtain(downTime, android.os.SystemClock.uptimeMillis(), action, x, y, 0).apply {
            source = InputDevice.SOURCE_TOUCHSCREEN
        }
        try {
            instrumentation.uiAutomation.injectInputEvent(event, true)
        } finally {
            event.recycle()
        }
    }

    @Test fun realDragEventsImportMultipleFilesAndSendingCleansOnlyTheirCopies() {
        val state = ChatComposerState()
        val service = RecordingComposerService()
        var sent = 0
        show(state, service, onSend = {
            sent++
            state.clearIfUnchanged(state.draft)
        })
        val description = ClipDescription("files", arrayOf("application/octet-stream"))
        val data = ClipData(description, ClipData.Item(source("one.txt", "one"))).apply {
            addItem(ClipData.Item(source("two.txt", "two")))
        }
        assertTrue(drop(description, data))
        rule.waitUntil(timeoutMillis = 10_000) {
            state.attachments.size == 2 && state.attachments.all { it.path != null } && service.snapshot().size == 2
        }
        val uploads = service.snapshot().associate { it.name to it.bytes.decodeToString() }
        assertEquals(setOf("one.txt", "two.txt"), uploads.keys)
        assertEquals(mapOf("one.txt" to "one", "two.txt" to "two"), uploads)
        val owned = state.attachments.map { it.uri }

        rule.onNode(hasSetTextAction()).performClick()
        rule.onNode(hasSetTextAction()).performKeyInput {
            withKeyDown(Key.CtrlLeft) { pressKey(Key.Enter) }
        }
        assertEquals(1, sent)
        assertTrue(state.attachments.isEmpty())
        rule.waitUntil(timeoutMillis = 5_000) { owned.all { !readable(rule.activity, it) } }
        assertEquals(2, service.snapshot().size)
    }

    /** Whole-screen captures of the real window while a real system drag hovers, uploads and finishes (drop-states-*.png). */
    @Test fun dropStatesAreCapturedOnTheRealWindow() {
        val state = ChatComposerState()
        val service = RecordingComposerService().apply { gate = CompletableDeferred() }
        show(state, service)
        val description = ClipDescription("files", arrayOf("application/octet-stream"))
        val data = ClipData(description,
            ClipData.Item(source("capture-one.txt", "one"))).apply { addItem(ClipData.Item(source("capture-two.txt", "two"))) }
        assertTrue(drop(description, data, onHighlighted = { Thread.sleep(400); capture("01-drag-hovering-the-composer") }))
        rule.waitUntil(timeoutMillis = 10_000) { service.snapshot().isNotEmpty() && state.attachments.size == 2 }
        Thread.sleep(400)
        capture("02-files-imported-and-uploading")
        service.gate!!.complete(Unit)
        rule.waitUntil(timeoutMillis = 10_000) { state.attachments.all { it.path != null } }
        rule.waitUntil(timeoutMillis = 10_000) {
            rule.onAllNodesWithText("Uploading", substring = true, ignoreCase = true).fetchSemanticsNodes().isEmpty()
        }
        Thread.sleep(400)
        capture("03-uploads-finished")
        assertEquals(2, service.snapshot().size)
    }

    private fun capture(name: String) {
        val folder = File(instrumentation.targetContext.getExternalFilesDir(null), "drop-shots").apply { mkdirs() }
        val bitmap = requireNotNull(instrumentation.uiAutomation.takeScreenshot()) { "no screenshot" }
        File(folder, "$name.png").outputStream().use { bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun failedUploadUsesTheExistingRetryRowWithoutAnotherImport() {
        val state = ChatComposerState()
        val service = RecordingComposerService().apply { failures.set(1) }
        show(state, service)
        val description = ClipDescription("files", arrayOf("application/octet-stream"))
        val data = ClipData(description, ClipData.Item(source("retry.txt", "retry")))
        assertTrue(drop(description, data))
        rule.waitUntil(timeoutMillis = 10_000) {
            state.attachments.singleOrNull()?.uploadState == DraftUploadState.Failed
        }
        val id = state.attachments.single().id
        rule.onNodeWithText(rule.activity.getString(R.string.chat_attach_retry)).performClick()
        rule.waitUntil(timeoutMillis = 10_000) {
            state.attachments.singleOrNull()?.path != null && service.snapshot().size == 2
        }
        assertEquals(id, state.attachments.single().id)
    }

    @Test fun rotationDuringUploadKeepsTheAttachmentAndOneOperation() {
        val state = ChatComposerState()
        val service = RecordingComposerService().apply { gate = CompletableDeferred() }
        show(state, service)
        val description = ClipDescription("files", arrayOf("application/octet-stream"))
        val data = ClipData(description, ClipData.Item(source("rotate.txt", "rotate")))
        assertTrue(drop(description, data))
        rule.waitUntil(timeoutMillis = 10_000) { service.snapshot().size == 1 }
        val id = state.attachments.single().id
        val activity = rule.activity
        val original = activity.resources.configuration.orientation
        val originalRequest = if (original == Configuration.ORIENTATION_PORTRAIT)
            ActivityInfo.SCREEN_ORIENTATION_PORTRAIT else ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE
        val other = if (originalRequest == ActivityInfo.SCREEN_ORIENTATION_PORTRAIT)
            ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE else ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
        instrumentation.runOnMainSync { activity.requestedOrientation = other }
        instrumentation.waitForIdleSync()
        assertSame(activity, rule.activity)
        assertEquals(1, service.snapshot().size)
        instrumentation.runOnMainSync { activity.requestedOrientation = originalRequest }
        instrumentation.waitForIdleSync()
        assertSame(activity, rule.activity)
        assertEquals(id, state.attachments.single().id)
        assertEquals(1, service.snapshot().size)

        service.gate!!.complete(Unit)
        rule.waitUntil(timeoutMillis = 10_000) { state.attachments.single().path != null }
        assertEquals(1, service.snapshot().size)
    }

    @Test fun largeWindowFlagChangesDoNotRestartUploadOrLoseDropCleanup() {
        val state = ChatComposerState().apply { value = TextFieldValue("draft", TextRange(3)) }
        val service = RecordingComposerService().apply { gate = CompletableDeferred() }
        var large by mutableStateOf(true)
        show(state, service, large = { large })
        val description = ClipDescription("files", arrayOf("application/octet-stream"))
        val data = ClipData(description, ClipData.Item(source("resize.txt", "resize")))
        assertTrue(drop(description, data))
        rule.waitUntil(timeoutMillis = 10_000) { service.snapshot().size == 1 }
        val id = state.attachments.single().id
        val owned = state.attachments.single().uri
        val selection = state.value.selection
        repeat(2) {
            large = false
            rule.waitForIdle()
            rule.onNodeWithTag("composer-drop-target").assertDoesNotExist()
            large = true
            rule.waitForIdle()
            assertEquals(1, service.snapshot().size)
            assertEquals(id, state.attachments.single().id)
            assertEquals(selection, state.value.selection)
        }
        service.gate!!.complete(Unit)
        rule.waitUntil(timeoutMillis = 10_000) { state.attachments.single().path != null }
        assertEquals(1, service.snapshot().size)

        val otherUri = source("other.txt", "other").toString()
        state.attachments = state.attachments + ComposerAttachment("other", "other.txt", otherUri, false,
            path = "/remote/other.txt", uploadState = DraftUploadState.Uploaded)
        rule.waitForIdle()
        large = false
        rule.waitForIdle()
        state.attachments = state.attachments.filter { it.uri != owned }
        rule.waitUntil(timeoutMillis = 5_000) { !readable(rule.activity, owned) }
        assertTrue(readable(rule.activity, otherUri))
        large = true
        rule.waitForIdle()
        assertFalse(readable(rule.activity, owned))
        assertTrue(readable(rule.activity, otherUri))
        assertEquals(1, service.snapshot().size)
    }

    @Test fun readOnlyAndPlainTextDropsNeverReachTheUploader() {
        val state = ChatComposerState()
        val service = RecordingComposerService()
        show(state, service, enabled = false)
        val files = ClipDescription("files", arrayOf("application/octet-stream"))
        val fileData = ClipData(files, ClipData.Item(source("readonly.txt", "no")))
        assertTrue(drop(files, fileData, expectHighlight = false))
        assertTrue(service.snapshot().isEmpty())
        assertTrue(state.attachments.isEmpty())
    }

    @Test fun plainTextIsNotConsumedAsAFile() {
        val state = ChatComposerState()
        val service = RecordingComposerService()
        show(state, service)
        val text = ClipDescription("text", arrayOf("text/plain"))
        val data = ClipData(text, ClipData.Item("plain text"))
        assertTrue(drop(text, data, expectHighlight = false))
        assertTrue(service.snapshot().isEmpty())
        assertTrue(state.attachments.isEmpty())
    }

    private fun readable(context: Context, uri: String): Boolean = runCatching {
        context.contentResolver.openInputStream(Uri.parse(uri))?.use { true } == true
    }.getOrDefault(false)
}

private data class RecordedUpload(val name: String, val bytes: ByteArray)

private class RecordingComposerService : ChatComposerService {
    override val deliveries = MutableStateFlow<Map<String, ComposerDelivery>>(emptyMap())
    val failures = AtomicInteger(0)
    var gate: CompletableDeferred<Unit>? = null
    private val lock = Any()
    private val uploads = mutableListOf<RecordedUpload>()

    fun snapshot(): List<RecordedUpload> = synchronized(lock) { uploads.toList() }

    override suspend fun upload(session: SessionKey, name: String, file: File): String {
        val bytes = file.readBytes()
        synchronized(lock) { uploads += RecordedUpload(name, bytes) }
        gate?.await()
        if (failures.getAndUpdate { (it - 1).coerceAtLeast(0) } > 0) throw IOException("synthetic upload failure")
        return "/remote/$name"
    }

    override suspend fun interrupt(session: SessionKey) = Unit

    override suspend fun retryRefused(clientMessageId: String, clearDraft: Boolean): ComposerDelivery =
        error("not used by composer drop tests")
}
