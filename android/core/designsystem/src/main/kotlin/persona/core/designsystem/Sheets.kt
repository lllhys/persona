package persona.core.designsystem

import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.getValue
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.DpOffset
import androidx.compose.ui.unit.dp
import persona.core.designsystem.icons.PhIcon
import kotlinx.coroutines.launch

/** One action in a menu or action sheet. */
@Immutable
data class JAction(
    val label: String,
    val icon: PhIcon? = null,
    val destructive: Boolean = false,
    val enabled: Boolean = true,
    val checked: Boolean = false,
    /** Draw a rule above this action, to group menus. */
    val separatorBefore: Boolean = false,
    val onClick: () -> Unit,
)

/** A bottom sheet in the style's shape and surface. Sheets sit above a scrim, so they are always opaque. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun JSheet(
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    title: String? = null,
    skipPartiallyExpanded: Boolean = true,
    /** False while an operation must finish (the sheet ignores drags, the scrim and back). */
    dismissible: Boolean = true,
    /** Called when a drag, the scrim or back tries to close a sheet that is not [dismissible] (to ask about unsaved edits). */
    onBlockedDismiss: (() -> Unit)? = null,
    /** Scroll the whole content; leave false when the content has its own lazy list. */
    scrollable: Boolean = false,
    contentPadding: androidx.compose.ui.unit.Dp? = null,
    bottomPadding: androidx.compose.ui.unit.Dp? = null,
    showDragHandle: Boolean = true,
    content: @Composable ColumnScope.() -> Unit,
) {
    val palette = PersonaTheme.palette
    val canDismiss by androidx.compose.runtime.rememberUpdatedState(dismissible)
    val blocked by androidx.compose.runtime.rememberUpdatedState(onBlockedDismiss)
    // Brief's surface is its card grey, the fill of its text fields and chips: on a grey sheet they vanish (no border
    // either), so its sheets are paper like its pages.
    val container = if (PersonaTheme.style == AppStyle.Brief) palette.background else palette.surfaceOpaque
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = skipPartiallyExpanded, confirmValueChange = {
        (it != SheetValue.Hidden || canDismiss).also { ok -> if (!ok) blocked?.invoke() }
    })
    val scope = androidx.compose.runtime.rememberCoroutineScope()
    ModalBottomSheet(
        onDismissRequest = { if (canDismiss) onDismiss() }, modifier = modifier,
        sheetState = sheetState,
        shape = PersonaTheme.shapes.sheetShape, containerColor = container, contentColor = palette.ink,
        scrimColor = palette.scrim, tonalElevation = 0.dp,
        dragHandle = if (!showDragHandle) null else ({ Box(Modifier.padding(top = 10.dp, bottom = 6.dp).size(36.dp, 4.dp).clip(CircleShape).background(palette.ink3.copy(alpha = 0.5f))) }),
    ) {
        // Opaque over a scrim: glass inside the sheet must not sample (and show through) the page behind it.
        androidx.compose.runtime.CompositionLocalProvider(LocalReadingInset provides 0.dp, LocalGlassSource provides null) {
        Column(Modifier.fillMaxWidth().dismissKeyboardOnBlankTap()
            // Content that grows once the sheet is open ("Session info" after it loads) would stay where the opening left the
            // sheet, its rest below the screen and out of reach: settle the sheet again at its new height.
            .onSizeChanged {
                if (skipPartiallyExpanded && sheetState.targetValue == SheetValue.Expanded) scope.launch { sheetState.expand() }
            }
            .then(if (scrollable) Modifier.verticalScroll(androidx.compose.foundation.rememberScrollState()) else Modifier).padding(horizontal = contentPadding ?: pagePadding).padding(bottom = bottomPadding ?: 16.dp)) {
            title?.let {
                Text(it, style = PersonaTheme.type.title, color = palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Spacer(Modifier.height(12.dp))
            }
            content()
        }
        }
    }
}

/** A long-press or overflow menu as a bottom sheet: easy to reach, room for a note under the actions. */
@Composable
fun JActionSheet(actions: List<JAction>, onDismiss: () -> Unit, title: String? = null, note: String? = null) {
    val palette = PersonaTheme.palette
    // When some actions have an icon, the others keep its place, so every label starts in one column.
    val iconColumn = actions.any { it.icon != null }
    JSheet(onDismiss, title = title) {
        Column(Modifier.weight(1f, fill = false).verticalScroll(androidx.compose.foundation.rememberScrollState())) {
        actions.forEach { action ->
            if (action.separatorBefore) JDivider(Modifier.padding(vertical = 6.dp))
            Row(
                Modifier.fillMaxWidth().heightIn(min = 52.dp).clip(PersonaTheme.shapes.controlShape)
                    .clickable(enabled = action.enabled, role = Role.Button) { onDismiss(); action.onClick() }
                    .stableAlpha(if (action.enabled) 1f else 0.4f).padding(horizontal = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                val color = if (action.destructive) palette.danger.text else palette.ink
                val icon = action.icon
                if (icon != null) { JIcon(icon, null, size = 22.dp, tint = if (action.destructive) palette.danger.dot else palette.ink2); Spacer(Modifier.width(16.dp)) }
                else if (iconColumn) Spacer(Modifier.width(22.dp + 16.dp))
                Text(action.label, style = PersonaTheme.type.body, color = color, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                if (action.checked) JIcon(persona.core.designsystem.icons.PhIcons.Check, null, size = 18.dp, tint = palette.ink)
            }
        }
        }
        note?.let {
            Spacer(Modifier.height(8.dp))
            Text(it, style = PersonaTheme.type.caption, color = palette.ink3, modifier = Modifier.padding(horizontal = 8.dp))
        }
    }
}

/**
 * A small anchored menu for a few choices next to the control that opened it. A tap outside closes it; what does not
 * fit the window scrolls. [content] goes above the [actions], with a rule between them: choices that are more than a
 * label (the sidebar's assistant switcher lists its assistants there, then New assistant and Manage assistants…).
 */
@Composable
fun JMenu(
    expanded: Boolean, onDismiss: () -> Unit, actions: List<JAction>, modifier: Modifier = Modifier, offset: DpOffset = DpOffset(0.dp, 4.dp),
    content: (@Composable ColumnScope.() -> Unit)? = null,
) {
    val palette = PersonaTheme.palette
    DropdownMenu(
        expanded, onDismiss, modifier, offset = offset, shape = PersonaTheme.shapes.controlShape,
        containerColor = palette.surfaceOpaque, tonalElevation = 0.dp, shadowElevation = 12.dp,
    ) {
        content?.let {
            it()
            if (actions.isNotEmpty()) HorizontalDivider(color = palette.line)
        }
        actions.forEach { action ->
            if (action.separatorBefore) HorizontalDivider(color = palette.line)
            DropdownMenuItem(
                text = { Text(action.label, style = PersonaTheme.type.body, color = if (action.destructive) palette.danger.text else palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                leadingIcon = action.icon?.let { { JIcon(it, null, size = 20.dp, tint = if (action.destructive) palette.danger.dot else palette.ink2) } },
                trailingIcon = if (action.checked) ({ JIcon(persona.core.designsystem.icons.PhIcons.Check, null, size = 18.dp, tint = palette.ink) }) else null,
                enabled = action.enabled, onClick = { onDismiss(); action.onClick() },
            )
        }
    }
}

/** A confirmation dialog. [confirm] is the primary action; mark it [destructive] for irreversible ones. */
@Composable
fun JDialog(
    onDismiss: () -> Unit,
    title: String,
    text: String? = null,
    confirm: Pair<String, () -> Unit>? = null,
    dismiss: Pair<String, () -> Unit>? = stringResource(R.string.designsystem_cancel) to onDismiss,
    destructive: Boolean = false,
    confirmEnabled: Boolean = true,
    content: (@Composable ColumnScope.() -> Unit)? = null,
) {
    val palette = PersonaTheme.palette
    androidx.compose.runtime.CompositionLocalProvider(LocalReadingInset provides 0.dp) {
    AlertDialog(
        onDismissRequest = onDismiss, containerColor = palette.surfaceOpaque, shape = PersonaTheme.shapes.panelShape, tonalElevation = 0.dp,
        title = { Text(title, style = PersonaTheme.type.title, color = palette.ink) },
        text = if (text == null && content == null) null else ({
            Column {
                text?.let { Text(it, style = PersonaTheme.type.callout, color = palette.ink2) }
                content?.let { Spacer(Modifier.height(if (text == null) 0.dp else 12.dp)); it() }
            }
        }),
        confirmButton = {
            confirm?.let { (label, action) ->
                JButton(label, action, kind = if (destructive) JButtonKind.Destructive else JButtonKind.Primary, size = JButtonSize.Medium, enabled = confirmEnabled)
            }
        },
        dismissButton = dismiss?.let { (label, action) -> { JButton(label, action, kind = JButtonKind.Quiet, size = JButtonSize.Medium) } },
    )
    }
}

/** Translucent-safe colour for Material components that draw their own surfaces. */
internal val PersonaPalette.opaqueSurface: Color get() = surfaceOpaque
