package persona.feature.chat

import android.content.ClipData
import android.content.ClipDescription
import android.content.Intent
import android.net.Uri
import android.view.DragEvent
import androidx.compose.ui.draganddrop.DragAndDropEvent
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.shadows.DragEventBuilder

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ComposerDropTargetTest {
    private val fileDescription = ClipDescription("files", arrayOf("application/octet-stream"))

    private fun event(action: Int, data: ClipData? = null, description: ClipDescription = fileDescription) =
        DragAndDropEvent(DragEventBuilder.newBuilder().setAction(action).setX(10f).setY(20f)
            .setClipDescription(description).setClipData(data).setResult(false).build())

    @Test fun mimeTypesAreOnlyCandidatesUntilClipDataProvesAFile() {
        assertTrue(composerDropAccepts(fileDescription))
        assertTrue(composerDropAccepts(ClipDescription("photos", arrayOf("image/*"))))
        assertTrue(composerDropAccepts(ClipDescription("mixed", arrayOf("text/plain", "image/png"))))
        // A .txt file and plain text can both say text/plain; the URI extraction below makes the final decision.
        assertTrue(composerDropAccepts(ClipDescription("text", arrayOf("text/plain"))))
        assertTrue(composerDropAccepts(ClipDescription("html", arrayOf("text/html"))))
        assertTrue(composerDropAccepts(ClipDescription("uris", arrayOf("text/uri-list"))))
        assertFalse(composerDropAccepts(null))
        assertTrue(composerDropUris(ClipData(ClipDescription("text", arrayOf("text/plain")), ClipData.Item("plain"))).isEmpty())
    }

    @Test fun enterExitAndEndedOnlyHighlightAnAcceptedTarget() {
        val hovers = mutableListOf<Boolean>()
        val target = ComposerDropTarget(accepts = { true }, hovering = hovers::add, drop = { false })
        target.onEntered(event(DragEvent.ACTION_DRAG_ENTERED))
        target.onExited(event(DragEvent.ACTION_DRAG_EXITED))
        target.onEntered(event(DragEvent.ACTION_DRAG_ENTERED))
        target.onEnded(event(DragEvent.ACTION_DRAG_ENDED))
        assertEquals(listOf(true, false, true, false), hovers)

        val unavailable = ComposerDropTarget(accepts = { false }, hovering = hovers::add, drop = { false })
        unavailable.onEntered(event(DragEvent.ACTION_DRAG_ENTERED))
        assertEquals(4, hovers.size)
    }

    @Test fun dropForwardsTheRealAndroidEventOnceAndClearsHighlight() {
        val data = ClipData("files", arrayOf("application/octet-stream"), ClipData.Item(Uri.parse("content://example/file")))
        val android = DragEventBuilder.newBuilder().setAction(DragEvent.ACTION_DROP).setX(10f).setY(20f)
            .setClipDescription(fileDescription).setClipData(data).setResult(false).build()
        val hovers = mutableListOf<Boolean>()
        var received: DragEvent? = null
        val target = ComposerDropTarget(accepts = { true }, hovering = hovers::add, drop = {
            received = it
            true
        })
        assertTrue(target.onDrop(DragAndDropEvent(android)))
        assertSame(android, received)
        assertEquals(listOf(false), hovers)
    }

    @Test fun unavailableTargetDoesNotReceiveDrop() {
        var called = false
        val target = ComposerDropTarget(accepts = { false }, hovering = {}, drop = { called = true; true })
        assertFalse(target.onDrop(event(DragEvent.ACTION_DROP)))
        assertFalse(called)
    }

    @Test fun uriExtractionKeepsOnlyDistinctContentAndFileItems() {
        val content = Uri.parse("content://example/document")
        val file = Uri.parse("file:///tmp/document.txt")
        val data = ClipData("files", arrayOf("application/octet-stream"), ClipData.Item(content)).apply {
            addItem(ClipData.Item(file))
            addItem(ClipData.Item(content))
            addItem(ClipData.Item("plain text"))
            addItem(ClipData.Item(Intent()))
            addItem(ClipData.Item(Uri.parse("https://example.com/not-a-file")))
        }
        assertEquals(listOf(content, file), composerDropUris(data))
        assertTrue(composerDropUris(null).isEmpty())
    }
}
