package persona.feature.chat

import android.content.Context
import android.net.Uri
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.ImagePayload
import java.io.File
import java.util.Base64

/**
 * The photos a Persona message carried come back with it when it is taken back from the queue (#4339 C6, iOS
 * `restoreQueued`): the hub returns them beside the queue as base64 and each becomes a photo of the draft.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class QueuedPhotosTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val bytes = byteArrayOf(1, 2, 3, 4, 5)
    private val payload = ImagePayload("image/png", Base64.getEncoder().encodeToString(bytes))
    private fun file(attachmentUri: String) = File(Uri.parse(attachmentUri).path!!)

    @Test fun eachPhotoIsWrittenWhereACaptureIsAndComesBackAsAPhotoOfTheDraft() {
        val restored = restoredPhotos(context, listOf(payload, payload.copy(mime = "image/jpeg")), share = { Uri.fromFile(it) })
        assertEquals(0, restored.lost)
        assertEquals(2, restored.attachments.size)
        for (photo in restored.attachments) {
            // a photo and nothing else, not uploaded yet: the composer sends it to the computer as it does any photo
            assertTrue(photo.image)
            assertFalse(photo.video)
            assertNull(photo.path)
            assertEquals(bytes.size.toLong(), photo.size)
            val written = file(photo.uri)
            assertEquals(File(context.cacheDir, "chat-captures").canonicalPath, written.parentFile!!.canonicalPath)
            assertTrue(written.name.startsWith("queued-"))
            assertEquals(bytes.toList(), written.readBytes().toList())
        }
        assertEquals(2, restored.attachments.map { it.id }.toSet().size)
    }

    @Test fun aPhotoThatIsNotBase64OrCannotBeSharedIsLeftOutAndCounted() {
        val restored = restoredPhotos(context, listOf(payload, ImagePayload("image/png", ""), ImagePayload("image/png", "%%%%")),
            share = { Uri.fromFile(it) })
        assertEquals(1, restored.attachments.size)
        assertEquals(2, restored.lost)
        // One the provider will not hand out leaves no file behind.
        val before = File(context.cacheDir, "chat-captures").listFiles().orEmpty().map { it.name }.toSet()
        val refused = restoredPhotos(context, listOf(payload), share = { throw IllegalArgumentException("no provider") })
        assertEquals(RestoredPhotos(emptyList(), 1), refused)
        assertEquals(before, File(context.cacheDir, "chat-captures").listFiles().orEmpty().map { it.name }.toSet())
    }

    @Test fun photosJoinTheDraftBesideWhatItHolds() {
        val held = ComposerAttachment("held", "held.jpg", "", true, "/tmp/held.jpg")
        val state = ChatComposerState(ChatComposerDraft("draft", listOf(held)))
        val restored = restoredPhotos(context, listOf(payload, payload), share = { Uri.fromFile(it) })
        state.restorePhotos(restored.attachments)
        // All of them stay, after the draft's own; nothing is dropped for the picker's four places.
        assertEquals(listOf(held) + restored.attachments, state.attachments)
        state.restorePhotos(emptyList())
        assertEquals(3, state.attachments.size)
    }
}
