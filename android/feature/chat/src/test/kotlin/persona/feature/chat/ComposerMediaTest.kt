package persona.feature.chat

import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.net.Uri
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.encodeToString
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.HubScope
import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import persona.core.domain.AttachedFile
import persona.core.domain.parseAttachedFiles

/**
 * Videos and pasted pictures in the composer (iOS `VideoAttachments`, `PasteImageChip`): a video goes as a
 * `<persona-file kind="video">` tag after the words, shares the four places with photos, survives a stored draft and
 * comes back on Edit; a picture on the clipboard is offered once per copy.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ComposerMediaTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()

    private fun video(id: String = "v", path: String? = "/tmp/clip-20261112.mov", duration: Double? = 12.4) = ComposerAttachment(id, "clip.mov",
        "content://media/picker/0/video/1", false, path, size = 25_270_000, uploadState = if (path != null) DraftUploadState.Uploaded else DraftUploadState.Pending,
        video = true, duration = duration)

    private fun photo(id: String = "p", path: String = "/tmp/photo.jpg") =
        ComposerAttachment(id, "photo.jpg", "content://media/picker/0/image/2", true, path, uploadState = DraftUploadState.Uploaded)

    private fun state(text: String, vararg attachments: ComposerAttachment) = ChatComposerState(ChatComposerDraft(text, attachments.toList())).apply {
        photosOnlyText = "Please look at the photos"
        videosOnlyText = "Please look at the video"
        filesOnlyText = "Please look at the attached files"
    }

    @Test fun aVideoGoesAsATagAfterTheWordsNotAsAPath() {
        val message = state("Where does it stall?", video()).message()
        assertEquals("Where does it stall?\n\n<persona-file kind=\"video\" path=\"/tmp/clip-20261112.mov\" name=\"clip.mov\" size=\"25270000\" duration=\"12.4\"/>", message)
        val (files, rest) = parseAttachedFiles(message)
        assertEquals(listOf(AttachedFile("video", "/tmp/clip-20261112.mov", "clip.mov", 25_270_000, 12.4)), files)
        assertEquals("Where does it stall?", rest)
    }

    @Test fun aVideoAloneSaysPleaseLookAtTheVideoAndPhotosWin() {
        assertTrue(state("", video()).message().startsWith("Please look at the video\n\n<persona-file"))
        val both = state("", video(), photo()).message()
        assertTrue(both.startsWith("Please look at the photos\n\n<persona-file"))
        assertTrue(both.endsWith("<attached_images>\n/tmp/photo.jpg\n</attached_images>"))
        // A video still on its way holds the message back, as a photo does.
        assertFalse(state("hi", video(path = null)).ready)
    }

    @Test fun photosAndVideosShareFourPlaces() {
        assertTrue(video().media)
        assertTrue(photo().media)
        assertFalse(ComposerAttachment("f", "notes.txt", "content://x/notes", false, "/tmp/notes.txt").media)
        assertEquals(4, MAX_COMPOSER_MEDIA)
    }

    @Test fun aStoredDraftKeepsItsVideoAndLength() {
        val draft = ChatComposerDraft("look", listOf(video(), photo()))
        val stored = JsonCodec.encodeToString(draft.storedAttachments())
        assertTrue(stored.contains("\"Video\""))
        val snapshot = DraftSnapshot(HubScope("hub", "pairing"), DraftKey.Chat(SessionKey("machine", "session")), draft.text, 1,
            JsonCodec.decodeFromString(stored))
        assertEquals(draft, ChatComposerState(snapshot.composerDraft()).draft)
    }

    @Test fun editPutsTheVideoBackAsAVideoOnTheMachine() {
        val sent = state("Where does it stall?", video(), photo()).message()
        val edited = ChatComposerState().apply { editMessage(sent) }
        assertEquals("Where does it stall?", edited.value.text)
        val back = edited.attachments.single { it.video }
        assertEquals("/tmp/clip-20261112.mov", back.path)
        assertEquals("clip.mov", back.name)
        assertEquals(25_270_000L, back.size)
        assertEquals(12.4, back.duration!!, 0.0)
        assertEquals("", back.uri)
        assertTrue(edited.ready)
        assertEquals(listOf("/tmp/photo.jpg"), edited.attachments.filter { it.image }.map { it.path })
        // Another kind of file keeps its tag in the words.
        val audio = "Listen\n\n<persona-file kind=\"audio\" path=\"/tmp/a.m4a\" name=\"a.m4a\" size=\"9\"/>"
        assertEquals(audio, ChatComposerState().apply { editMessage(audio) }.value.text)
    }

    @Test fun ordinaryFilesComeAfterVideosAndReturnAsFileAttachmentsOnEdit() {
        val document = ComposerAttachment("f", "notes.txt", "content://docs/notes", false, "/tmp/notes.txt", size = 86,
            uploadState = DraftUploadState.Uploaded)
        val sent = state("", video(), document, photo()).message()
        assertEquals("Please look at the photos\n\n${video().attachedFile()!!.toXml()}\n${document.attachedFile()!!.toXml()}\n\n" +
            "<attached_images>\n/tmp/photo.jpg\n</attached_images>", sent)
        val edited = ChatComposerState().apply {
            photosOnlyText = "Please look at the photos"; videosOnlyText = "Please look at the video"
            filesOnlyText = "Please look at the attached files"; editMessage(sent)
        }
        assertEquals("Please look at the photos", edited.value.text)
        assertEquals(listOf("video", "file"), edited.attachments.filterNot { it.image }.map { it.attachedFile()!!.kind })
        assertEquals(sent, edited.message())

        val filesOnly = state("", document).message()
        assertEquals("Please look at the attached files\n\n${document.attachedFile()!!.toXml()}", filesOnly)
        assertEquals("notes.txt", composerMessagePreview(filesOnly, "[Photo]", "[Video]"))
    }

    @Test fun editKeepsEveryVideoFromAnOlderOrHandWrittenMessage() {
        val videos = List(5) { index -> video("v$index", "/tmp/clip-$index.mov") }
        val sent = state("Keep every video", *videos.toTypedArray()).message()
        val edited = ChatComposerState().apply {
            videosOnlyText = "Please look at the video"; editMessage(sent)
        }
        assertEquals(5, edited.attachments.count { it.video })
        assertEquals(sent, edited.message())
    }

    @Test fun aQueuedVideoReadsAsItsMark() {
        val alone = state("", video()).message()
        assertEquals("[Video]", composerMessagePreview(alone, "[Photo]", "[Video]"))
        assertEquals("Where does it stall?\n[Video]", composerMessagePreview(state("Where does it stall?", video()).message(), "[Photo]", "[Video]"))
        // Taken back to edit, the stand-in words go and the video comes back.
        val restored = state("").apply { restoreQueued(alone) }
        assertEquals("", restored.value.text)
        assertTrue(restored.attachments.single().video)
    }

    @Test fun aClipboardPictureIsOfferedOncePerCopy() {
        val clipboard = context.getSystemService(ClipboardManager::class.java)
        val images = ClipboardImages(clipboard)
        clipboard.setPrimaryClip(ClipData.newPlainText("words", "just words"))
        images.changed()
        assertFalse(images.offered)
        val picture = ClipData(ClipDescription("shot", arrayOf("image/png")), ClipData.Item(Uri.parse("content://example.com/shot.png")))
        clipboard.setPrimaryClip(picture)
        images.changed()
        assertTrue(images.offered)
        images.dismiss()
        assertFalse(images.offered)
        images.refresh()
        assertFalse(images.offered)
        clipboard.setPrimaryClip(ClipData(ClipDescription("next", arrayOf("image/jpeg")), ClipData.Item(Uri.parse("content://example.com/next.jpg"))))
        images.changed()
        assertTrue(images.offered)
    }

    @Test fun aPictureLeftOnTheClipboardIsCopiedForTheDraft() {
        val shared = java.io.File(context.filesDir, "screenshot.png").apply { writeBytes(byteArrayOf(1, 2, 3)) }
        val clip = ClipData(ClipDescription("shot", arrayOf("image/png")), ClipData.Item(Uri.fromFile(shared)))
        // A copy of the app's own in cache/chat-captures, which the draft keeps and cleans up like a camera photo.
        val copied = java.io.File(pastedImages(context, clip, MAX_COMPOSER_MEDIA) { Uri.fromFile(it) }.single().path!!)
        assertEquals(java.io.File(context.cacheDir, "chat-captures").canonicalPath, copied.parentFile!!.canonicalPath)
        assertTrue(copied.name.startsWith("pasted-") && copied.name.endsWith(".png"))
        assertArrayEquals(byteArrayOf(1, 2, 3), copied.readBytes())
        assertEquals(1, pastedImages(context, ClipData(clip).apply { addItem(ClipData.Item(Uri.fromFile(shared))) }, 1) { Uri.fromFile(it) }.size)

        // One the app cannot read is left out rather than attached broken.
        val missing = ClipData(ClipDescription("shot", arrayOf("image/png")), ClipData.Item(Uri.parse("content://example.com/missing.png")))
        assertEquals(emptyList<Uri>(), pastedImages(context, missing, MAX_COMPOSER_MEDIA) { Uri.fromFile(it) })
        val words = ClipData.newPlainText("words", "just words")
        assertEquals(emptyList<Uri>(), pastedImages(context, words, MAX_COMPOSER_MEDIA) { Uri.fromFile(it) })
    }
}
