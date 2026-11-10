package persona.feature.chat

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import persona.core.designsystem.JTextButton
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.WorkState
import persona.core.designsystem.text
import persona.core.designsystem.WorkStep as TimelineStep
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.lines.LineItem
import persona.core.domain.lines.TaskIndex
import persona.core.domain.lines.TaskNote

/** The chat's background tasks and the calls that started them (`ChatSnapshot.tasks`), for the notices' lines. */
// Not static: it changes with every call and notice, and only the notices' lines read it.
internal val LocalChatTaskIndex = compositionLocalOf { TaskIndex() }

/** Brings the call with this tool_use_id into view, opening the work line it is in; null: the chat cannot jump. */
internal val LocalChatJumpToCall = staticCompositionLocalOf<((toolUseId: String, notice: persona.core.contracts.Item) -> Unit)?> { null }

/**
 * A background task reporting back (iOS `TaskNoteRow`): one line in the tools' look, its description (a Monitor event:
 * the event) with what it is and how it went under it. A tap shows the whole command / event / result, "View output"
 * and "Jump to the step that started it".
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun TaskNoteLine(item: LineItem, last: Boolean = false, onFullText: ((persona.core.contracts.Item) -> Unit)? = null) {
    val note = item.task ?: return
    val index = LocalChatTaskIndex.current
    val text = remember(item, index) { index.note(item) }
    val jump = LocalChatJumpToCall.current
    val openPath = LocalChatRowActions.current?.openPath
    val body = if (item.kind == "tool_result") item.text?.takeIf(String::isNotBlank) ?: note.detail else note.detail
    var open by rememberSaveable(item.id) { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth()) {
        TimelineStep(taskNoteIcon(note), text.subject,
            detail = listOfNotNull(text.label.text(), text.outcome?.text()).joinToString(" · "),
            state = when {
                note.failed -> WorkState.Failed
                note.status == "waiting" -> WorkState.Waiting
                note.kind == "agent" && !note.terminal -> WorkState.Running
                else -> WorkState.Done
            },
            last = last, onClick = { open = !open })
        if (open) Column(Modifier.fillMaxWidth().padding(start = Tokens.Space.m),
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            when (note.kind) {
                "shell" -> CodeText("$ " + (note.detail ?: note.title))
                "agent" -> body?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
                else -> note.detail?.let { CodeText(it) }
            }
            if (note.kind == "agent" && body != null) {
                ToolReaderActions(null, body, onFullText?.takeIf { item.item.fullSize != null }?.let { read -> { read(item.item) } })
            }
            if (note.kind == "monitor" && (note.status == "event" || note.detail == null) && note.title != text.subject) {
                Text(note.title, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2, maxLines = 2)
            }
            // Wraps rather than squeezing a label onto two lines (long in English on a narrow phone).
            FlowRow(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                val file = note.outputFile?.takeIf(String::isNotBlank)
                if (file != null && openPath != null) {
                    JTextButton(stringResource(R.string.chat_task_note_output), { openPath(file, null) }, icon = PhIcons.FileText)
                }
                val call = text.callId
                if (call != null && jump != null) {
                    JTextButton(stringResource(R.string.chat_task_note_jump), { jump(call, item.item) }, icon = PhIcons.ArrowUp)
                }
            }
        }
    }
}

internal fun taskNoteIcon(note: TaskNote): PhIcon = when (note.kind) {
    "shell" -> toolCategory("Bash")
    "monitor" -> PhIcons.Broadcast
    "agent" -> toolCategory("Agent")
    else -> PhIcons.BellSimple
}
