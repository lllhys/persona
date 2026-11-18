package persona.feature.chat

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.Todo
import persona.core.contracts.TodoAsk
import persona.core.contracts.api.AskAnswer
import persona.core.designsystem.*
import persona.core.domain.tasks.AskDefault
import persona.core.domain.tasks.AskResponse
import persona.core.domain.tasks.asked
import persona.core.domain.tasks.confirmsOptions
import persona.core.domain.tasks.decisionOptions
import persona.core.domain.tasks.fallback
import persona.core.domain.tasks.fixedResponses

/** What the row says once its round was answered from here. */
private sealed interface DecisionAnswered {
    data class Chosen(val option: String) : DecisionAnswered
    data object Responded : DecisionAnswered

    /** Answered somewhere else meanwhile: the hub took nothing from here. */
    data object Handled : DecisionAnswered
}

/**
 * A decision answered on its row (#3804, docs/assistant/todos.md "Decisions the owner can answer"; the web's DecisionRow):
 * what is to be decided, where the task came from and stands in one line, each option with what it leads to (the
 * recommended one first and marked), what happens when nobody answers, then the options as buttons, "Other…" (opens the
 * task to answer in words) and a menu with the options that do not fit and the fixed answers. An option of a step that
 * cannot be undone or that others will see takes two presses: the first arms it ("Confirm: <option>"). Every answer
 * goes through the accept route with the round the owner read.
 */
@Composable
internal fun TaskDecisionAnswers(todo: Todo, ask: TodoAsk, host: AssistantTaskHost, enabled: Boolean = true, onOpen: (() -> Unit)? = null) {
    val options = ask.decisionOptions
    val busy by host.tasks.busy.collectAsStateWithLifecycle()
    val sending = "accept:${todo.id}" in busy
    val resources = LocalResources.current
    val locale = LocalizedFormats.locale(LocalContext.current)
    var armed by remember(todo.id, ask) { mutableStateOf<Int?>(null) }
    var answered by remember(todo.id, ask.askedAt) { mutableStateOf<DecisionAnswered?>(null) }
    var failure by remember(todo.id, ask) { mutableStateOf<String?>(null) }
    var changed by remember(todo.id, ask) { mutableStateOf(false) }
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type

    fun answer(answer: AskAnswer, done: DecisionAnswered) {
        failure = null; changed = false
        host.tasks.answerTask(todo, answer, onError = { failure = operationFeedback(it).text.resolve(resources) },
            onChanged = { changed = true }, onDone = { taken -> answered = if (taken) done else DecisionAnswered.Handled })
    }
    fun choose(index: Int) {
        if (ask.confirmsOptions && armed != index) { armed = index; return }
        answer(AskAnswer.Option(options[index].label), DecisionAnswered.Chosen(options[index].label))
    }
    // Another way to answer: the task's own page from a list; on that page, the composer routed to the task.
    val other = onOpen ?: { host.tasks.setRoute(host.conversation, todo.id); host.sheet.value = null }

    // The compact padding keeps the answers in reach in the tray, whose height is bounded.
    JCard(Modifier.fillMaxWidth().testTag("assistant-task-decision-${todo.id}"), padding = Tokens.Space.m) {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            if (onOpen != null) Text(todo.title, style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(ask.asked, Modifier.testTag("decision-question"), style = type.bodyStrong, color = palette.ink)
            ask.background?.takeIf(String::isNotBlank)?.let {
                Text(it, Modifier.testTag("decision-background"), style = type.caption, color = palette.ink2, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            if (ask.forBriefing) ForBriefingMark()
            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                options.forEachIndexed { index, option ->
                    Column(Modifier.testTag("decision-leads-$index")) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                            Text(option.label, Modifier.weight(1f, fill = false), style = type.callout, color = palette.ink,
                                maxLines = 1, overflow = TextOverflow.Ellipsis)
                            if (index == 0) JTag(stringResource(R.string.chat_decision_recommended))
                        }
                        option.leadsTo?.takeIf(String::isNotBlank)?.let {
                            Text(it, style = type.caption, color = palette.ink2, maxLines = 2, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
            }
            val fallback = when (val default = ask.fallback) {
                is AskDefault.TakenAt -> stringResource(R.string.chat_decision_default_at,
                    LocalizedFormats.format(default.at, LocalizedFormats.MONTH_DAY_TIME, locale))
                AskDefault.Never -> stringResource(R.string.chat_decision_default_never)
                null -> null
            }
            // what happens when nobody answers, then what waiting costs: one quiet line
            val cost = ask.costOfWaiting?.takeIf(String::isNotBlank)?.let { stringResource(R.string.chat_decision_cost, it) }
            val waits = if (fallback != null && cost != null) stringResource(persona.core.designsystem.R.string.designsystem_core_dot_joined, fallback, cost)
                else fallback ?: cost
            waits?.let { Text(it, Modifier.testTag("decision-default"), style = type.caption, color = palette.ink2, maxLines = 3, overflow = TextOverflow.Ellipsis) }
            failure?.let { Text(stringResource(R.string.chat_decision_failed, it), style = type.caption, color = palette.danger.text) }
            if (changed) Text(stringResource(R.string.chat_tray_accept_changed), style = type.caption, color = palette.cautionText)
            when (val done = answered) {
                null -> {
                    // a fixed answer given before (it still waits: Persona is to ask again with the background)
                    if (!ask.response.isNullOrBlank()) Text(stringResource(R.string.chat_decision_responded), style = type.caption, color = palette.ink2,
                        maxLines = 1, overflow = TextOverflow.Ellipsis)
                    DecisionAnswers(
                        options = options.mapIndexed { index, option ->
                            JAction(if (armed == index) stringResource(R.string.chat_decision_confirm, option.label) else option.label,
                                subtitle = option.leadsTo?.takeIf(String::isNotBlank), enabled = enabled) { choose(index) }
                        },
                        other = JAction(stringResource(R.string.chat_decision_other), enabled = enabled, onClick = other),
                        more = ask.fixedResponses.map { response ->
                            val (label, detail) = when (response) {
                                AskResponse.DecideYourself -> R.string.chat_decision_decide_yourself to R.string.chat_decision_decide_yourself_detail
                                AskResponse.NotWhatIAsked -> R.string.chat_decision_not_what_i_asked to R.string.chat_decision_not_what_i_asked_detail
                                AskResponse.ExplainFirst -> R.string.chat_decision_explain_first to R.string.chat_decision_explain_first_detail
                            }
                            JAction(stringResource(label), subtitle = stringResource(detail), enabled = enabled) {
                                answer(AskAnswer.Response(response.code), DecisionAnswered.Responded)
                            }
                            // ending the task without an answer stays possible, as before the options (#3038)
                        } + JAction(stringResource(R.string.chat_tray_finish_now), enabled = enabled, separatorBefore = true) {
                            failure = null; changed = false
                            host.tasks.finishTaskNow(todo, { failure = operationFeedback(it).text.resolve(resources) }, { changed = true })
                        },
                        overflowLabel = stringResource(R.string.chat_decision_more), modifier = Modifier.testTag("decision-answers"), busy = sending,
                    )
                    if (sending) JSpinner()
                }
                else -> Text(when (done) {
                    is DecisionAnswered.Chosen -> stringResource(R.string.chat_decision_chosen, done.option)
                    DecisionAnswered.Responded -> stringResource(R.string.chat_decision_responded)
                    DecisionAnswered.Handled -> stringResource(R.string.chat_decision_handled)
                }, Modifier.testTag("decision-done"), style = type.callout, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}
