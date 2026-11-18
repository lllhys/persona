package persona.feature.agents

import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import persona.core.contracts.api.AgentLauncher
import persona.core.contracts.api.UnavailableLauncher
import persona.core.designsystem.*
import persona.core.designsystem.dense
import persona.core.designsystem.icons.PersonaIcons
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.models.AgentModelPanel

/**
 * Why New Agent grays out a launcher on the computer picked (#4530, iOS `LauncherBlock`, docs/server/launcher-health.md):
 * its health there is blocked. It stays listed with the reason, and choosing it shows how to fix it rather than picking
 * it; the help's Try Anyway picks it after all (a start with it that works clears the record). A failing or repairing
 * launcher is offered as before: the hub is not done with it.
 */
internal data class LauncherBlock(
    /** The failure's class (`LauncherHealth.kind`); "" when the record has none. */
    val kind: String,
    /** The record's text (redacted by the hub), under Details; null when it has none. */
    val detail: String?,
) {
    /** The new terminal tab's shell could not run it, though the login shell the node checks in can. */
    val notInTerminal: Boolean get() = kind == NOT_IN_TERMINAL

    /** The one line under its name. */
    @get:StringRes val reason: Int get() = when (kind) {
        NOT_IN_TERMINAL -> R.string.agents_launcher_blocked_not_in_terminal
        "auth" -> R.string.agents_launcher_blocked_auth
        "model_route" -> R.string.agents_launcher_blocked_model_route
        "cli_outdated" -> R.string.agents_launcher_blocked_cli_outdated
        "timeout" -> R.string.agents_launcher_blocked_timeout
        else -> R.string.agents_launcher_blocked_other
    }

    /** The help's title. */
    @get:StringRes val title: Int get() =
        if (notInTerminal) R.string.agents_launcher_help_title_terminal else R.string.agents_launcher_help_title_computer

    companion object {
        const val NOT_IN_TERMINAL = "not_in_terminal"
    }
}

/** Why New Agent grays it out on its computer; null when it can be picked (never checked, working, failing, repairing). */
internal val AgentLauncher.block: LauncherBlock?
    get() {
        val h = health?.takeIf { it.state == "blocked" } ?: return null
        return LauncherBlock(h.kind.orEmpty(), h.error?.trim()?.takeIf(String::isNotEmpty))
    }

/**
 * The launcher a computer's form starts with (iOS `LauncherChoices.initial`): [saved] (the one used there last) unless it
 * is blocked now, else the first that is not; only when every one is blocked, [saved] (or the first), grayed reason and
 * all on its row, rather than nothing to start with.
 */
internal fun initialLauncher(saved: String?, launchers: List<AgentLauncher>): String {
    launchers.firstOrNull { it.name == saved && it.block == null }?.let { return it.name }
    launchers.firstOrNull { it.block == null }?.let { return it.name }
    if (saved != null && launchers.any { it.name == saved }) return saved
    return launchers.firstOrNull()?.name.orEmpty()
}

/**
 * The providers New Agent lists off after [type]'s launchers (#8109, the web's `newAgentPanel`): the hub's `unavailable`
 * for this computer ([NewAgentState.unavailable], the Persona-managed providers their own-service rule leaves out where
 * the agent has a sign-in of its own), never one the computer lists already. Each goes under the type it runs (Claude
 * Code when the hub names none), or under whichever type is shown when the computer has no launcher of its type, so it
 * is never just missing. [query] finds them as it finds the launchers.
 */
internal fun leftOutLaunchers(unavailable: List<UnavailableLauncher>, launchers: List<AgentLauncher>, type: String, query: String = ""): List<UnavailableLauncher> {
    val types = launchers.mapTo(mutableSetOf()) { it.agentType }
    return unavailable.filter { u ->
        val runs = AgentModelPanel.engineId(u.agent)
        launchers.none { it.name == u.name } && (runs == type || runs !in types) &&
            (query.isBlank() || u.name.contains(query, true) || u.title.contains(query, true))
    }
}

/**
 * How to fix a launcher New Agent grays out (iOS `LauncherBlockedHelp`): why it can't start there, what to do about it,
 * the hub's own words under Details, and the ways on: Add Provider for one the terminal can't run ([onAddProvider],
 * null on a computer of another group of hubs, where a provider added in Settings would land on the wrong hub), or
 * Try Anyway, which picks it after all.
 */
@Composable
internal fun LauncherBlockedSheet(
    launcher: AgentLauncher, block: LauncherBlock, onAddProvider: (() -> Unit)?, onTryAnyway: () -> Unit, onDismiss: () -> Unit,
) {
    var details by remember { mutableStateOf(false) }
    val palette = PersonaTheme.palette
    JSheet(onDismiss = onDismiss, title = stringResource(block.title), scrollable = true, modifier = Modifier.testTag("launcher-help")) {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.l)) {
            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.Top) {
                JIcon(PhIcons.WarningCircle, null, size = 20.dp, tint = palette.cautionText, modifier = Modifier.padding(top = 2.dp.dense))
                Column(verticalArrangement = Arrangement.spacedBy(2.dp.dense)) {
                    Text(launcher.name, style = PersonaTheme.type.headline, color = palette.ink, maxLines = 1)
                    Text(stringResource(block.reason), style = PersonaTheme.type.callout, color = palette.cautionText)
                }
            }
            Text(if (block.notInTerminal) stringResource(R.string.agents_launcher_help_not_in_terminal, launcher.name)
                else stringResource(R.string.agents_launcher_help_failed), style = PersonaTheme.type.body, color = palette.ink)
            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                Text(stringResource(R.string.agents_launcher_help_how), style = PersonaTheme.type.headline, color = palette.ink)
                val steps = if (block.notInTerminal) listOf(R.string.agents_launcher_help_fix_startup, R.string.agents_launcher_help_fix_provider)
                    else listOf(R.string.agents_launcher_help_fix_other)
                steps.forEach { step ->
                    Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.Top) {
                        JIcon(PersonaIcons.success, null, size = 16.dp, tint = palette.accent, modifier = Modifier.padding(top = 3.dp.dense))
                        Text(stringResource(step), style = PersonaTheme.type.body, color = palette.ink)
                    }
                }
            }
            block.detail?.let { detail ->
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    JTextButton(stringResource(R.string.agents_launcher_help_details), { details = !details },
                        icon = if (details) PersonaIcons.collapse else PersonaIcons.expand)
                    if (details) SelectionContainer {
                        Text(detail, Modifier.fillMaxWidth().clip(PersonaTheme.shapes.controlShape).background(palette.sunken).padding(Tokens.Space.s),
                            style = PersonaTheme.type.code, color = palette.ink2)
                    }
                }
            }
            Text(stringResource(if (block.notInTerminal) R.string.agents_launcher_help_clears_terminal else R.string.agents_launcher_help_clears),
                style = PersonaTheme.type.body, color = palette.ink2)
            Column(Modifier.padding(top = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                if (block.notInTerminal && onAddProvider != null)
                    JButton(stringResource(R.string.agents_launcher_help_add_provider), onAddProvider, Modifier.fillMaxWidth())
                JButton(stringResource(R.string.agents_launcher_help_try_anyway), onTryAnyway, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary)
            }
        }
    }
}
