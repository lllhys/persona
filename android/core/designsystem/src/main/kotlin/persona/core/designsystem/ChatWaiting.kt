package persona.core.designsystem

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.animateColor
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import persona.core.domain.tasks.AssistantLines
import java.time.LocalDate
import kotlin.math.PI
import kotlin.math.max
import kotlin.math.sin

/** One turn of [TypingLine]'s wave (iOS `TypingIndicator`'s 1.2 s, linear). */
private const val TypingWaveMillis = 1200

/** How far each of [TypingLine]'s dots trails the one before it, in radians of the wave. */
private const val TypingDotLag = 0.9f

/** [ChatSkeleton]'s fill going one way, eased (iOS `ChatSkeleton`'s 0.9 s, back and forth). */
private const val SkeletonPulseMillis = 900

/**
 * [ChatSkeleton]'s rows as iOS draws them: 220, 300, 180, 260 and 140 pt of a phone's 370 pt column, as fractions of the
 * width so a wider screen keeps their shape. The first stands where your message would, at the trailing edge.
 */
private val SkeletonRows = listOf(0.59f, 0.81f, 0.49f, 0.7f, 0.38f)

/**
 * The day over the first thing of each day in Persona's conversation (iOS `DayLine`): "Today · Mon, Nov 3", "Yesterday · …", or
 * the date (with its year when it is not this year). A caption in the quiet ink, centered; a heading for screen readers.
 */
@Composable
fun DayLine(date: LocalDate, modifier: Modifier = Modifier) {
    Text(AssistantLines.dayLabel(date).text(), modifier.fillMaxWidth().padding(vertical = Tokens.Space.xs).semantics { heading() },
        style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3, textAlign = TextAlign.Center)
}

/**
 * A line saying what the agent is about to do before anything of it shows (iOS `TypingIndicator`): "Claude is getting
 * ready…" under your message while its turn has not begun. Three dots pulse one after another, then the words; still when
 * motion is reduced.
 */
@Composable
fun TypingLine(text: String, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val dot = if (PersonaTheme.style == AppStyle.Brief) palette.ink else palette.accent
    val phase: State<Float> = if (PersonaTheme.reduceMotion) remember { mutableFloatStateOf(0f) }
        else rememberInfiniteTransition(label = "typing").animateFloat(0f, (2 * PI).toFloat(),
            infiniteRepeatable(tween(TypingWaveMillis, easing = LinearEasing)), label = "typing-wave")
    Row(modifier.fillMaxWidth().semantics(mergeDescendants = true) {}, verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        Row(Modifier.clearAndSetSemantics {}, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            repeat(3) { i ->
                // Read in the layer, so the wave redraws the dots and recomposes nothing.
                Box(Modifier.size(Tokens.Size.dotS)
                    .graphicsLayer { alpha = 0.35f + 0.65f * max(0f, sin(phase.value + i * TypingDotLag)) }
                    .clip(CircleShape).background(dot))
            }
        }
        Text(text, style = PersonaTheme.type.callout, color = palette.ink2)
    }
}

/**
 * Where a chat's lines will be while its first page loads (iOS `ChatSkeleton`): five rounded bars the height of a small
 * mark, the first at the trailing edge as your message, the rest at the leading edge as the agent's words. Their fill
 * breathes between the well and chip greys; still when motion is reduced. Read as loading. Glass draws it; Brief and Board
 * keep their spinner.
 */
@Composable
fun ChatSkeleton(modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val well = if (palette.dark) Tokens.Common.Dark.Fill.well else Tokens.Common.Light.Fill.well
    val chip = if (palette.dark) Tokens.Common.Dark.Fill.chip else Tokens.Common.Light.Fill.chip
    val fill: State<Color> = if (PersonaTheme.reduceMotion) remember(well) { mutableStateOf(well) }
        else rememberInfiniteTransition(label = "skeleton").animateColor(well, chip,
            infiniteRepeatable(tween(SkeletonPulseMillis), RepeatMode.Reverse), label = "skeleton-fill")
    val shape = RoundedCornerShape(Tokens.Radius.control)
    val loading = stringResource(R.string.designsystem_loading)
    Column(modifier.fillMaxWidth().clearAndSetSemantics { contentDescription = loading }, verticalArrangement = Arrangement.spacedBy(Tokens.Space.l)) {
        SkeletonRows.forEachIndexed { i, width ->
            Box(Modifier.fillMaxWidth(), contentAlignment = if (i == 0) Alignment.CenterEnd else Alignment.CenterStart) {
                Box(Modifier.fillMaxWidth(width).height(Tokens.Size.markS).clip(shape).drawBehind { drawRect(fill.value) })
            }
        }
    }
}
