package persona.core.designsystem

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.movableContentOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

internal val LocalFlatPageSurface = staticCompositionLocalOf { false }

/** A settings navigation stack presented above its originating home, with iPhone-style grouped surfaces. */
@Composable
fun JSettingsSheet(onDismiss: () -> Unit, content: @Composable () -> Unit) {
    val base = PersonaTheme.palette
    val grouped = if (PersonaTheme.style != AppStyle.Glass) base else base.copy(
        background = if (base.dark) base.background else base.surfaceOpaque,
        surface = if (base.dark) base.surfaceOpaque else base.background,
        surfaceOpaque = if (base.dark) base.surfaceOpaque else base.background,
    )
    val settingsState = rememberSaveableStateHolder()
    val currentContent by rememberUpdatedState(content)
    val currentGrouped by rememberUpdatedState(grouped)
    // One sheet, frame and content invocation stay mounted; only presentation geometry changes.
    val settingsContent = remember {
        movableContentOf {
            settingsState.SaveableStateProvider("settings-sheet-content") {
                CompositionLocalProvider(LocalPalette provides currentGrouped, LocalFlatPageSurface provides true,
                    LocalHomeChrome provides null, LocalRootBack provides null, LocalRootLeading provides null,
                    LocalSplitDetail provides null) {
                    Box(Modifier.fillMaxSize()) { currentContent() }
                }
            }
        }
    }
    val presentation = settingsPresentation(LocalLargeWindowInput.current)
    val restoration = remember { SettingsFocusRestoration() }
    HardwareShortcutHost(Modifier.fillMaxSize(), requestFocusWhenLarge = false) {
        JSheet(
            onDismiss,
            Modifier.fillMaxSize(),
            contentPadding = 0.dp,
            bottomPadding = 0.dp,
            clearContainer = presentation == SettingsPresentation.PageSized,
            clearScrim = presentation == SettingsPresentation.PageSized,
            onClearFocus = { restoration.clearFocus() },
            showDragHandle = false,
        ) {
            JSettingsPresentationFrame(presentation, onDismiss, restoration) { settingsContent() }
        }
    }
}
