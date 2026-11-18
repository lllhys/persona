package persona.feature.agents

import android.Manifest
import android.content.ActivityNotFoundException
import android.content.Context
import android.net.Uri
import android.text.format.Formatter
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.core.content.FileProvider
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import persona.core.contracts.CoreText
import persona.core.contracts.CoreTextFailure
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatComposerService
import persona.core.contracts.api.DraftUploadState
import persona.core.designsystem.*
import persona.core.designsystem.icons.PersonaIcons
import persona.core.domain.ComposerMessage
import persona.feature.chat.ChatWiringSource
import persona.feature.chat.ComposerAttachment
import persona.feature.chat.MAX_COMPOSER_FILES
import persona.feature.chat.cameraAllowed
import persona.feature.chat.chatWiringSource
import persona.feature.chat.decodeThumbnail
import persona.feature.chat.deleteCapture
import persona.feature.chat.openAppSettings
import persona.feature.chat.appLabel
import persona.feature.chat.selectAttachment
import persona.feature.chat.uploadAttachment
import persona.feature.chat.attachedFile
import java.io.File
import persona.feature.chat.R as ChatR

/*
 * New Agent's attachments (#2321, iOS NewAgentAttachments / NewAgentAttachmentRows, web new-agent.ts): photos and files
 * for the first message. Each goes to the computer picked as soon as it is picked, through the chat's upload service of
 * that computer's hub group, and the first message carries their paths as the chat composer writes them: the files'
 * after the words, the photos' in an `<attached_images>` block, drawn as thumbnails above the message. iOS and the web
 * send the photos as image data with the message instead; Android's composer has always sent them this way.
 */
internal object NewAgentAttachments {
    /** Photos at most, as the composer and iOS (`MessagePhotos.limit`). Ordinary files have their own ten places. */
    const val MAX_PHOTOS = 4
    const val MAX_FILES = MAX_COMPOSER_FILES

    /** The first message: exact words, video and file tags, then the photos' block, as the composer writes it. */
    fun firstMessage(typed: String, attachments: List<ComposerAttachment>, photosOnly: String, filesOnly: String = ""): String =
        ComposerMessage.text(typed, attachments.filter { it.image }.mapNotNull { it.path },
            attachments.filterNot { it.media }.mapNotNull { it.attachedFile() },
            attachments.filter { it.video }.mapNotNull { it.attachedFile() }, photosOnly = photosOnly, filesOnly = filesOnly)
}

/** How New Agent's attachments reach a computer (#2321). */
internal class NewAgentUploads(
    /** The upload service of the hub group [machineId] belongs to; null where there is none (offline, the demo). */
    val service: (machineId: String) -> ChatComposerService?,
    /** Puts an attachment on the computer and returns its path there (the composer's `uploadAttachment`). */
    val upload: suspend (ChatComposerService, SessionKey, ComposerAttachment, suspend (Long) -> Unit) -> String,
    /** What the first message says for photos sent without words ("Please look at the photos", the composer's). */
    val photosOnly: () -> String = { "" },
    /** What it says for ordinary files sent without words or photos ("Please look at the attached files"). */
    val filesOnly: () -> String = { "" },
    /** A retained failed row's reason, resolved in the current language. */
    val errorText: (name: String, error: Exception) -> String,
    /** An attachment left the form (removed, moved off, or sent): a photo this app took goes from the cache. */
    val discard: (ComposerAttachment) -> Unit = {},
)

/**
 * The chat's upload service for [machineId]: that of the hub group the machine belongs to (`bindingsFor`; its group
 * translation turns the merged machine id back into the hub's own before the upload goes out).
 */
internal fun ChatWiringSource.composerFor(machineId: String): ChatComposerService? =
    bindingsFor(SessionKey(machineId, "")).value?.composer

internal fun newAgentUploads(context: Context, source: ChatWiringSource) = NewAgentUploads(
    service = { source.composerFor(it) },
    upload = { service, session, attachment, onStaged ->
        uploadAttachment(context, service, session, attachment, onStaged = onStaged)
    },
    photosOnly = { context.getString(ChatR.string.chat_composer_photos_only) },
    filesOnly = { context.getString(ChatR.string.chat_composer_files_only) },
    errorText = { name, error -> uploadFailureRow(name, error).resolve(context.resources) },
    discard = { attachment ->
        if (attachment.uri.isNotEmpty()) CoroutineScope(Dispatchers.IO).launch { deleteCapture(context, attachment.uri) }
    },
)

/** The app's hub services for New Agent's attachments; null in previews and tests, which run without Hilt. */
@Composable
internal fun rememberNewAgentUploads(): NewAgentUploads? {
    val context = LocalContext.current.applicationContext
    if (context !is dagger.hilt.internal.GeneratedComponentManager<*>) return null
    val source = chatWiringSource()
    return remember(context, source) { newAgentUploads(context, source) }
}

/** The three sources of the add menu (the composer's: photo picker, camera, documents). */
@Stable
internal class NewAgentPickers(val photos: () -> Unit, val camera: () -> Unit, val files: () -> Unit) {
    /** The camera permission was refused: a dialog points to the system settings. */
    var cameraDenied by mutableStateOf(false)
}

/**
 * The pickers New Agent's add menu opens, as the composer's (feature/chat `ChatComposer`): photos only from the photo
 * picker (iOS `matching: .images`), any file from the documents picker, a photo taken by the system camera into
 * cache/chat-captures through the chat FileProvider. Null where the form takes no attachments.
 */
@Composable
internal fun rememberNewAgentPickers(model: NewAgentViewModel): NewAgentPickers? {
    if (!model.attachable) return null
    val context = LocalContext.current
    val resources = LocalResources.current
    val scope = rememberCoroutineScope()
    var cameraUri by rememberSaveable { mutableStateOf<String?>(null) }
    fun select(uris: List<Uri>, image: Boolean, capture: Boolean = false) {
        if (uris.isEmpty()) return
        scope.launch {
            var note: AgentText? = null
            val picked = mutableListOf<ComposerAttachment>()
            for (uri in uris) {
                // Invalid picks take no place: keep looking until ten valid files (or four valid photos) are retained.
                val room = (if (image) model.photoRoom() else model.fileRoom()) - picked.size
                if (room <= 0) {
                    note = AgentText(if (image) R.string.agents_attach_photo_limit else ChatR.string.chat_composer_file_limit)
                    if (capture) withContext(Dispatchers.IO) { deleteCapture(context, uri.toString()) }
                    continue
                }
                try {
                    val attachment = withContext(Dispatchers.IO) { selectAttachment(context, uri, image = image) }
                    // Over 200 MB, or unreadable: refused at once with the reason, never listed (iOS `FileUploads.start`).
                    if (attachment.uploadState != DraftUploadState.Invalid) picked += attachment
                    else {
                        if (capture) withContext(Dispatchers.IO) { deleteCapture(context, uri.toString()) }
                        note = AgentText.raw(attachment.error ?: resources.getString(ChatR.string.chat_composer_attachment_unreadable))
                    }
                } catch (e: CancellationException) { throw e }
                catch (_: Exception) {
                    if (capture) withContext(Dispatchers.IO) { deleteCapture(context, uri.toString()) }
                    note = AgentText(ChatR.string.chat_composer_attachment_unreadable)
                }
            }
            model.attach(picked)
            note?.let(model::notice)
        }
    }
    val photos = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(NewAgentAttachments.MAX_PHOTOS)) { select(it, true) }
    val files = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { select(it, false) }
    val camera = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { success ->
        cameraUri?.let { value ->
            if (success) select(listOf(Uri.parse(value)), true, capture = true)
            else runCatching { context.contentResolver.delete(Uri.parse(value), null, null) }
        }
        cameraUri = null
    }
    fun openCamera() {
        val folder = File(context.cacheDir, "chat-captures").apply { mkdirs() }
        val file = File.createTempFile("camera-", ".jpg", folder)
        val uri = FileProvider.getUriForFile(context, "${context.packageName}.chat.files", file)
        cameraUri = uri.toString()
        try { camera.launch(uri) }
        catch (error: Exception) {
            file.delete(); cameraUri = null
            model.notice(AgentText(if (error is ActivityNotFoundException) ChatR.string.chat_composer_no_camera_app else ChatR.string.chat_composer_camera_failed))
        }
    }
    fun launch(action: () -> Unit) {
        try { action() } catch (_: Exception) { model.notice(AgentText(ChatR.string.chat_composer_picker_failed)) }
    }
    val holder = remember { arrayOfNulls<NewAgentPickers>(1) }
    // The app declares CAMERA (pairing scans codes), so Android starts another app's camera for it only once granted.
    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) openCamera() else holder[0]?.cameraDenied = true
    }
    val pickers = remember(photos, files, camera, permission) {
        NewAgentPickers(
            photos = { launch { photos.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) } },
            camera = {
                if (cameraAllowed(context)) openCamera()
                else try { permission.launch(Manifest.permission.CAMERA) } catch (_: Exception) { holder[0]?.cameraDenied = true }
            },
            files = { launch { files.launch(arrayOf("*/*")) } },
        )
    }
    holder[0] = pickers
    if (pickers.cameraDenied) JDialog(onDismiss = { pickers.cameraDenied = false }, title = stringResource(ChatR.string.chat_composer_camera_title),
        text = stringResource(ChatR.string.chat_composer_camera_text, appLabel(context)),
        confirm = stringResource(ChatR.string.chat_composer_open_settings) to { pickers.cameraDenied = false; openAppSettings(context) })
    return pickers
}

/** A photo's thumbnail, decoded in the background; null while decoding or when it can't be read. */
@Composable
private fun rememberThumbnail(uri: String): ImageBitmap? {
    val context = LocalContext.current
    val target = with(LocalDensity.current) { AttachmentThumbSize.roundToPx() }
    val image by produceState<ImageBitmap?>(null, uri, target) {
        if (uri.isNotEmpty()) value = withContext(Dispatchers.IO) { decodeThumbnail(context.applicationContext, uri, target) }
    }
    return image
}

/**
 * The attachments under the first message (iOS `NewAgentAttachmentRows`): the photos as the composer's thumbnails (✕
 * takes one out), a row per file with its state and ✕, and the row that opens
 * the add menu. A launcher that takes none (Codex and other CLIs) has no menu, only what is left to take out, and says
 * so; [NewAgentState.attachNote] goes last.
 */
@Composable
internal fun NewAgentAttachmentRows(state: NewAgentState, model: NewAgentViewModel, pickers: NewAgentPickers?) {
    val adds = pickers != null && state.takesAttachments && model.canUpload(state.machineId)
    val note = state.attachNote
    if (!adds && state.attachments.isEmpty() && note == null) return
    val context = LocalContext.current
    val editable = !state.locked
    val photos = state.attachments.filter { it.image }
    val files = state.attachments.filterNot { it.image }
    var menu by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        if (photos.isNotEmpty()) {
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                photos.forEachIndexed { index, photo ->
                    key(photo.id) {
                        AttachmentThumb(rememberThumbnail(photo.uri), stringResource(ChatR.string.chat_attach_photo, index + 1),
                            state = if (photo.path != null) AttachmentState.Ready else AttachmentState.Uploading, onRemove = if (editable) ({ model.removeAttachment(photo.id) }) else null)
                    }
                }
            }
        }
        val rows = files.size + if (adds) 1 else 0
        files.forEachIndexed { index, file ->
            key(file.id) {
                val uploading = file.uploadState == DraftUploadState.Uploading
                val status = when {
                    file.error != null -> file.error!!
                    file.path == null && file.uploadState == DraftUploadState.Pending -> stringResource(ChatR.string.chat_attach_waiting)
                    file.path == null -> stringResource(ChatR.string.chat_attach_uploading)
                    file.size != null && file.size!! > 0 -> Formatter.formatShortFileSize(context, file.size!!)
                    else -> ""
                }
                JGroupItem(GroupPosition.of(index, rows)) {
                    JRow(file.name, subtitle = status.ifEmpty { null }, chevron = false,
                        leading = { if (uploading) JSpinner(Modifier.size(Tokens.Size.iconXS)) else JIcon(PersonaIcons.file, null, tint = PersonaTheme.palette.ink2) },
                        trailing = {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                if (file.error != null && file.uploadState != DraftUploadState.Invalid)
                                    JTextButton(stringResource(ChatR.string.chat_attach_retry), { model.retryAttachment(file.id) }, enabled = editable)
                                JIconButton(PersonaIcons.close, stringResource(R.string.agents_attach_remove, file.name), { model.removeAttachment(file.id) }, enabled = editable)
                            }
                    })
                }
            }
        }
        if (adds) JGroupItem(GroupPosition.of(rows - 1, rows)) {
            JRow(stringResource(R.string.agents_attach_add), Modifier.testTag("newAgent.attach"), leadingIcon = PersonaIcons.add,
                enabled = editable, onClick = { menu = true }, chevron = false)
        }
        // A launcher that takes its first message with the start takes no attachments (iOS `FirstMessage.codexRefusal`).
        if (!state.takesAttachments && state.attachments.isNotEmpty()) {
            Text(stringResource(R.string.agents_attach_launcher_refusal), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        }
        note?.let { Text(it.resolve(), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
    }
    if (menu && pickers != null) {
        val room = model.photoRoom() > 0
        JActionSheet(listOf(
            JAction(stringResource(ChatR.string.chat_composer_photos), PersonaIcons.photo, enabled = room) { menu = false; pickers.photos() },
            JAction(stringResource(ChatR.string.chat_composer_take_photo), PersonaIcons.camera, enabled = room) { menu = false; pickers.camera() },
            JAction(stringResource(ChatR.string.chat_composer_files), PersonaIcons.file) { menu = false; pickers.files() },
        ), onDismiss = { menu = false }, title = stringResource(R.string.agents_attach_add))
    }
}

/**
 * Why an upload failed, as iOS says it (`FileUploads.message`): a file over the limit, a computer or a hub too old for
 * uploads in their own words; anything else "Couldn't send “name”: why".
 */
internal fun uploadFailure(name: String, error: Exception): AgentText =
    when ((error as? CoreTextFailure)?.text?.key) {
        CoreText.Key.UploadTooLarge, CoreText.Key.ComposerFileTooLarge -> AgentText(ChatR.string.chat_attach_too_large, name)
        CoreText.Key.UploadNodeOutdated, CoreText.Key.UploadHubUnsupported -> AgentText(R.string.agents_text_raw, failureReasonText(error))
        else -> AgentText(R.string.agents_attach_upload_failed, name, failureReasonText(error))
    }

/**
 * A retained row's title is already the file's name and the notice says the whole failure once; the row says only why.
 * Size and version refusals keep their complete sentences from [uploadFailure] because those sentences are the reason.
 */
internal fun uploadFailureRow(name: String, error: Exception): AgentText =
    when ((error as? CoreTextFailure)?.text?.key) {
        CoreText.Key.UploadTooLarge, CoreText.Key.ComposerFileTooLarge,
        CoreText.Key.UploadNodeOutdated, CoreText.Key.UploadHubUnsupported -> uploadFailure(name, error)
        else -> AgentText(R.string.agents_text_raw, failureReasonText(error))
    }

/**
 * The form's toast (iOS `model.toast` over the New Agent sheet): what [NewAgentViewModel.notices] says, an error's look.
 * Drawn by the form itself, since the sheet covers the window's own toast.
 */
@Composable
internal fun NewAgentToast(model: NewAgentViewModel, modifier: Modifier = Modifier) {
    val toast = rememberToastState()
    val resources = LocalResources.current
    LaunchedEffect(model) {
        model.notices.collect { toast.show(FeedbackNotice(it.resolve(resources), style = FeedbackStyle.Error)) }
    }
    JToastHost(toast, modifier)
}

/**
 * Leaving the form without starting takes its attachments with it (iOS: the sheet's go with it): uploads stop and photos
 * taken for it leave the cache. A rotation redraws the form and keeps them.
 */
@Composable
internal fun CloseAttachmentsOnLeave(model: NewAgentViewModel) {
    val context = LocalContext.current
    DisposableEffect(model) {
        onDispose {
            var c: Context? = context
            while (c is android.content.ContextWrapper && c !is android.app.Activity) c = c.baseContext
            if ((c as? android.app.Activity)?.isChangingConfigurations != true) model.closeAttachments()
        }
    }
}
