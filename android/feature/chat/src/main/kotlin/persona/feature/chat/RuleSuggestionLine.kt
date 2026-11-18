package persona.feature.chat

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.RuleSuggestion
import persona.core.contracts.RuleSuggestionMade
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JButtonSize
import persona.core.designsystem.JIcon
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PersonaIcons
import persona.core.domain.tasks.RuleOutcome
import persona.core.domain.tasks.TasksStore
import persona.core.domain.tasks.ruleAlwaysKey
import persona.core.domain.tasks.ruleNotNowKey

// The rule a card suggests after the same answer three times (#4929, docs/assistant/unified-entry.md "A rule after the
// same answer three times"; docs/android/android.md "Rule suggestions, held decisions, recommended setup and purpose
// descriptions (#5802)"): one quiet block under a plan card, a to-do's decision and a question a supervisor passed up.
// Nothing is drawn for a card without the field (an older hub).

/** Where a suggested rule would apply: the computer and the directory, whichever the hub named; null for neither. */
@Composable
private fun ruleScope(suggestion: RuleSuggestion): String? {
    val machine = suggestion.scope.machine?.takeIf(String::isNotBlank) ?: suggestion.scope.machineId?.takeIf(String::isNotBlank)
    val directory = suggestion.scope.directory?.takeIf(String::isNotBlank)
    val where = if (machine != null && directory != null)
        stringResource(persona.core.designsystem.R.string.designsystem_core_dot_joined, machine, directory)
    else machine ?: directory ?: return null
    return stringResource(R.string.chat_rule_scope, where)
}

/**
 * Exactly what Always stores, by the hub's `creates`: a remembered kind of task, a standing authorization, or nothing
 * (an empty `creates`, with the setting that would answer such questions when the hub names it). A `creates` this build
 * does not know gets no line of ours: the hub's own text says what it is, and we do not guess.
 */
@Composable
private fun ruleCreates(suggestion: RuleSuggestion): List<String> = when (suggestion.creates) {
    RuleSuggestion.TASK_TRUST -> listOf(stringResource(R.string.chat_rule_creates_trust))
    RuleSuggestion.STANDING_AUTHORIZATION -> {
        val scope = suggestion.scope
        // an action or a condition a newer hub names keeps the hub's own word
        val action = when (scope.action) {
            "merge" -> stringResource(R.string.chat_rule_action_merge)
            "push" -> stringResource(R.string.chat_rule_action_push)
            "deploy-test" -> stringResource(R.string.chat_rule_action_deploy_test)
            else -> scope.action.orEmpty()
        }
        val conditions = scope.conditions.orEmpty().filter(String::isNotBlank).map { condition ->
            when (condition) {
                "ci_green" -> stringResource(R.string.chat_rule_condition_ci_green)
                "no_conflicts" -> stringResource(R.string.chat_rule_condition_no_conflicts)
                else -> condition
            }
        }
        listOfNotNull(
            stringResource(R.string.chat_rule_creates_standing, action),
            conditions.takeIf { it.isNotEmpty() }?.let {
                stringResource(R.string.chat_rule_conditions, it.joinToString(stringResource(R.string.chat_tasks_list_separator)))
            },
            scope.quote?.takeIf(String::isNotBlank)?.let { stringResource(R.string.chat_rule_quote, it) },
        )
    }
    // a `delegate` suggestion stores no rule: it names the setting that would answer such questions
    "" -> listOf(suggestion.setting?.takeIf(String::isNotBlank)?.let { stringResource(R.string.chat_rule_creates_none_setting, it) }
        ?: stringResource(R.string.chat_rule_creates_none))
    else -> emptyList()
}

/** What Always made, with where the rule is listed and removed; a rule this build does not know is only said to be made. */
@Composable
private fun ruleMade(made: RuleSuggestionMade, suggestion: RuleSuggestion): String = when (made.creates) {
    RuleSuggestion.TASK_TRUST -> stringResource(R.string.chat_rule_made_trust)
    RuleSuggestion.STANDING_AUTHORIZATION -> stringResource(R.string.chat_rule_made_standing)
    "" -> (made.setting ?: suggestion.setting)?.takeIf(String::isNotBlank)?.let { stringResource(R.string.chat_rule_made_none_setting, it) }
        ?: stringResource(R.string.chat_rule_made_none)
    else -> stringResource(R.string.chat_rule_made_other)
}

/**
 * The suggested rule of [card] (a key of the card's own: `plan:<id>`, a to-do's decision round, `card:<id>`): what the
 * hub noticed in its own words, where the rule would apply and exactly what Always stores, then Always and Not now.
 * After Always the block says what was made and where it is listed and removed, and stays for the card although the hub
 * sends the card again without its [suggestion]. Not now, and a suggestion that is gone (404), leave nothing.
 */
@Composable
internal fun RuleSuggestionLine(
    card: String, suggestion: RuleSuggestion?, tasks: TasksStore, onError: (Throwable) -> Unit,
    manager: String? = null, enabled: Boolean = true, modifier: Modifier = Modifier,
) {
    val answers by tasks.ruleAnswers.collectAsStateWithLifecycle()
    val busy by tasks.busy.collectAsStateWithLifecycle()
    val answer = answers[card]
    val made = answer?.outcome as? RuleOutcome.Made
    // The hub sent the card without its line: one put away here is settled, and a later one is drawn again.
    if (suggestion == null && answer != null && made == null) LaunchedEffect(card) { tasks.ruleSettled(card) }
    val live = suggestion?.takeIf { it.isShown }
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    if (answer != null && made != null && (live == null || live.id == answer.suggestion.id)) {
        Row(modifier.fillMaxWidth().padding(horizontal = Tokens.Space.m).testTag("rule-made-$card"),
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            JIcon(PersonaIcons.done, decorative = true, size = Tokens.Space.l, tint = palette.positive)
            Text(ruleMade(made.made, answer.suggestion), Modifier.weight(1f), style = type.caption, color = palette.ink2)
        }
        return
    }
    val shown = live?.takeIf { it.id != answer?.suggestion?.id } ?: return
    val always = ruleAlwaysKey(card) in busy
    val notNow = ruleNotNowKey(card) in busy
    val open = enabled && !always && !notNow
    Column(modifier.fillMaxWidth().padding(horizontal = Tokens.Space.m).testTag("rule-suggestion-$card"),
        verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        Text(shown.text, Modifier.testTag("rule-text"), style = type.caption, color = palette.ink2)
        ruleScope(shown)?.let { Text(it, Modifier.testTag("rule-scope"), style = type.caption, color = palette.ink3) }
        ruleCreates(shown).forEach { Text(it, Modifier.testTag("rule-creates"), style = type.caption, color = palette.ink3) }
        Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            JButton(stringResource(R.string.chat_rule_always), { tasks.ruleAlways(card, shown, onError, manager) },
                Modifier.testTag("rule-always-$card"), kind = JButtonKind.Secondary, size = JButtonSize.Small,
                enabled = open, loading = always)
            JButton(stringResource(R.string.chat_rule_not_now), { tasks.ruleNotNow(card, shown, onError, manager) },
                Modifier.testTag("rule-not-now-$card"), kind = JButtonKind.Quiet, size = JButtonSize.Small,
                enabled = open, loading = notNow)
        }
    }
}
