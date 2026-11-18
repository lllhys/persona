package persona.core.designsystem

import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons

// Parts of a conversation beyond the bubbles in Chat.kt: pictures, the photos sent with a message, the API error card.

/** How tall a picture in a conversation gets at most: a tall screenshot would otherwise fill several screens. */
val ChatPictureMaxHeight: Dp = 280.dp

/**
 * The frame of a picture in a conversation (iOS `Base64Image`): it holds the picture's [ratio] (width / height, known
 * from the page) while the picture loads, so the list does not jump; at most [ChatPictureMaxHeight] tall and as wide as
 * that allows. [onClick] opens it (the image viewer). [failed] shows "Failed to load, tap to retry" and a tap calls [onRetry].
 * [alignEnd] puts a narrow picture on the right (the user's side).
 */
@Composable
fun ChatPictureFrame(
    ratio: Float?,
    failed: Boolean,
    onClick: (() -> Unit)?,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
    description: String = stringResource(R.string.designsystem_image),
    alignEnd: Boolean = false,
    content: @Composable BoxScope.() -> Unit,
) {
    val palette = PersonaTheme.palette
    val aspect = ratio?.takeIf { it.isFinite() && it > 0f } ?: 1f
    BoxWithConstraints(modifier.fillMaxWidth(), contentAlignment = if (alignEnd) Alignment.TopEnd else Alignment.TopStart) {
        val width = minOf(maxWidth, ChatPictureMaxHeight * aspect)
        val click = when {
            failed -> Modifier.clickable(role = Role.Button, onClickLabel = stringResource(R.string.designsystem_retry), onClick = onRetry)
            onClick != null -> Modifier.clickable(role = Role.Image, onClickLabel = stringResource(R.string.designsystem_view_full), onClick = onClick)
            else -> Modifier
        }
        val spoken = if (failed) stringResource(R.string.designsystem_with_state, description, stringResource(R.string.designsystem_load_failed)) else description
        Box(
            Modifier.width(width).aspectRatio(aspect).clip(PersonaTheme.shapes.controlShape).background(palette.sunken)
                .then(click).semantics { contentDescription = spoken },
        ) {
            content()
            if (failed) Column(
                Modifier.align(Alignment.Center).padding(12.dp.dense),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                JIcon(PhIcons.ArrowClockwise, null, size = 20.dp, tint = palette.ink2)
                Spacer(Modifier.height(4.dp.dense))
                Text(stringResource(R.string.designsystem_load_failed_tap), style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 2,
                    textAlign = TextAlign.Center, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

private val SinglePhoto = 120.dp
private val GroupPhoto = 76.dp

/**
 * A photo sent with a message, as a square thumbnail above its text (iOS `AttachedImages`): [single] (the only one) is
 * larger. A spinner while [image] is null; [failed] shows a retry mark and a tap calls [onRetry]; otherwise a tap
 * calls [onClick] (the image viewer).
 */
@Composable
fun PhotoThumbnail(
    image: ImageBitmap?,
    failed: Boolean,
    single: Boolean,
    onClick: () -> Unit,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
    description: String = stringResource(R.string.designsystem_photo),
) {
    val spoken = if (failed) stringResource(R.string.designsystem_with_state, description, stringResource(R.string.designsystem_load_failed)) else description
    val palette = PersonaTheme.palette
    // The photos sit in the user's bubble; brief's wells are that bubble's own colour (sunken == surface), so an empty
    // box (loading, failed) would not show there: it takes the stronger fill instead.
    val well = if (palette.sunken == palette.surface) palette.surfaceStrong else palette.sunken
    Box(
        modifier.size(if (single) SinglePhoto else GroupPhoto).clip(PersonaTheme.shapes.controlShape).background(well)
            .clickable(role = Role.Button, onClickLabel = stringResource(if (failed) R.string.designsystem_retry else R.string.designsystem_view_full), onClick = if (failed) onRetry else onClick)
            .semantics { contentDescription = spoken },
        contentAlignment = Alignment.Center,
    ) {
        when {
            image != null -> Image(image, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            failed -> Column(horizontalAlignment = Alignment.CenterHorizontally) {
                JIcon(PhIcons.ArrowClockwise, null, size = 18.dp, tint = palette.ink2)
                if (single) {
                    Spacer(Modifier.height(4.dp.dense))
                    Text(stringResource(R.string.designsystem_load_failed), style = PersonaTheme.type.micro, color = palette.ink2, maxLines = 1)
                }
            }
            else -> JSpinner(Modifier.size(18.dp))
        }
    }
}

/**
 * The photos of one message in a row, wrapping when they do not fit; as wide as they are, so a bubble with one photo
 * and a few words stays small. [alignEnd] lines wrapped rows up on the right (the user's side).
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun PhotoRow(modifier: Modifier = Modifier, alignEnd: Boolean = true, content: @Composable () -> Unit) {
    FlowRow(
        modifier,
        horizontalArrangement = Arrangement.spacedBy(8.dp.dense, if (alignEnd) Alignment.End else Alignment.Start),
        verticalArrangement = Arrangement.spacedBy(8.dp.dense),
    ) { content() }
}

/** How an [ErrorTag] reads: plain, the error's red, or caution (someone has to act). */
enum class ErrorTagTone { Plain, Danger, Caution }

/** One fact under an error card's reason: the status code, "Temporary" / "Needs action", "N times in a row". */
@Immutable
data class ErrorTag(val text: String, val tone: ErrorTagTone = ErrorTagTone.Plain)

/**
 * A model API error in a conversation (iOS `APIErrorCard`): a warning mark and [reason], [tags] under it, a tap on the
 * header shows or hides [details] (ids, time, the raw text), [action] ("Retry") at the end. A neutral panel with an error icon/edge in Glass and
 * Brief, a neutral strip with a thin red edge on Board.
 */
@Composable
fun ErrorCard(
    reason: String,
    tags: List<ErrorTag>,
    expanded: Boolean,
    onToggle: () -> Unit,
    modifier: Modifier = Modifier,
    action: (@Composable () -> Unit)? = null,
    details: @Composable ColumnScope.() -> Unit,
) {
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val shape = PersonaTheme.shapes.cardShape
    val frame = when (style) {
        AppStyle.Glass -> Modifier.clip(shape).background(palette.statusSurface).border(1.dp, palette.danger.dot.copy(alpha = 0.35f), shape)
        AppStyle.Brief -> Modifier.clip(shape).background(palette.statusSurface)
        AppStyle.Board -> Modifier.clip(shape).background(palette.surface).holder(palette.danger.dot, Tokens.Space.xxs).border(1.dp, palette.line, shape)
    }
    val inner = if (style == AppStyle.Board) PaddingValues(start = 20.dp.dense, end = 14.dp.dense, top = 12.dp.dense, bottom = 12.dp.dense)
        else PaddingValues(horizontal = 16.dp.dense, vertical = 14.dp.dense)
    val spoken = stringResource(R.string.designsystem_api_error, reason)
    val detailGap = 10.dp.dense
    // Raw details fade in place (APIErrorCard.swift:118 and :189, ChatPin.swift:707-726). The action stays outside the fade.
    val edge = rememberOpenEdge()
    val revealing = edge.revealing(expanded)
    // A details block with nothing in it takes no gap, so the action stays where it was.
    ErrorStack(modifier.fillMaxWidth().padding(vertical = 6.dp.dense).then(frame).padding(inner), detailGap) {
        Column(
            Modifier.fillMaxWidth().clickable(role = Role.Button, onClickLabel = stringResource(if (expanded) R.string.designsystem_details_collapse else R.string.designsystem_details_expand), onClick = onToggle)
                .semantics { contentDescription = spoken },
            verticalArrangement = Arrangement.spacedBy(8.dp.dense),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                JIcon(PhIcons.WarningCircle, null, size = 20.dp, tint = palette.danger.dot, selected = true)
                Spacer(Modifier.width(10.dp.dense))
                Text(reason, Modifier.weight(1f), style = PersonaTheme.type.bodyStrong, color = palette.ink)
                Spacer(Modifier.width(6.dp.dense))
                JIcon(if (expanded) PhIcons.CaretUp else PhIcons.CaretDown, null, size = 14.dp, tint = palette.ink3)
            }
            if (tags.isNotEmpty()) Row(horizontalArrangement = Arrangement.spacedBy(6.dp.dense)) { tags.forEach { ErrorTagLabel(it) } }
        }
        if (expanded) Column(Modifier.unfolding(revealing), verticalArrangement = Arrangement.spacedBy(detailGap)) { details() }
        action?.invoke()
    }
}

/** The error card's stack: the same gap as before, and a child with no height (empty details) does not add one. */
@Composable
private fun ErrorStack(modifier: Modifier, gap: Dp, content: @Composable () -> Unit) {
    val gapPx = with(LocalDensity.current) { gap.roundToPx() }
    Layout(content, modifier) { measurables, constraints ->
        // Loose width, as the Column this replaced. A tight minWidth stretches Retry to the card.
        val placeables = measurables.map { it.measure(constraints.copy(minWidth = 0, minHeight = 0)) }
        val shown = placeables.filter { it.height > 0 }
        val height = shown.sumOf { it.height } + gapPx * (shown.size - 1).coerceAtLeast(0)
        layout(constraints.maxWidth, height) {
            var y = 0
            placeables.forEach { child ->
                if (child.height == 0) return@forEach
                child.placeRelative(0, y)
                y += child.height + gapPx
            }
        }
    }
}

@Composable
private fun ErrorTagLabel(tag: ErrorTag) {
    val palette = PersonaTheme.palette
    val (ink, fill) = when (tag.tone) {
        ErrorTagTone.Plain -> palette.ink2 to palette.surfaceStrong
        ErrorTagTone.Danger -> palette.danger.text to palette.statusSurface
        ErrorTagTone.Caution -> palette.cautionText to palette.statusSurface
    }
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(if (PersonaTheme.style == AppStyle.Board) 4.dp else 6.dp.corner)
    Text(
        tag.text, style = PersonaTheme.type.micro.copy(fontWeight = FontWeight.SemiBold), color = ink, maxLines = 1,
        modifier = Modifier.clip(shape).background(fill).padding(horizontal = 6.dp.dense, vertical = 2.dp.dense),
    )
}

/** A named value inside a card's details ("Request ID", Session, "Time"): the name in a fixed column, [trailing] at the end. */
@Composable
fun DetailField(name: String, value: String, modifier: Modifier = Modifier, mono: Boolean = true, trailing: (@Composable () -> Unit)? = null) {
    val palette = PersonaTheme.palette
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(name, Modifier.width(72.dp), style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1)
        Text(value, Modifier.weight(1f), style = if (mono) PersonaTheme.type.code else PersonaTheme.type.caption, color = palette.ink,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
        trailing?.invoke()
    }
}

/** A quiet capsule for a note in the flow of a conversation ("While you were away · 21 minutes ago ›"): [onClick] opens what it stands for. */
@Composable
fun ChatNoteChip(text: String, icon: PhIcon, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(if (PersonaTheme.style == AppStyle.Board) 8.dp.corner else 50.dp)
    Row(
        modifier.padding(vertical = 4.dp.dense).heightIn(min = 36.dp.row).clip(shape)
            .background(if (PersonaTheme.style == AppStyle.Board) palette.surface else palette.surfaceStrong)
            .then(if (PersonaTheme.style == AppStyle.Board) Modifier.border(1.dp, palette.line, shape) else Modifier)
            .clickable(role = Role.Button, onClick = onClick).padding(horizontal = 12.dp.dense),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        JIcon(icon, null, size = 16.dp, tint = palette.accent)
        Spacer(Modifier.width(6.dp.dense))
        Text(text, style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f, fill = false))
        Spacer(Modifier.width(4.dp.dense))
        JIcon(PhIcons.CaretRight, null, size = 14.dp, tint = palette.ink3)
    }
}

/**
 * A line the chat shows in place of a message Persona typed in for the model (a task's notice, a scheduled run…;
 * docs/protocol/protocol.md "Messages injected by Persona"): centered, the look of [ChatNoteChip], an icon in [tint], one line. Tappable
 * (with a caret) only when [onClick] is set.
 */
@Composable
fun ChatHint(text: String, icon: PhIcon, tint: androidx.compose.ui.graphics.Color, onClick: (() -> Unit)? = null,
             modifier: Modifier = Modifier, onClickLabel: String? = null) {
    val palette = PersonaTheme.palette
    val board = PersonaTheme.style == AppStyle.Board
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(if (board) 8.dp.corner else 50.dp)
    Box(modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
        Row(
            Modifier.padding(vertical = 4.dp.dense).heightIn(min = 36.dp.row).clip(shape)
                .background(if (board) palette.surface else palette.surfaceStrong)
                .then(if (board) Modifier.border(1.dp, palette.line, shape) else Modifier)
                .then(if (onClick != null) Modifier.clickable(role = Role.Button, onClickLabel = onClickLabel, onClick = onClick) else Modifier)
                .padding(horizontal = 12.dp.dense),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            JIcon(icon, null, size = 16.dp, tint = tint)
            Spacer(Modifier.width(6.dp.dense))
            Text(text, style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false))
            if (onClick != null) {
                Spacer(Modifier.width(4.dp.dense))
                JIcon(PhIcons.CaretRight, null, size = 14.dp, tint = palette.ink3)
            }
        }
    }
}

/**
 * A file named in a message or a tool card, as a chip that opens it on the session's machine: the file's icon and its
 * name, one line.
 */
@Composable
fun PathChip(text: String, icon: PhIcon, onClick: () -> Unit, modifier: Modifier = Modifier, onClickLabel: String = stringResource(R.string.designsystem_view_file)) {
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(PersonaTheme.shapes.chip)
    Row(
        modifier.heightIn(min = 32.dp.row).clip(shape)
            .background(if (style == AppStyle.Board) palette.surface else palette.statusSurface)
            .then(if (style == AppStyle.Board) Modifier.border(1.dp, palette.line, shape) else Modifier)
            .clickable(role = Role.Button, onClickLabel = onClickLabel, onClick = onClick).padding(horizontal = 10.dp.dense),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        JIcon(icon, null, size = 14.dp, tint = palette.accent)
        Spacer(Modifier.width(5.dp.dense))
        Text(text, style = PersonaTheme.type.labelSmall, color = if (style == AppStyle.Brief) palette.ink else palette.accent,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/**
 * A one-line bar under a conversation's header for something the agent keeps working on (its goal, docs/integrations/agent-goal.md;
 * iOS `GoalBar`): an icon and [text] in [colors]' role colour, [trailing] (a status) at the end, a tap opens the detail.
 * Every style uses a neutral backing; the icon and trailing words carry its role color.
 */
@Composable
fun JActivityBar(
    text: String,
    icon: PhIcon,
    colors: StatusColors,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    trailing: String? = null,
    onClickLabel: String? = null,
) {
    val palette = PersonaTheme.palette
    val board = PersonaTheme.style == AppStyle.Board
    val shape = PersonaTheme.shapes.chipShape
    Row(
        modifier.fillMaxWidth().heightIn(min = 36.dp.row).clip(shape)
            .background(if (board) palette.surface else palette.statusSurface)
            .then(if (board) Modifier.border(1.dp, palette.line, shape) else Modifier)
            .clickable(role = Role.Button, onClickLabel = onClickLabel, onClick = onClick)
            .padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
    ) {
        JIcon(icon, null, size = 16.dp, tint = colors.text)
        Text(text, style = PersonaTheme.type.labelSmall.copy(fontWeight = FontWeight.SemiBold), color = palette.ink,
            maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        trailing?.let {
            Text(it, style = PersonaTheme.type.labelSmall.copy(fontWeight = FontWeight.SemiBold), color = colors.text, maxLines = 1, softWrap = false)
        }
    }
}
