package persona.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource

/** Android's large-window shortcut reference. It lists only shortcuts implemented by this wave and the terminal. */
@Composable
internal fun KeyboardShortcutsSheet(onDismiss: () -> Unit) {
    val rows = listOf(
        stringResource(R.string.designsystem_shortcuts_send) to stringResource(R.string.designsystem_shortcuts_send_keys),
        stringResource(R.string.designsystem_shortcuts_newline) to stringResource(R.string.designsystem_shortcuts_newline_keys),
        stringResource(R.string.designsystem_shortcuts_close) to stringResource(R.string.designsystem_shortcuts_close_keys),
        stringResource(R.string.designsystem_shortcuts_help) to stringResource(R.string.designsystem_shortcuts_help_keys),
        stringResource(R.string.designsystem_shortcuts_terminal) to stringResource(R.string.designsystem_shortcuts_terminal_keys),
    )
    JDialog(
        onDismiss = onDismiss,
        title = stringResource(R.string.designsystem_shortcuts_title),
        dismiss = stringResource(R.string.designsystem_close) to onDismiss,
    ) {
        Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).testTag("keyboard-shortcuts")) {
            rows.forEachIndexed { index, row ->
                if (index > 0) JDivider()
                Column(Modifier.fillMaxWidth()) {
                    Text(row.first, style = PersonaTheme.type.bodyStrong, color = PersonaTheme.palette.ink)
                    Text(row.second, style = PersonaTheme.type.code, color = PersonaTheme.palette.ink2)
                }
            }
        }
    }
}
