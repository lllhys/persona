package persona.feature.assistant.proactive

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.snapping.rememberSnapFlingBehavior
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.setProgress
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.Machine
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.contracts.api.Run
import persona.core.contracts.api.ScheduledTask
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.feature.assistant.R
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.ZoneId
import java.util.Locale
import kotlin.math.abs
import kotlin.math.roundToInt

private val WheelRow = Tokens.Space.Standard.xxl + Tokens.Space.Standard.s
private val DayButton = Tokens.Space.Standard.xxl + Tokens.Space.Standard.s
private val TimeColumn = Tokens.Space.Standard.xxl * 2 + Tokens.Space.Standard.s

/** A caption under a section, in the page's quiet ink. */
@Composable
internal fun SectionNote(text: String) {
    Text(text, Modifier.fillMaxWidth().padding(start = Tokens.Space.xs, end = Tokens.Space.xs, top = Tokens.Space.s),
        style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
}

/** A timeline row, like Reminders: the time large on the left, what happens on the right, how long until. */
@Composable
internal fun UpcomingRow(item: Upcoming, now: Instant, zone: ZoneId, position: GroupPosition, onClick: (() -> Unit)?) {
    val say = rememberProactiveText()
    JGroupItem(position) {
        JRow(
            item.title, subtitle = item.detail, value = say.until(item.at, now, zone),
            leading = {
                Column(Modifier.widthIn(min = TimeColumn)) {
                    Text(say.hm(item.at, zone), style = PersonaTheme.type.number, color = PersonaTheme.palette.ink, maxLines = 1)
                    Text(say.day(item.at, now, zone), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2,
                        maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            },
            onClick = onClick, chevron = false,
        )
    }
}

/**
 * A repeating task: its rule in words, the next and last run, what it runs with when that is not the default, its push and Inbox choices,
 * who set it up, and its switch. The picture carries a mark when the last run failed (danger) or when Persona made it.
 */
@Composable
internal fun TaskRow(
    task: ScheduledTask, lastRun: LastRun?, say: ProactiveText, now: Instant, zone: ZoneId, position: GroupPosition, busy: Boolean,
    onToggle: (Boolean) -> Unit, onOpen: () -> Unit, onMore: () -> Unit,
) {
    val palette = PersonaTheme.palette
    val failed = lastRun?.failed == true
    val title = task.displayTitle(say)
    val lines = listOfNotNull(
        listOfNotNull(say.rule(task, now, zone),
            stringResource(R.string.assistant_proactive_push_asked_mark).takeIf { task.pushesOnlyAsked },
            taskInboxMark(task)).joinToString(" · "),
        task.nextLine(say, now, zone),
        lastRun?.line(say, now, zone),
        task.runsWith(say),
        stringResource(R.string.assistant_proactive_made_by_assistant).takeIf { task.byAssistant },
    )
    JGroupItem(position) {
        JRow(
            // Lines may wrap (a long reason), so leave room for them.
            title, subtitle = lines.joinToString("\n"), subtitleLines = lines.size + 2,
            leading = {
                Box(Modifier.stableAlpha(if (task.enabled) 1f else 0.55f)) {
                    JAvatar(task.title, icon = taskIcon(task.title, task.repeat),
                        badge = when { failed -> PhIcons.Warning; task.byAssistant -> PhIcons.Sparkle; else -> null },
                        badgeColor = if (failed) palette.danger.dot else null)
                }
            },
            trailing = {
                if (busy) JSpinner(Modifier.size(Tokens.Space.Standard.xl))
                else {
                    val description = stringResource(R.string.assistant_proactive_auto_run, title)
                    JSwitch(task.enabled, onToggle, Modifier.semantics { contentDescription = description })
                }
            },
            onClick = onOpen, onLongClick = onMore,
        )
    }
}

/** The Inbox setting beside the push mark; null for Don't or a value this app does not know. */
@Composable
private fun taskInboxMark(task: ScheduledTask): String? = when (task.inbox) {
    ScheduledTask.INBOX_WORTHY -> stringResource(R.string.assistant_proactive_inbox_worthy_mark)
    ScheduledTask.INBOX_EVERY -> stringResource(R.string.assistant_proactive_inbox_every_mark)
    else -> null
}

/** A one-off Persona scheduled (or the user did): when, where it goes, what it will do. ⋯ holds "Run now" and "Delete". */
@Composable
internal fun OneOffRow(
    task: ScheduledTask, machines: List<Machine>, say: ProactiveText, now: Instant, zone: ZoneId, position: GroupPosition, busy: Boolean,
    onOpen: () -> Unit, onMore: () -> Unit,
) {
    val title = task.displayTitle(say)
    val at = parseTime(task.at)
    val lines = listOfNotNull(
        at?.let { stringResource(R.string.assistant_proactive_moment_until, say.moment(it, now, zone), say.until(it, now, zone)) },
        task.whereLine(say, machines),
        task.prompt.lineSequence().firstOrNull { it.isNotBlank() }?.trim()?.takeIf { it != title },
        task.runsWith(say),
        stringResource(R.string.assistant_proactive_push_asked_mark).takeIf { task.pushesOnlyAsked },
        taskInboxMark(task),
    )
    JGroupItem(position) {
        JRow(
            title, subtitle = lines.joinToString("\n"), subtitleLines = lines.size + 1,
            leading = {
                Box(Modifier.stableAlpha(if (task.enabled) 1f else 0.55f)) {
                    JAvatar(title, icon = if (task.origin != null) PhIcons.ArrowBendUpLeft else PhIcons.BellRinging)
                }
            },
            trailing = {
                if (busy) JSpinner(Modifier.size(Tokens.Space.Standard.xl))
                else JIconButton(PhIcons.DotsThree, stringResource(R.string.assistant_proactive_actions_for, title), onMore)
            },
            onClick = onOpen, onLongClick = onMore, chevron = false,
        )
    }
}

/** One run: how it went, when, and why when it did not run. Opens the conversation it started. */
@Composable
internal fun RunRow(
    run: Run, title: String, time: String, position: GroupPosition, conversation: SessionKey?, say: ProactiveText,
    onOpen: (SessionKey) -> Unit,
) {
    val palette = PersonaTheme.palette
    val outcome = run.outcome
    val (icon, tint) = when (outcome) {
        RunOutcome.Ran -> PhIcons.CheckCircle to palette.done.dot
        RunOutcome.Failed -> PhIcons.WarningCircle to palette.danger.dot
        RunOutcome.Skipped -> PhIcons.ClockCountdown to palette.ink3
        RunOutcome.Unknown -> PhIcons.Question to palette.ink3
    }
    JGroupItem(position) {
        JRow(
            title, subtitle = listOf(time, run.detail(say)).filter { it.isNotBlank() && it != title }.joinToString("\n"), subtitleLines = 3,
            leading = { JIcon(icon, stringResource(outcome.word), size = Tokens.Space.xl, tint = tint, selected = true) },
            onClick = conversation?.let { key -> { onOpen(key) } },
        )
    }
}

/** A row that leads somewhere else in the page ("All 5 follow-ups"). */
@Composable
internal fun MoreRow(title: String, position: GroupPosition, onClick: () -> Unit) {
    JGroupItem(position) { JRow(title, onClick = onClick) }
}

/**
 * Hours and minutes as two wheels, 24-hour, like the clock app. Scrolling snaps to a value; tapping a value moves
 * there; TalkBack adjusts each wheel like a slider.
 */
@Composable
internal fun TimeWheel(time: String, onTimeChange: (String) -> Unit, modifier: Modifier = Modifier) {
    val parts = time.split(":").mapNotNull { it.toIntOrNull() }
    val hour = parts.getOrNull(0)?.coerceIn(0, 23) ?: 9
    val minute = parts.getOrNull(1)?.coerceIn(0, 59) ?: 0
    val latest by rememberUpdatedState(hour to minute)
    Box(modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
        // The band behind the chosen row.
        Box(Modifier.fillMaxWidth().height(WheelRow).clip(PersonaTheme.shapes.controlShape).background(PersonaTheme.palette.surfaceStrong))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Wheel(24, hour, stringResource(R.string.assistant_proactive_hours)) { onTimeChange(String.format(Locale.ROOT, "%02d:%02d", it, latest.second)) }
            Text(":", style = PersonaTheme.type.number, color = PersonaTheme.palette.ink2, modifier = Modifier.padding(horizontal = Tokens.Space.s))
            Wheel(60, minute, stringResource(R.string.assistant_proactive_minutes)) { onTimeChange(String.format(Locale.ROOT, "%02d:%02d", latest.first, it)) }
        }
    }
}

@Composable
private fun Wheel(count: Int, value: Int, description: String, onValue: (Int) -> Unit) {
    val state = rememberLazyListState(initialFirstVisibleItemIndex = value)
    val scope = rememberCoroutineScope()
    val report by rememberUpdatedState(onValue)
    val current by rememberUpdatedState(value)
    val selected by remember(state) {
        derivedStateOf {
            val info = state.layoutInfo
            val center = (info.viewportStartOffset + info.viewportEndOffset) / 2
            info.visibleItemsInfo.minByOrNull { abs(it.offset + it.size / 2 - center) }?.index ?: state.firstVisibleItemIndex
        }
    }
    // Report the value the wheel settles on; follow outside changes (a template, a reset) when it is still.
    LaunchedEffect(state) {
        snapshotFlow { state.isScrollInProgress }.filter { !it }.collect { if (selected != current) report(selected) }
    }
    LaunchedEffect(value) { if (!state.isScrollInProgress && selected != value) state.scrollToItem(value) }
    LazyColumn(
        Modifier.height(WheelRow * 5).width(WheelRow * 2).semantics {
            contentDescription = description
            stateDescription = "$selected"
            progressBarRangeInfo = ProgressBarRangeInfo(selected.toFloat(), 0f..(count - 1).toFloat(), steps = count - 2)
            setProgress { target -> scope.launch { state.animateScrollToItem(target.roundToInt().coerceIn(0, count - 1)) }; true }
        },
        state = state, flingBehavior = rememberSnapFlingBehavior(state),
        contentPadding = PaddingValues(vertical = WheelRow * 2), horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        items(count) { index ->
            val distance = abs(index - selected)
            Box(
                Modifier.height(WheelRow).fillMaxWidth().clickable(role = Role.Button) { scope.launch { state.animateScrollToItem(index) } },
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    String.format(Locale.ROOT, "%02d", index),
                    style = if (distance == 0) PersonaTheme.type.number else PersonaTheme.type.title,
                    color = when (distance) { 0 -> PersonaTheme.palette.ink; 1 -> PersonaTheme.palette.ink2; else -> PersonaTheme.palette.ink3 },
                    modifier = Modifier.stableAlpha(if (distance > 1) 0.6f else 1f), maxLines = 1,
                )
            }
        }
    }
}

/** Mon … Sun as round buttons (squares on the board) plus "Every day" / "Weekdays" / "Weekends". The last day cannot be turned off. */
@Composable
internal fun WeekdayPicker(days: List<Int>, onDaysChange: (List<Int>) -> Unit, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val say = rememberProactiveText()
    val chosen = normalizeDays(days).ifEmpty { (1..7).toList() }.toSet()
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        Row(Modifier.fillMaxWidth()) {
            (1..7).forEach { day ->
                val on = day in chosen
                val name = say.weekday(day)
                Box(Modifier.weight(1f), contentAlignment = Alignment.Center) {
                    Box(
                        Modifier.size(DayButton).clip(PersonaTheme.shapes.chipShape).background(if (on) palette.action else palette.surfaceStrong)
                            .clickable(role = Role.Checkbox) {
                                val next = if (on) chosen - day else chosen + day
                                if (next.isNotEmpty()) onDaysChange(normalizeDays(next.toList()))
                            }
                            .semantics { selected = on; contentDescription = name },
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(say.dayName(day), style = PersonaTheme.type.label, color = if (on) palette.onAction else palette.ink)
                    }
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            JChip(stringResource(R.string.assistant_proactive_every_day), selected = chosen.size == 7, onClick = { onDaysChange(emptyList()) })
            JChip(stringResource(R.string.assistant_proactive_weekdays), selected = chosen == Workdays.toSet(), onClick = { onDaysChange(Workdays) })
            JChip(stringResource(R.string.assistant_proactive_weekends), selected = chosen == setOf(6, 7), onClick = { onDaysChange(listOf(6, 7)) })
        }
    }
}

/** The intervals as chips, wrapping; one the hub has that is not in the list is offered too. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun IntervalPicker(minutes: Int, onChange: (Int) -> Unit, modifier: Modifier = Modifier) {
    val say = rememberProactiveText()
    val options = (TaskIntervals + minutes.takeIf { it in IntervalRange }.let(::listOfNotNull)).distinct().sorted()
    FlowRow(modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
        verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        options.forEach { option -> JChip(say.interval(option), selected = option == minutes, onClick = { onChange(option) }) }
    }
}

/** How many days ahead a one-off can be put on its chips; further ones the hub made stay as they are. */
private const val OnceDays = 14

/** The day of a one-off as chips (Today, Tomorrow, Wed…) over the clock wheel; a past time is not offered. */
@Composable
internal fun OncePicker(at: Instant?, now: Instant, zone: ZoneId, onChange: (Instant) -> Unit, modifier: Modifier = Modifier) {
    val say = rememberProactiveText()
    val chosen = (at ?: nextHour(now, zone)).atZone(zone)
    val today = now.atZone(zone).toLocalDate()
    val days = remember(today, chosen.toLocalDate()) {
        ((0 until OnceDays).map { today.plusDays(it.toLong()) } + chosen.toLocalDate()).distinct().sorted()
    }
    val latest by rememberUpdatedState(chosen)
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.l)) {
        Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            days.forEach { day ->
                val instant = day.atTime(chosen.toLocalTime()).atZone(zone).toInstant()
                JChip(say.day(instant, now, zone), selected = day == chosen.toLocalDate(), onClick = { onChange(instant) })
            }
        }
        TimeWheel(String.format(Locale.ROOT, "%02d:%02d", chosen.hour, chosen.minute), { time ->
            val parts = time.split(":").mapNotNull { it.toIntOrNull() }
            if (parts.size == 2) onChange(latest.toLocalDate().atTime(parts[0], parts[1]).atZone(zone).toInstant())
        })
    }
}
