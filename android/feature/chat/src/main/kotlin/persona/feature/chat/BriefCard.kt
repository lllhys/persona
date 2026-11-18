package persona.feature.chat

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.BriefBody
import persona.core.contracts.BriefDone
import persona.core.contracts.BriefFailed
import persona.core.contracts.BriefProgress
import persona.core.contracts.BriefRow
import persona.core.contracts.BriefSetup
import persona.core.contracts.BriefTake
import persona.core.contracts.FeedItem
import persona.core.contracts.brief
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JButtonSize
import persona.core.designsystem.JCard
import persona.core.designsystem.JDivider
import persona.core.designsystem.JIcon
import persona.core.designsystem.LocalizedFormats
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.ProgressStep
import persona.core.designsystem.Status
import persona.core.designsystem.StepProgress
import persona.core.designsystem.StepState
import persona.core.designsystem.Tokens
import persona.core.designsystem.currentLocale
import persona.core.designsystem.icons.PersonaIcons
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.tasks.AssistantLines
import persona.core.domain.tasks.BriefOutcome
import persona.core.domain.tasks.briefAcceptOutcomeKey
import persona.core.domain.tasks.briefLaterOutcomeKey

/** A routine's fixed facts in the real assistant feed, shared by all three design-system styles. */
@Composable
internal fun BriefCard(item: FeedItem, host: AssistantTaskHost) {
    val brief = item.brief ?: return
    JCard(Modifier.fillMaxWidth().testTag("brief-card")) {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            Text(brief.title.ifBlank { stringResource(R.string.chat_brief_default_title) },
                style = PersonaTheme.type.label, color = PersonaTheme.palette.ink2, maxLines = 1)
            BriefWaiting(brief, host)
            JDivider()
            BriefPast(brief, host)
            JDivider()
            BriefAccept(brief, host)
            if (brief.later.isNotEmpty() || brief.laterTotal > 0) {
                JDivider()
                BriefLater(brief, host)
            }
            if (brief.progressTotal > 0) {
                JDivider()
                BriefProgress(brief, host)
            }
            if (brief.setup.isNotEmpty()) {
                JDivider()
                BriefSetupSection(brief, host)
            }
        }
    }
}

@Composable
private fun BriefHead(icon: PhIcon, status: Status?, title: String) {
    Row(Modifier.semantics { heading() }, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
        JIcon(icon, decorative = true, size = Tokens.Space.l,
            tint = status?.let { PersonaTheme.palette.status(it).text } ?: PersonaTheme.palette.accent)
        Text(title, style = PersonaTheme.type.label, color = PersonaTheme.palette.ink2)
    }
}

@Composable
private fun BriefNote(text: String) {
    Text(text, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
}

@Composable
private fun BriefMore(n: Int) {
    if (n > 0) BriefNote(stringResource(R.string.chat_brief_more, n))
}

@Composable
private fun BriefOpen(tag: String, onClick: () -> Unit) {
    JButton(stringResource(R.string.chat_brief_open), onClick, Modifier.testTag(tag), kind = JButtonKind.Secondary, size = JButtonSize.Small)
}

@Composable
private fun briefMetadata(parts: List<String>): String =
    parts.joinToString(stringResource(persona.core.designsystem.R.string.designsystem_separator))

@Composable
private fun briefMeta(row: BriefRow): String {
    val minutes = (row.waitedS.coerceAtLeast(60) + 59) / 60
    val waited = when {
        minutes < 60 -> stringResource(R.string.chat_brief_waited_minutes, minutes)
        minutes < 1_440 -> if (minutes % 60 == 0L) stringResource(R.string.chat_brief_waited_hours, minutes / 60)
            else stringResource(R.string.chat_brief_waited_hours_minutes, minutes / 60, minutes % 60)
        else -> if (minutes % 1_440 == 0L) stringResource(R.string.chat_brief_waited_days, minutes / 1_440)
            else stringResource(R.string.chat_brief_waited_days_hours, minutes / 1_440, (minutes % 1_440) / 60)
    }
    return briefMetadata(buildList {
        add(waited)
        if (row.nudged) add(stringResource(R.string.chat_brief_nudged))
        row.from?.takeIf(String::isNotBlank)?.let(::add)
    })
}

@Composable
private fun BriefWaiting(brief: BriefBody, host: AssistantTaskHost) {
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        if (brief.waitingUnknown) {
            BriefHead(PhIcons.Hand, Status.NeedsYou, stringResource(R.string.chat_brief_waiting_unknown))
            BriefNote(stringResource(R.string.chat_brief_waiting_unknown_note))
        } else {
            BriefHead(PhIcons.Hand, Status.NeedsYou, stringResource(R.string.chat_brief_waiting, brief.waiting.size))
            if (brief.waiting.isEmpty()) BriefNote(stringResource(R.string.chat_brief_nothing))
            brief.waiting.forEach { row ->
                val chat = row.chat(waiting = true, pending = host.pending)
                Row(Modifier.fillMaxWidth().padding(vertical = Tokens.Space.xs), verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
                        Text(row.title, style = PersonaTheme.type.bodyStrong, color = PersonaTheme.palette.ink)
                        Text(briefMeta(row), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                    }
                    if (chat != null) BriefOpen("brief-open-${row.id}") { host.onOpen(chat) }
                }
            }
        }
    }
}

@Composable
private fun BriefMarkLine(icon: PhIcon, status: Status, id: String, title: String, detail: String?, open: (() -> Unit)?, danger: Boolean = false) {
    Row(Modifier.fillMaxWidth().padding(vertical = Tokens.Space.xs), verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        JIcon(icon, decorative = true, size = Tokens.Space.m,
            tint = if (danger) PersonaTheme.palette.danger.text else PersonaTheme.palette.status(status).text)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
            Text(title, style = PersonaTheme.type.bodyStrong, color = PersonaTheme.palette.ink)
            detail?.takeIf(String::isNotBlank)?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
        }
        if (open != null) BriefOpen("brief-open-$id", open)
    }
}

@Composable
private fun briefTime(at: String?): String? = AssistantLines.time(at)?.let {
    LocalizedFormats.format(it, LocalizedFormats.TIME, currentLocale)
}

@Composable
private fun BriefPast(brief: BriefBody, host: AssistantTaskHost) {
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        BriefHead(PhIcons.Clock, Status.Working,
            stringResource(R.string.chat_brief_past, brief.hours, brief.doneTotal, brief.failedTotal))
        if (brief.done.isEmpty() && brief.failed.isEmpty()) BriefNote(stringResource(R.string.chat_brief_nothing))
        brief.failed.forEach { row: BriefFailed ->
            val label = stringResource(when {
                row.skipped -> R.string.chat_brief_skipped
                row.kind == "task" -> R.string.chat_brief_failed_task
                else -> R.string.chat_brief_failed_routine
            })
            BriefMarkLine(PhIcons.XCircle, Status.NeedsYou, row.id, row.title,
                briefMetadata(listOfNotNull(label, briefTime(row.at), row.why?.takeIf(String::isNotBlank))),
                if (row.kind == "task") ({ host.sheet.value = TaskSheet.Task(row.id) }) else null, danger = true)
        }
        brief.done.forEach { row: BriefDone ->
            BriefMarkLine(PhIcons.CheckCircle, Status.Unread, row.id, row.title, briefTime(row.at), open = {
                host.sheet.value = TaskSheet.Task(row.id)
            })
        }
        BriefMore(brief.pastMore)
    }
}

@Composable
private fun BriefAccept(brief: BriefBody, host: AssistantTaskHost) {
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        BriefHead(PhIcons.CheckCircle, Status.Unread, stringResource(R.string.chat_brief_accept, brief.accept.size))
        if (brief.accept.isEmpty()) BriefNote(stringResource(R.string.chat_brief_nothing))
        brief.accept.forEach { row -> BriefAcceptRow(row, host) }
    }
}

@Composable
private fun BriefAcceptRow(row: BriefRow, host: AssistantTaskHost) {
    val busy by host.tasks.busy.collectAsStateWithLifecycle()
    val outcomes by host.tasks.briefOutcomes.collectAsStateWithLifecycle()
    val outcome = outcomes[briefAcceptOutcomeKey(row.id, row.askedAt.orEmpty())]
    val askedAt = row.askedAt.orEmpty()
    val sending = "accept:${row.id}" in busy
    Column(Modifier.fillMaxWidth().padding(vertical = Tokens.Space.xs), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
        Text(row.title, style = PersonaTheme.type.bodyStrong, color = PersonaTheme.palette.ink)
        row.verdict?.takeIf(String::isNotBlank)?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink) }
        row.detail?.takeIf(String::isNotBlank)?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
        row.recommend?.takeIf(String::isNotBlank)?.let {
            Text(stringResource(R.string.chat_tray_recommended, it), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        }
        Text(briefMeta(row), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        if (outcome != null) {
            val text = when (outcome) {
                BriefOutcome.Taken -> stringResource(when (row.take) {
                    BriefTake.Accept, null -> R.string.chat_brief_accepted
                    BriefTake.GotIt -> R.string.chat_brief_noted
                    BriefTake.Recommendation -> R.string.chat_brief_recommendation_taken
                })
                BriefOutcome.Completed -> stringResource(R.string.chat_tray_done)
                BriefOutcome.AlreadyHandled -> stringResource(R.string.chat_brief_already_handled)
                BriefOutcome.HubTooOld -> stringResource(R.string.chat_brief_hub_too_old)
                else -> ""
            }
            Text(text, Modifier.testTag("brief-outcome-${row.id}"), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            val take = row.take
            if (outcome == null && take != null) {
                val label = stringResource(when (take) {
                    BriefTake.Accept -> R.string.chat_brief_accept_action
                    BriefTake.GotIt -> R.string.chat_brief_got_it
                    BriefTake.Recommendation -> R.string.chat_brief_take_recommendation
                })
                JButton(label, { host.tasks.acceptBrief(row.id, askedAt, host.onError, host.manager) },
                    Modifier.testTag("brief-take-${row.id}"), enabled = askedAt.isNotBlank() && !sending,
                    kind = JButtonKind.Primary, size = JButtonSize.Small, loading = sending)
            }
            if (outcome == null && row.isDecision) {
                JButton(stringResource(R.string.chat_brief_complete_directly), {
                    host.tasks.finishBrief(row.id, askedAt, host.onError, host.manager)
                }, Modifier.testTag("brief-finish-${row.id}"), enabled = askedAt.isNotBlank() && !sending,
                    kind = JButtonKind.Secondary, size = JButtonSize.Small, loading = sending)
            }
            BriefOpen("brief-open-${row.id}") { host.sheet.value = TaskSheet.Task(row.id) }
        }
    }
}

@Composable
private fun BriefLater(brief: BriefBody, host: AssistantTaskHost) {
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        BriefHead(PhIcons.Question, Status.Idle, stringResource(R.string.chat_brief_later, brief.laterTotal))
        brief.later.forEach { row -> BriefLaterRow(row, host) }
        BriefMore(brief.laterMore)
    }
}

@Composable
private fun BriefLaterRow(row: BriefRow, host: AssistantTaskHost) {
    val busy by host.tasks.busy.collectAsStateWithLifecycle()
    val outcomes by host.tasks.briefOutcomes.collectAsStateWithLifecycle()
    val outcome = outcomes[briefLaterOutcomeKey(row.id)]
    val chat = row.chat(waiting = false, pending = emptyList())
    Column(Modifier.fillMaxWidth().padding(vertical = Tokens.Space.xs), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
        Text(row.title, style = PersonaTheme.type.bodyStrong, color = PersonaTheme.palette.ink)
        Text(briefMeta(row), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        if (outcome != null) {
            val text = stringResource(when (outcome) {
                BriefOutcome.MarkedHandled -> R.string.chat_brief_marked_handled
                BriefOutcome.AlreadyHandled -> R.string.chat_brief_already_handled
                BriefOutcome.HubTooOld -> R.string.chat_brief_hub_too_old
                else -> R.string.chat_brief_marked_handled
            })
            Text(text, Modifier.testTag("brief-outcome-${row.id}"), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            if (outcome == null) JButton(stringResource(R.string.chat_brief_handled), {
                host.tasks.markBriefHandled(row.id, host.onError, host.manager)
            }, Modifier.testTag("brief-handled-${row.id}"), enabled = "later:${row.id}" !in busy,
                kind = JButtonKind.Secondary, size = JButtonSize.Small, loading = "later:${row.id}" in busy)
            if (chat != null) BriefOpen("brief-open-${row.id}") { host.onOpen(chat) }
        }
    }
}

/**
 * The week's recommended setup (#5085, docs/assistant/assistant.md "Recommended setup"): the hub's own counts and words,
 * drawn as they came. A row whose mechanism has a settings page in this app opens it; nothing is turned on from here.
 */
@Composable
private fun BriefSetupSection(brief: BriefBody, host: AssistantTaskHost) {
    Column(Modifier.testTag("brief-setup"), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        BriefHead(PersonaIcons.settings, null, stringResource(R.string.chat_brief_setup))
        BriefNote(stringResource(R.string.chat_brief_setup_intro))
        brief.setup.forEach { row: BriefSetup ->
            val open: (() -> Unit)? = row.page?.let { page -> host.onOpenSetup?.let { go -> { go(page) } } }
            // the hub's line takes the card's width, its button under it, as the card's other rows are laid out
            Column(Modifier.fillMaxWidth().padding(vertical = Tokens.Space.xs), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                Text(row.text, Modifier.testTag("brief-setup-${row.id}"), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink)
                if (open != null) FlowRow {
                    JButton(stringResource(R.string.chat_brief_setup_open), open, Modifier.testTag("brief-setup-open-${row.id}"),
                        kind = JButtonKind.Secondary, size = JButtonSize.Small)
                }
            }
        }
    }
}

private val briefStages = listOf(
    "plan" to R.string.chat_brief_stage_plan,
    "develop" to R.string.chat_brief_stage_develop,
    "review" to R.string.chat_brief_stage_review,
    "merge" to R.string.chat_brief_stage_merge,
    "deploy" to R.string.chat_brief_stage_deploy,
    "verify" to R.string.chat_brief_stage_verify,
)

@Composable
private fun BriefStage(row: BriefProgress) {
    val stage = row.stage?.trim()?.takeIf(String::isNotEmpty) ?: return
    if (row.stageKind == "general") return // A general task does not have the six code-task stages.
    val current = briefStages.indexOfFirst { it.first == stage }
    if (current < 0) {
        // A newer hub's stage keeps the hub's own words.
        BriefNote(stage)
        return
    }
    StepProgress(briefStages.mapIndexed { index, (_, label) ->
        ProgressStep(stringResource(label), when {
            index < current -> StepState.Complete
            index == current -> StepState.Active
            else -> StepState.Pending
        })
    })
}

@Composable
private fun BriefProgress(brief: BriefBody, host: AssistantTaskHost) {
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        BriefHead(PhIcons.GearSix, Status.Working, stringResource(R.string.chat_brief_progress, brief.progressTotal))
        brief.progress.forEach { row: BriefProgress ->
            Row(Modifier.fillMaxWidth().padding(vertical = Tokens.Space.xs), verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
                    Text(row.title, style = PersonaTheme.type.bodyStrong, color = PersonaTheme.palette.ink)
                    BriefStage(row)
                }
                BriefOpen("brief-open-${row.id}") { host.sheet.value = TaskSheet.Task(row.id) }
            }
        }
        BriefMore(brief.progressMore)
    }
}
