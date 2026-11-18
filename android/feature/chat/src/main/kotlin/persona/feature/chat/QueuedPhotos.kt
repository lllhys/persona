package persona.feature.chat

import android.content.Context
import android.net.Uri
import android.webkit.MimeTypeMap
import androidx.core.content.FileProvider
import persona.core.contracts.ImagePayload
import java.io.File
import java.util.Base64
import java.util.UUID

/** Photos taken back with a queued message as the composer's attachments, and how many of them could not be put back. */
internal data class RestoredPhotos(val attachments: List<ComposerAttachment>, val lost: Int)

/**
 * The photos a queued message carried, which the hub returns beside a Persona conversation's queue as base64 (iOS
 * `restoreQueued`'s `sent`): each is written to `cache/chat-captures/` and handed out through the chat's FileProvider like a
 * camera photo ([share]), so it comes back to the composer as the photo it was and goes up to the computer as any photo does.
 * A payload that is not base64 or cannot be written is left out and counted in [RestoredPhotos.lost]. Call off the main thread.
 */
internal fun restoredPhotos(context: Context, payloads: List<ImagePayload>,
    share: (File) -> Uri = { FileProvider.getUriForFile(context, "${context.packageName}.chat.files", it) }): RestoredPhotos {
    val folder = File(context.cacheDir, "chat-captures").apply { mkdirs() }
    val restored = payloads.mapNotNull { payload ->
        runCatching {
            val bytes = Base64.getMimeDecoder().decode(payload.data)
            check(bytes.isNotEmpty()) { "an empty photo" }
            val extension = MimeTypeMap.getSingleton().getExtensionFromMimeType(payload.mime) ?: "jpg"
            val file = File.createTempFile("queued-", ".$extension", folder)
            val uri = try { file.writeBytes(bytes); share(file) } catch (error: Throwable) { file.delete(); throw error }
            ComposerAttachment(UUID.randomUUID().toString(), file.name, uri.toString(), image = true, size = bytes.size.toLong())
        }.getOrNull()
    }
    return RestoredPhotos(restored, payloads.size - restored.size)
}
