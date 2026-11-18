package persona.feature.inbox

import androidx.compose.ui.res.stringResource

import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview

/**
 * What can be done with an item besides replying by hand (iOS `InboxActionBar`, the old app's detail): "AI draft" — a
 * reply in the user's voice, edited and confirmed here — and "Hand to AI", the item handed to Persona or an Agent with a
 * request ([InboxHandOffSheet]). With [primaryAction] (the mail page's "Reply") they sit beside it as decision buttons.
 */
@Composable
internal fun InboxAiActions(
    onDraft: () -> Unit,
    onHandOff: () -> Unit,
    modifier: Modifier = Modifier,
    primaryAction: JAction? = null,
    draftEnabled: Boolean = true,
    showHandOff: Boolean = true,
) {
    if (primaryAction == null) Row(
        modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
    ) {
        JChip(stringResource(R.string.inbox_ai_draft), icon = PhIcons.Sparkle, onClick = { if (draftEnabled) onDraft() })
        if (showHandOff) JChip(stringResource(R.string.inbox_hand_to_ai), icon = PhIcons.Robot, onClick = onHandOff)
    } else DecisionButtons(primary = primaryAction, modifier = modifier,
        secondary = if (showHandOff) JAction(stringResource(R.string.inbox_hand_to_ai), PhIcons.Robot, onClick = onHandOff) else null,
        more = listOf(JAction(stringResource(R.string.inbox_ai_draft), PhIcons.Sparkle, enabled = draftEnabled, onClick = onDraft)))
}

@Preview(showBackground = true, widthDp = 320)
@Preview(showBackground = true, widthDp = 320, fontScale = 1.5f)
@Composable
private fun InboxAiActionsPreview() {
    PersonaTheme(dynamicColor = false) {
        InboxAiActions({}, {}, Modifier.padding(Tokens.Space.m))
    }
}
