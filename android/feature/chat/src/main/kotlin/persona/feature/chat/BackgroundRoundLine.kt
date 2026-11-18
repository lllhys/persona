package persona.feature.chat

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import persona.core.domain.tasks.BackgroundRound

/** The hub's "Fold background rounds" switch (#4314; on until read and on a hub without it) and the main conversation. */
internal data class BackgroundFoldSetting(val on: Boolean = true, val main: String? = null) {
    companion object {
        /**
         * The last value read this run, by conversation (in memory only): until the hub answers again the page draws
         * with it, so a user who turned folding off does not see a round fold for a moment.
         */
        private val lastRead = java.util.concurrent.ConcurrentHashMap<String, BackgroundFoldSetting>()

        fun lastKnown(conversation: String): BackgroundFoldSetting = lastRead[conversation] ?: BackgroundFoldSetting()
        fun store(conversation: String, setting: BackgroundFoldSetting) { lastRead[conversation] = setting }
    }
}

/**
 * A folded background round of the main conversation (#4314, docs/assistant/assistant.md "Background rounds"): one muted
 * line, `@<source> · Reported from the background` (`· Working in the background…` while it runs), the same line as the work summary;
 * a folded supervision round says "Handled in the background".
 * A tap opens it and its whole transcript follows it in place; another tap folds it again.
 */
@Composable
internal fun BackgroundRoundLine(round: BackgroundRound, onToggle: (String) -> Unit) {
    // A quiet supervision round (Glass, #4339 C13): "Handled in the background", "· Handling in the background…" while it runs (iOS `SupervisionRecordLine`).
    val label = if (round.supervision) stringResource(if (round.live) R.string.chat_supervision_record_live else R.string.chat_supervision_record)
        else stringResource(if (round.live) R.string.chat_background_round_live else R.string.chat_background_round,
            stringResource(R.string.chat_source_label, sourceTitle(round.source)))
    AssistantFoldLine(label, round.open, Modifier.testTag("background-round-${round.id}")) { onToggle(round.id) }
}
