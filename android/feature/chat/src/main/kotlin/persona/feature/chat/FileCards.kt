package persona.feature.chat

import android.content.res.Resources
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import persona.core.designsystem.*
import persona.core.designsystem.icons.PersonaIcons
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.AttachedFile
import persona.core.domain.mediaClock

/**
 * The files a sent message carries (`<persona-file>`, iOS `AttachedFileCards`, web `FileCards`), above its bubble: one
 * card each with the name and "size · length". A tap opens the file on the session's machine (the file viewer plays
 * videos and recordings); a long press copies its path.
 */
internal fun attachedFilesFold(files: List<AttachedFile>, alignEnd: Boolean): Boolean = alignEnd && files.size > 4

/** Four or fewer stay open; past four an outgoing message starts with three, an answer always shows all. */
internal fun visibleAttachedFiles(files: List<AttachedFile>, alignEnd: Boolean, expanded: Boolean): List<AttachedFile> =
    if (attachedFilesFold(files, alignEnd) && !expanded) files.take(3) else files

@Composable
internal fun AttachedFileCards(files: List<AttachedFile>, alignEnd: Boolean = true) {
    if (files.isEmpty()) return
    val actions = LocalChatRowActions.current
    val open = actions?.openPath
    val resources = LocalResources.current
    val copy = rememberCopy()
    val pathCopied = stringResource(R.string.chat_file_path_copied)
    var expanded by remember(files, alignEnd) { mutableStateOf(false) }
    val folded = attachedFilesFold(files, alignEnd)
    Column(Modifier.fillMaxWidth().padding(bottom = Tokens.Space.xs), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s),
        horizontalAlignment = if (alignEnd) Alignment.End else Alignment.Start) {
        visibleAttachedFiles(files, alignEnd, expanded).forEach { file ->
            val icon = when (file.kind) { "video" -> PhIcons.Play; "audio" -> PhIcons.Waveform; else -> fileIcon(file.path) }
            FileAttachmentCard(file.name, attachedDetail(file, resources), leading = { FileCardIcon(icon) },
                onClick = open?.let { { it(file.path, null) } }, onClickLabel = stringResource(R.string.chat_file_open),
                onLongClick = { copy(file.path, pathCopied) }, onLongClickLabel = stringResource(R.string.chat_file_copy_path))
        }
        if (folded) {
            val hidden = files.size - 3
            val label = if (expanded) stringResource(R.string.chat_files_show_less)
                else resources.getQuantityString(R.plurals.chat_files_more, hidden, hidden)
            JTextButton(label, { expanded = !expanded }, Modifier.testTag("attached-files-fold"),
                icon = if (expanded) PersonaIcons.collapse else PersonaIcons.expand)
        }
    }
}

/** "24.1 MB · 0:12"; what it is when the tag says neither. */
internal fun attachedDetail(file: AttachedFile, resources: Resources): String {
    val parts = listOfNotNull(file.size.takeIf { it > 0 }?.let { formatBytes(it, resources) }, file.duration?.let(::mediaClock))
    if (parts.isNotEmpty()) return parts.joinToString(" · ")
    return resources.getString(when (file.kind) { "video" -> R.string.chat_file_video; "audio" -> R.string.chat_file_audio; else -> R.string.chat_file_file })
}

/**
 * The documents an answer names (Word, Excel, PowerPoint, iWork, PDF: what has a first page to show) as cards with that
 * page from the machine (iOS `DocumentCards`, `file/thumb`): only those there on the machine, at most six, side by
 * side. A tap opens the file as its chip did; other files stay chips ([FileChips]).
 */
@Composable
internal fun DocumentCards(paths: List<String>) {
    val actions = LocalChatRowActions.current
    val open = actions?.openPath ?: return
    if (paths.isEmpty()) return
    val presence = actions.paths
    if (presence != null) LaunchedEffect(presence, paths) { presence.ask(paths) }
    val shown = (if (presence == null) paths else paths.filter(presence::linkable)).take(6)
    if (shown.isEmpty()) return
    val copy = rememberCopy()
    val pathCopied = stringResource(R.string.chat_file_path_copied)
    FileCardRow(Modifier.padding(top = Tokens.Space.s)) {
        shown.forEach { path ->
            val thumbnail = actions.thumbnails?.invoke(path)
            FileAttachmentCard(fileLabel(path), stringResource(documentKind(path)),
                leading = { DocumentCardThumbnail(thumbnail?.first ?: path, fileIcon(path), thumbnail?.second) },
                onClick = { open(path, null) }, onClickLabel = stringResource(R.string.chat_file_open),
                onLongClick = { copy(path, pathCopied) }, onLongClickLabel = stringResource(R.string.chat_file_copy_path))
        }
    }
}

/** What a document is, as the Files app says it (iOS `DocumentCards.kind`). */
internal fun documentKind(path: String): Int = when (path.substringAfterLast('/').substringAfterLast('.', "").lowercase()) {
    "docx", "doc" -> R.string.chat_doc_word
    "xlsx", "xls" -> R.string.chat_doc_excel
    "pptx", "ppt" -> R.string.chat_doc_powerpoint
    "pages" -> R.string.chat_doc_pages
    "numbers" -> R.string.chat_doc_numbers
    "key" -> R.string.chat_doc_keynote
    "rtf" -> R.string.chat_doc_rtf
    "pdf" -> R.string.chat_doc_pdf
    else -> R.string.chat_doc_other
}
