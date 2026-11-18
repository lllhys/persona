package persona.feature.chat

import android.content.res.Resources
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.contracts.Item
import persona.core.contracts.ModelRefusal
import persona.core.contracts.Session
import persona.core.contracts.parseTime
import persona.core.contracts.api.ChatActions
import persona.core.contracts.api.hubCodedFailure
import persona.core.designsystem.*
import persona.core.designsystem.dense
import persona.core.designsystem.icons.PersonaIcons
import java.time.Instant
import java.time.ZoneId

// A refusal fallback (#2289, #2528; web lib/model-fallback.ts): the upstream refused a request and Claude Code answered
// it with another model on its own, keeping the session there. The transcript shows the refusal as a line of its own;
// above the composer a card names both models and offers the ways back. "Back to the configured model" closes a Persona
// conversation's idle headless process so its next start uses the configured model; it sends nothing, and whether that
// model then runs is only known from a newer reply, so nothing here says it was restored.

/** The turn_error_code of a turn that failed on the missing fallback model: the user's to act on, never "Continue". */
internal const val FALLBACK_UNAVAILABLE = "model_fallback_unavailable"

/**
 * "New main chat" for the fallback card: the assistant page provides it around the main conversation when the hub can
 * make one (its own dialog), and nothing anywhere else.
 */
val LocalNewMainChat = compositionLocalOf<(() -> Unit)?> { null }

/**
 * What the card shows: the models as recorded, the error since, and a pending restore's configured model. A switch the
 * node's own recovery made (#7824) also carries [cause], one of [recoveryCause]'s categories, and [returning]: the
 * configured model answered a probe, so the next message runs on it again. A runtime-written record has neither.
 */
internal data class FallbackView(
    val original: String, val fallback: String, val error: String, val pending: Boolean, val target: String,
    val cause: String? = null, val returning: Boolean = false,
)

/**
 * Why the node's own recovery left the configured model (#7824, server `proto.ModelRecoveryReason*`), in plain words;
 * null for any other category, such as the runtime's own refusal categories, which keep their words as recorded.
 */
internal fun recoveryCause(category: String?): Int? = when (category) {
    "safeguards_refusal" -> R.string.chat_model_fallback_cause_safeguards_refusal
    "quota_exhausted" -> R.string.chat_model_fallback_cause_quota_exhausted
    "rate_limited" -> R.string.chat_model_fallback_cause_rate_limited
    "server_error" -> R.string.chat_model_fallback_cause_server_error
    "model_unavailable" -> R.string.chat_model_fallback_cause_model_unavailable
    else -> null
}

/**
 * When the configured model is available again, in the phone's zone, as iOS `Session.usageLimitClock` and the web's
 * `usageLimitClock` say a usage limit's reset: the clock when that is today, the date as well on another day. Null
 * without a readable time, where the failure card offers Retry with no time.
 */
internal fun recoveryResetText(reset: String?, resources: Resources, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String? {
    val at = parseTime(reset) ?: return null
    val today = at.atZone(zone).toLocalDate() == now.atZone(zone).toLocalDate()
    val clock = LocalizedFormats.format(at, if (today) LocalizedFormats.TIME else LocalizedFormats.MONTH_DAY_TIME,
        resources.configuration.locales[0], zone)
    return resources.getString(R.string.chat_model_fallback_recovery_reset, clock)
}

/**
 * What an exhausted recovery chain's failure card says above Retry (#7824): the cause in plain words and, when the
 * upstream named one, when the configured model is available again. The models tried fold with the card's details.
 */
internal fun recoveryExhaustedLines(refusal: ModelRefusal, resources: Resources, now: Instant = Instant.now(),
                                    zone: ZoneId = ZoneId.systemDefault()): List<String> = listOfNotNull(
    recoveryCause(refusal.category)?.let { resources.getString(R.string.chat_model_fallback_recovery_cause, resources.getString(it)) },
    recoveryResetText(refusal.recoveryReset, resources, now, zone),
)

/** The card for [session], or null when it has no active refusal fallback. */
internal fun fallbackView(session: Session): FallbackView? {
    val f = session.modelFallback ?: return null
    if (f.subtype != "model_refusal_fallback" || f.fallbackModel.isNullOrEmpty()) return null
    val pending = session.modelRestore?.state == "next_start"
    return FallbackView(f.originalModel.orEmpty(), f.fallbackModel!!, session.turnError.orEmpty(), pending,
        if (pending) session.modelRestore?.to.orEmpty() else "", f.category?.takeIf { recoveryCause(it) != null }, f.recoveryReturning)
}

/** A refused restore in the app's language by its code (web `restoreErrorText`); otherwise the usual failure words. */
internal fun restoreErrorText(resources: Resources, error: Throwable): String =
    when (hubCodedFailure(error)?.code) {
        "restore_busy" -> resources.getString(R.string.chat_model_fallback_err_busy)
        "restore_unsupported" -> resources.getString(R.string.chat_model_fallback_err_unsupported)
        "restore_nothing" -> resources.getString(R.string.chat_model_fallback_err_nothing)
        "restore_unknown" -> resources.getString(R.string.chat_model_fallback_err_unknown)
        "node_outdated" -> resources.getString(R.string.chat_model_fallback_err_outdated)
        else -> operationFeedback(error).text.resolve(resources)
    }

/**
 * Above the composer while [session] runs on the model Claude Code fell back to (web ModelFallbackCard). A Persona
 * conversation ([Session.assistant], headless) offers "Back to configured model" and, in the main conversation,
 * [LocalNewMainChat]; a terminal session gets the manual `/model` step only. While a restore waits for a newer reply
 * the card says only that the next start will use the configured model.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ModelFallbackCard(session: Session, actions: ChatActions?, modifier: Modifier = Modifier) {
    val view = fallbackView(session) ?: return
    val resources = LocalResources.current
    val scope = rememberCoroutineScope()
    val newMain = LocalNewMainChat.current.takeIf { session.assistant }
    var busy by remember(session.key) { mutableStateOf(false) }
    var error by remember(session.key) { mutableStateOf<String?>(null) }
    val headless = session.assistant
    val canRestore = headless && !view.pending && actions != null
    val palette = PersonaTheme.palette
    Column(
        modifier.fillMaxWidth().padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.xs)
            .clip(PersonaTheme.shapes.controlShape).background(palette.statusSurface)
            .padding(horizontal = 12.dp.dense, vertical = 8.dp.dense).testTag("model-fallback"),
        verticalArrangement = Arrangement.spacedBy(8.dp.dense),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp.dense), verticalAlignment = Alignment.Top) {
            JIcon(PersonaIcons.error, null, size = 16.dp, tint = palette.caution, modifier = Modifier.padding(top = 1.dp.dense))
            Column(Modifier.weight(1f).semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
                verticalArrangement = Arrangement.spacedBy(2.dp.dense)) {
                val line = PersonaTheme.type.caption
                Text(stringResource(if (view.pending) R.string.chat_model_fallback_pending_title else R.string.chat_model_fallback_title),
                    style = line.copy(fontWeight = FontWeight.SemiBold), color = palette.ink)
                if (view.original.isNotEmpty()) Text(stringResource(R.string.chat_model_fallback_original, view.original), style = line, color = palette.ink2)
                // The node's own switch names its cause (#7824); a runtime-written record keeps its words.
                val cause = recoveryCause(view.cause)?.let { stringResource(it) }
                Text(if (cause != null) stringResource(R.string.chat_model_fallback_switched_cause, view.fallback, cause)
                    else stringResource(R.string.chat_model_fallback_fallback, view.fallback), style = line, color = palette.ink2)
                // The configured model answered a probe: the next message switches back without anything to tap (#7824).
                if (view.returning && view.original.isNotEmpty())
                    Text(stringResource(R.string.chat_model_fallback_recovery_returning, view.original), style = line, color = palette.ink)
                if (view.pending && view.target.isNotEmpty())
                    Text(stringResource(R.string.chat_model_fallback_pending_target, view.target), style = line, color = palette.ink)
                if (view.error.isNotEmpty()) Text(stringResource(R.string.chat_model_fallback_error_after, view.error), style = line, color = palette.ink2)
                if (view.pending) Text(stringResource(R.string.chat_model_fallback_unverified), style = line, color = palette.ink2)
                Text(stringResource(if (headless) R.string.chat_model_fallback_no_promise else R.string.chat_model_fallback_terminal_hint),
                    style = line, color = palette.ink3)
                error?.let { Text(it, style = line, color = palette.danger.text) }
            }
        }
        if (canRestore || newMain != null) FlowRow(Modifier.padding(start = 24.dp.dense), horizontalArrangement = Arrangement.spacedBy(8.dp.dense),
            verticalArrangement = Arrangement.spacedBy(8.dp.dense)) {
            if (canRestore) JButton(stringResource(R.string.chat_model_fallback_restore), {
                if (busy) return@JButton
                busy = true
                error = null
                scope.launch {
                    // the session's next push carries model_restore; nothing is claimed before it
                    try { actions!!.restoreModel(session) }
                    catch (e: CancellationException) { throw e }
                    catch (e: Exception) { error = restoreErrorText(resources, e) }
                    finally { busy = false }
                }
            }, kind = JButtonKind.Primary, size = JButtonSize.Small, enabled = session.status != "working" && !busy, loading = busy)
            if (newMain != null) JButton(stringResource(R.string.chat_model_fallback_new_main), newMain,
                kind = JButtonKind.Secondary, size = JButtonSize.Small)
        }
    }
}

/**
 * A Claude Code model_refusal_* record in the transcript (web RefusalNotice): the refused model and the one Claude Code
 * switched to, the category and scope as recorded, and the refusal's own words verbatim. Never the record's line.
 */
@Composable
internal fun RefusalNotice(refusal: ModelRefusal) {
    val palette = PersonaTheme.palette
    val line = PersonaTheme.type.caption
    val fallback = refusal.subtype == "model_refusal_fallback" && !refusal.fallbackModel.isNullOrEmpty()
    Row(
        Modifier.fillMaxWidth().padding(vertical = 4.dp.dense).clip(PersonaTheme.shapes.controlShape).background(palette.statusSurface)
            .padding(horizontal = 12.dp.dense, vertical = 8.dp.dense).semantics(mergeDescendants = true) {}.testTag("refusal-notice"),
        horizontalArrangement = Arrangement.spacedBy(8.dp.dense), verticalAlignment = Alignment.Top,
    ) {
        JIcon(PersonaIcons.error, null, size = 16.dp, tint = palette.caution, modifier = Modifier.padding(top = 1.dp.dense))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp.dense)) {
            Text(stringResource(R.string.chat_model_fallback_refusal_title), style = line.copy(fontWeight = FontWeight.SemiBold), color = palette.ink)
            Text(if (fallback) stringResource(R.string.chat_model_fallback_refusal_fallback, refusal.originalModel?.ifEmpty { null } ?: "?", refusal.fallbackModel!!)
                else stringResource(R.string.chat_model_fallback_refusal_no_switch), style = line, color = palette.ink2)
            if (fallback && refusal.scope == "session") Text(stringResource(R.string.chat_model_fallback_refusal_session), style = line, color = palette.ink2)
            refusal.category?.takeIf(String::isNotEmpty)?.let { Text(stringResource(R.string.chat_model_fallback_refusal_category, it), style = line, color = palette.ink2) }
            refusal.explanation?.takeIf(String::isNotEmpty)?.let { Text(stringResource(R.string.chat_model_fallback_refusal_explanation, it), style = line, color = palette.ink2) }
            refusal.text?.takeIf(String::isNotBlank)?.let {
                Text(stringResource(R.string.chat_model_fallback_refusal_text), style = line, color = palette.ink3)
                Text(it, style = line, color = palette.ink2)
            }
        }
    }
}

/** The refusal record of a transcript notice, when it is one. */
internal val Item.refusal: ModelRefusal? get() = modelRefusal?.takeIf { kind == "notice" }
