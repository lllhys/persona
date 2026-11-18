package persona.core.designsystem

import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * One suggestion for what is being typed: the [word] to type, what it does; [destructive] marks /clear. [section] is the
 * title of the group it belongs to, drawn above the first of a run (the @ list's "Message a Task"); [status] puts that
 * state's dot before the word (a task's).
 */
@Immutable
data class ComposerSuggestion(
    val word: String, val detail: String, val destructive: Boolean = false,
    val key: String = word, val flexible: Boolean = false,
    val section: String? = null, val status: Status? = null,
)

/**
 * Suggestions for the word being typed (the "/" quick messages), above the field inside the composer's accessory: the
 * word in bold. In "/" command rows, the detail sits beside it while both fit at their measured widths; otherwise it moves
 * below with no line limit. Quick-message and @ rows keep their single-line layout. Picking one calls [onPick]. Glass is a
 * soft well inside the floating composer, brief a paper list under hairlines, the board a bordered list. The list scrolls
 * when its rows exceed its height.
 * Items in sections have each section's title above its first row, a quiet heading.
 */
@Composable
fun ComposerSuggestions(items: List<ComposerSuggestion>, onPick: (ComposerSuggestion) -> Unit, modifier: Modifier = Modifier) {
    if (items.isEmpty()) return
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val shape = RoundedCornerShape(when (style) { AppStyle.Glass -> 20.dp.corner; AppStyle.Brief -> 16.dp.corner; AppStyle.Board -> 8.dp.corner })
    val frame = when (style) {
        AppStyle.Glass -> Modifier.clip(shape).background(palette.surface)
            .border(1.dp, Brush.verticalGradient(listOf(palette.strokeTop, palette.strokeBottom)), shape)
        AppStyle.Brief -> Modifier.clip(shape).background(palette.surface)
        AppStyle.Board -> Modifier.clip(shape).background(palette.surface).border(1.dp, palette.line, shape)
    }
    Column(modifier.fillMaxWidth().then(frame).heightIn(max = 272.dp).verticalScroll(rememberScrollState()).padding(vertical = 4.dp.dense)) {
        items.forEachIndexed { index, item ->
            if (index > 0) JDivider(Modifier.padding(horizontal = RowInset))
            val section = item.section?.takeIf { it != items.getOrNull(index - 1)?.section }
            if (section != null) Text(section, Modifier.fillMaxWidth().semantics { heading() }
                .padding(start = RowInset, end = RowInset, top = Tokens.Space.s, bottom = Tokens.Space.xxs),
                style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
            ComposerSuggestionRow(item, onPick)
        }
    }
}

/** The detail stays beside the word while both fit at their full widths; otherwise it wraps below the word. */
@Composable
private fun ComposerSuggestionRow(item: ComposerSuggestion, onPick: (ComposerSuggestion) -> Unit) {
    val palette = PersonaTheme.palette
    val spoken = stringResource(R.string.designsystem_with_state, item.word, item.detail)
    val wordStyle = PersonaTheme.type.bodyStrong
    val detailStyle = PersonaTheme.type.callout
    val wordMinimum = 84.dp
    val detailGap = 12.dp.dense
    val rowModifier = Modifier.fillMaxWidth().heightIn(min = 40.dp.row).clickable(role = Role.Button) { onPick(item) }
        .semantics(mergeDescendants = true) { contentDescription = spoken }
        .padding(horizontal = RowInset, vertical = 6.dp.dense)

    @Composable
    fun word(modifier: Modifier) = Text(item.word, style = wordStyle,
        color = if (item.destructive) palette.danger.text else palette.ink,
        maxLines = 1, overflow = if (item.flexible) TextOverflow.Ellipsis else TextOverflow.Clip, modifier = modifier)

    @Composable
    fun ordinaryRow() {
        Row(rowModifier, horizontalArrangement = Arrangement.spacedBy(detailGap), verticalAlignment = Alignment.CenterVertically) {
            item.status?.let { JStatusDot(it) }
            word(if (item.flexible) Modifier.weight(1f) else Modifier.widthIn(min = wordMinimum))
            Text(item.detail, style = detailStyle, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f))
        }
    }

    // Flexible words share the remaining width with their details; a status dot has its own intrinsic size. Keep those
    // existing layouts, and measure the ordinary command row, whose fixed slots make the full required width exact.
    if (item.flexible || item.status != null) {
        ordinaryRow()
        return
    }
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        val fits = with(density) {
            val wordWidth = maxOf(wordMinimum.roundToPx(), measurer.measure(item.word, style = wordStyle, maxLines = 1, softWrap = false).size.width)
            val detailWidth = measurer.measure(item.detail, style = detailStyle, maxLines = 1, softWrap = false).size.width
            wordWidth + detailGap.toPx() + detailWidth <= (maxWidth - RowInset * 2).toPx()
        }
        if (fits) ordinaryRow()
        else Column(rowModifier, verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
            word(Modifier.widthIn(min = wordMinimum))
            Text(item.detail, style = detailStyle, color = palette.ink2, maxLines = Int.MAX_VALUE, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** The rows' and section titles' side inset inside the frame. */
private val RowInset = 14.dp
