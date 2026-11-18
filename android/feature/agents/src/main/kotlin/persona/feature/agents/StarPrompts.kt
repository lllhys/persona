package persona.feature.agents

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JButtonSize
import persona.core.designsystem.JCard
import persona.core.designsystem.JChip
import persona.core.designsystem.JIcon
import persona.core.designsystem.JIconButton
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PersonaIcons

// The GitHub star prompts of the Agents list (#5761, docs/design/DESIGN_SYSTEM.md "GitHub star prompts"; the rules are
// core:domain's StarPrompt.kt). Both open the project in the browser, where the user's GitHub session is: the caller's
// onStar.

/** Under the first-run empty state, centred: Star on GitHub, and ✕ ("Hide") to hide it for good. */
@Composable
internal fun StarPill(onStar: () -> Unit, onHide: () -> Unit, modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
        JChip(stringResource(R.string.agents_star_on_github), Modifier.testTag("agents.starPill"), onClick = onStar,
            icon = PersonaIcons.gitHubStar, role = Role.Button, onRemove = onHide, removeLabel = stringResource(R.string.agents_star_hide))
    }
}

/**
 * The milestone card in the hub notice's slot, drawn as [HubNoticeRow]: the star in the accent, the words whole in
 * secondary text, Star on GitHub under them (beside them a phone's width cut the sentence short, as on the web) and ✕
 * ("Later") at the end.
 */
@Composable
internal fun StarPromptCard(onStar: () -> Unit, onLater: () -> Unit, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    JCard(modifier.fillMaxWidth().testTag("agents.starCard"), padding = Tokens.Space.m) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            JIcon(PersonaIcons.gitHubStar, null, Modifier.padding(top = Tokens.Space.xxs), tint = palette.accent, decorative = true)
            Column(Modifier.weight(1f)) {
                Text(stringResource(R.string.agents_star_card), style = PersonaTheme.type.callout, color = palette.ink2)
                // As JNotice's action under its words: a quiet button's grey would read as more of the words.
                JButton(stringResource(R.string.agents_star_on_github), onStar, Modifier.padding(top = Tokens.Space.s),
                    kind = JButtonKind.Secondary, size = JButtonSize.Small)
            }
            JIconButton(PersonaIcons.close, stringResource(R.string.agents_star_later), onLater, Modifier.offset(y = -Tokens.Space.s),
                raised = false)
        }
    }
}
