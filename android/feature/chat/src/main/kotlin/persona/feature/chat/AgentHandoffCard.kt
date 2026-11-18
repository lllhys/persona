package persona.feature.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import java.time.Instant
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.UsageLimitWhen
import persona.core.designsystem.JCard
import persona.core.designsystem.JIcon
import persona.core.designsystem.JStatusDot
import persona.core.designsystem.LocalizedFormats
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.text
import persona.core.designsystem.Status
import persona.core.designsystem.Tokens
import persona.core.designsystem.currentLocale
import java.time.ZoneId
import persona.core.designsystem.icons.PersonaIcons
import persona.core.domain.lines.ApiErrorInfo
import persona.core.domain.tasks.AgentHandoff
import persona.core.domain.tasks.AssistantLines
import persona.core.domain.tasks.DispatchHint
import persona.core.domain.tasks.HandoffWake
import persona.core.domain.tasks.PlannedHandoff
import persona.core.domain.tasks.StartCause
import persona.core.domain.tasks.StartFailurePlain
import persona.core.domain.tasks.feedWake
import persona.core.domain.tasks.startFailurePlain
import persona.core.domain.tasks.taskHandoffs

/**
 * The cards of one dispatch (#5110, the web's `HandoffCards`): one per Agent, no heading over them. A card with an
 * Agent opens that Agent's page; before an Agent is up it opens the task.
 */
@Composable
internal fun DispatchHandoffCards(hint: DispatchHint, host: AssistantTaskHost) {
    val todos by host.tasks.todos.collectAsStateWithLifecycle()
    val plans by host.tasks.plans.collectAsStateWithLifecycle()
    val ready by host.tasks.todosReady.collectAsStateWithLifecycle()
    val handoffs = hint.tasks.flatMap { task ->
        val todo = task.todoId?.let { id -> todos.firstOrNull { it.id == id } }
        taskHandoffs(
            todo,
            PlannedHandoff(task.key ?: task.todoId ?: task.title, task.title, task.error, task.todoId),
            feedWake(task.wake),
            plans,
            ready,
        )
    }
    AgentHandoffCards(handoffs, host, Modifier.testTag("assistant-dispatch-hint"))
}

/** One card per hand-off, stacked. [cardTag] is on each card. */
@Composable
internal fun AgentHandoffCards(handoffs: List<AgentHandoff>, host: AssistantTaskHost, modifier: Modifier = Modifier, cardTag: String = "handoff-card") {
    if (handoffs.isEmpty()) return
    Column(modifier, verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        handoffs.forEach { AgentHandoffCard(it, host, Modifier.testTag(cardTag)) }
    }
}

/** cindy's hand-off card: a round ↳, the title and badge, when the Agent last moved and its state, and ↗. */
@Composable
internal fun AgentHandoffCard(handoff: AgentHandoff, host: AssistantTaskHost, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val todos by host.tasks.todos.collectAsStateWithLifecycle()
    val todo = handoff.todoId?.let { id -> todos.firstOrNull { it.id == id } }
    val session = handoff.machineId?.let { mid ->
        handoff.sessionId?.let { sid -> host.sessions.firstOrNull { it.machineId == mid && it.sessionId == sid } }
    }
    val agent = !handoff.machineId.isNullOrBlank() && !handoff.sessionId.isNullOrBlank()
    val closed = todo?.doneAt != null
    val interactive = agent || handoff.todoId != null
    val open: (() -> Unit)? = if (!interactive) null else {
        {
            val mid = handoff.machineId
            val sid = handoff.sessionId
            if (!mid.isNullOrBlank() && !sid.isNullOrBlank()) host.onOpen(SessionKey(mid, sid))
            else handoff.todoId?.let { host.sheet.value = TaskSheet.Task(it) }
        }
    }
    val title = session?.title?.trim()?.takeIf { it.isNotEmpty() }
        ?: handoff.agentTitle?.trim()?.takeIf { it.isNotEmpty() }
        ?: handoff.title.ifBlank { stringResource(R.string.chat_tasks_default_title) }
    JCard(modifier.semantics(mergeDescendants = true) {}, onClick = open, padding = Tokens.Space.l) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            Box(Modifier.size(Tokens.Size.wellXS).clip(CircleShape).background(palette.sunken), contentAlignment = Alignment.Center) {
                JIcon(PersonaIcons.followOn, decorative = true, size = Tokens.Size.iconXS, tint = palette.ink)
            }
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    Text(title, Modifier.weight(1f, fill = false), style = PersonaTheme.type.bodyStrong, color = palette.ink,
                        maxLines = 1, overflow = TextOverflow.Ellipsis)
                    HandoffBadge(handoff.wake)
                }
                HandoffMeta(handoff, session, agent, closed)
            }
            if (interactive) JIcon(PersonaIcons.open, decorative = true, size = Tokens.Size.iconXS, tint = palette.ink2)
        }
    }
}

@Composable
private fun HandoffBadge(wake: HandoffWake) {
    val palette = PersonaTheme.palette
    val filled = wake != HandoffWake.Active
    val label = when (wake) {
        HandoffWake.Active -> stringResource(R.string.chat_handoff_wake_active)
        HandoffWake.Resumed -> stringResource(R.string.chat_handoff_wake_resumed)
        HandoffWake.Created -> stringResource(R.string.chat_new_agent_started)
    }.uppercase(currentLocale)
    Text(label, Modifier.clip(CircleShape)
        .background(if (filled) palette.ink else palette.surface)
        .then(if (filled) Modifier else Modifier.border(Tokens.Size.line, palette.ink, CircleShape))
        .padding(horizontal = Tokens.Space.s, vertical = Tokens.Space.xxs)
        .testTag("handoff-badge"),
        style = PersonaTheme.type.micro.copy(fontWeight = FontWeight.SemiBold),
        color = if (filled) palette.background else palette.ink, maxLines = 1, softWrap = false)
}

@Composable
private fun HandoffMeta(handoff: AgentHandoff, session: Session?, agent: Boolean, closed: Boolean) {
    val palette = PersonaTheme.palette
    if (agent) {
        val at = AssistantLines.time(session?.activityAt ?: session?.updatedAt)
        // the minute tick, so a usage limit's line and Continue change when its grace ends
        val ticking = !session?.turnError.isNullOrBlank()
        val now by rememberNow(60_000, ticking || at != null)
        val nowMs = now.toEpochMilli()
        // a usage limit, the task still open: when it resets, in the caution color, not "Interrupted" (#8237)
        if (!closed && session?.isUsageLimited(nowMs) == true) {
            Text(usageLimitCardLine(session, nowMs), Modifier.testTag("handoff-usage-limit"), style = PersonaTheme.type.caption,
                color = palette.cautionText, maxLines = 2, overflow = TextOverflow.Ellipsis)
            return
        }
        // a turn the model service cut off, the task still open: said here, in the danger color (#8237)
        if (!closed && session?.interrupted == true) {
            Text(interruptedLine(session.turnError), Modifier.testTag("handoff-interrupted"), style = PersonaTheme.type.caption,
                color = palette.danger.text, maxLines = 2, overflow = TextOverflow.Ellipsis)
            return
        }
        val status = session?.status()
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            if (at != null) {
                JIcon(PersonaIcons.time, decorative = true, size = Tokens.Size.markXS, tint = palette.ink2)
                Text(lastActive(at, now), Modifier.weight(1f, fill = false), style = PersonaTheme.type.caption, color = palette.ink2,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
            } else {
                Text(stringResource(R.string.chat_handoff_not_active), Modifier.weight(1f, fill = false), style = PersonaTheme.type.caption,
                    color = palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (status != null) {
                Text("·", style = PersonaTheme.type.caption, color = palette.ink3, maxLines = 1)
                JStatusDot(status)
                val attention = status == Status.NeedsYou
                Text(status.localizedLabel, style = PersonaTheme.type.caption, maxLines = 1, softWrap = false,
                    color = if (attention) palette.status(Status.NeedsYou).text else palette.ink2)
            }
        }
        return
    }
    val failure = handoff.failure
    val (words, color) = when {
        failure != null -> startFailureCardLine(failure) to palette.danger.text
        handoff.ended && handoff.todoId != null -> stringResource(R.string.chat_handoff_closed) to palette.ink3
        handoff.ended -> stringResource(R.string.chat_handoff_gone) to palette.ink3
        else -> stringResource(R.string.chat_tasks_now_starting) to palette.ink3
    }
    Text(words, style = PersonaTheme.type.caption, color = color, maxLines = 2, overflow = TextOverflow.Ellipsis)
}

@Composable
private fun lastActive(at: Instant, now: Instant): String {
    val elapsed = (now.toEpochMilli() - at.toEpochMilli()).coerceAtLeast(0)
    if (elapsed < 45_000) return stringResource(R.string.chat_handoff_active_now)
    val minutes = elapsed / 60_000
    val whenText = when {
        minutes < 60 -> stringResource(R.string.chat_handoff_minutes_ago, minutes)
        minutes < 60 * 24 -> stringResource(R.string.chat_handoff_hours_ago, minutes / 60)
        else -> stringResource(R.string.chat_handoff_days_ago, minutes / (60 * 24))
    }
    return stringResource(R.string.chat_handoff_last_active, whenText)
}

/** The hand-off card's line: "Didn't start: <why>", or the one sentence that asks to open the task. */
@Composable
internal fun startFailureCardLine(raw: String): String {
    val why = startFailureWhy(raw)
    return if (why == null) stringResource(R.string.chat_start_failed_details) else stringResource(R.string.chat_tasks_now_failed, why)
}

/** The Needs you row's line: the why alone, or the sentence that asks to open the task. */
@Composable
internal fun startFailureRowLine(raw: String): String = startFailureWhy(raw) ?: stringResource(R.string.chat_start_failed_details)

/** "Interrupted · <why>" in the danger color's words, or the word alone when the error has no sentence. */
@Composable
internal fun interruptedLine(error: String?): String {
    val why = interruptedWhy(error)
    return if (why.isEmpty()) stringResource(R.string.chat_exchange_interrupted) else stringResource(R.string.chat_handoff_interrupted, why)
}

/** The hand-off card's line for a usage limit: "Usage Limit · resets <time>", or the word alone when nothing says when. */
@Composable
internal fun usageLimitCardLine(session: Session, nowMs: Long): String {
    val clock = usageLimitClock(session, nowMs)
    return when (session.usageLimitWhen(nowMs)) {
        is UsageLimitWhen.Resets -> stringResource(R.string.chat_usage_limit_resets, clock)
        is UsageLimitWhen.Resumes -> stringResource(R.string.chat_usage_limit_resumes, clock)
        UsageLimitWhen.AfterReset -> stringResource(R.string.chat_usage_limit_resumes_after)
        null -> stringResource(R.string.chat_usage_limit)
    }
}

/** The In progress row's second line: the limit's reason, then when it resets (iOS `usageLimitWhen`). */
@Composable
internal fun usageLimitRowSubtitle(session: Session, nowMs: Long): String {
    val reason = session.turnError?.trim()?.takeIf { it.isNotEmpty() }
    val clock = usageLimitClock(session, nowMs)
    val whenText = when (session.usageLimitWhen(nowMs)) {
        is UsageLimitWhen.Resets -> stringResource(R.string.chat_usage_resets, clock)
        is UsageLimitWhen.Resumes -> stringResource(R.string.chat_usage_resumes, clock)
        UsageLimitWhen.AfterReset -> stringResource(R.string.chat_usage_resumes_after)
        null -> null
    }
    return listOfNotNull(reason, whenText).joinToString(" · ")
}

/** The clock of a usage limit: the time when that is today, the date as well on another day (iOS `usageLimitClock`). */
@Composable
private fun usageLimitClock(session: Session, nowMs: Long): String {
    val at = when (val part = session.usageLimitWhen(nowMs)) {
        is UsageLimitWhen.Resets -> part.at
        is UsageLimitWhen.Resumes -> part.at
        else -> return ""
    }
    val zone = ZoneId.systemDefault()
    val now = Instant.ofEpochMilli(nowMs)
    val today = at.atZone(zone).toLocalDate() == now.atZone(zone).toLocalDate()
    return LocalizedFormats.format(at, if (today) LocalizedFormats.TIME else LocalizedFormats.MONTH_DAY_TIME, currentLocale, zone)
}

/** The cut-off reason without the "Interrupted" word: the row already says that word. */
@Composable
internal fun interruptedWhy(error: String?): String {
    val parsed = ApiErrorInfo.parse(error)?.reason?.text()
    return parsed?.takeIf { it.isNotBlank() } ?: error?.trim().orEmpty()
}

@Composable
private fun startFailureWhy(raw: String): String? = when (val plain = startFailurePlain(raw)) {
    is StartFailurePlain.Known -> stringResource(when (plain.cause) {
        StartCause.ConnectionLost -> R.string.chat_start_cause_connection
        StartCause.Offline -> R.string.chat_start_cause_offline
        StartCause.TimedOut -> R.string.chat_start_cause_timeout
        StartCause.NotAllowed -> R.string.chat_start_cause_denied
        StartCause.NeedsUpdate -> R.string.chat_start_cause_update
    })
    is StartFailurePlain.Sentence -> plain.text
    StartFailurePlain.Details -> null
}
