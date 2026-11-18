package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import persona.core.designsystem.InlineTone
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JInlineStatus
import persona.core.designsystem.JSheet
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PersonaIcons
import persona.core.designsystem.rememberCopy

/**
 * A successful uninstall whose cleanup needs the user. The hub's warnings stay readable and selectable until OK;
 * dismissing with a drag, the scrim or Back is disabled so the list cannot be lost by accident.
 */
@Composable
internal fun ExtensionRemovalWarningsSheet(
    notice: ExtensionRemovalNotice,
    onDone: () -> Unit,
    modifier: Modifier = Modifier,
) {
    if (notice.warnings.isEmpty()) return
    val copy = rememberCopy()
    val palette = PersonaTheme.palette
    JSheet(onDismiss = {}, modifier = modifier.testTag("extension-removal-warnings"), title = notice.title, dismissible = false) {
        Column(Modifier.padding(vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            JInlineStatus(stringResource(R.string.settings_ext_removal_warnings_text), tone = InlineTone.Caution)
            Text(stringResource(R.string.settings_ext_removal_warnings_header), style = PersonaTheme.type.caption, color = palette.ink2)
            Column(
                Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(Tokens.Space.s),
            ) {
                notice.warnings.forEachIndexed { index, warning ->
                    SelectionContainer {
                        Text(
                            warning,
                            modifier = Modifier.fillMaxWidth().testTag("extension-removal-warning-$index"),
                            style = PersonaTheme.type.callout,
                            color = palette.ink,
                        )
                    }
                }
            }
            JButton(
                stringResource(R.string.settings_ext_removal_copy),
                { copy(notice.warnings.joinToString("\n"), null) },
                Modifier.fillMaxWidth(),
                kind = JButtonKind.Secondary,
                icon = PersonaIcons.copy,
            )
            JButton(stringResource(R.string.settings_ext_removal_done), onDone, Modifier.fillMaxWidth())
        }
    }
}
