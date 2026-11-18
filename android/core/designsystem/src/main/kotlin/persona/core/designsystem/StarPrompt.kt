package persona.core.designsystem

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.core.content.edit
import androidx.core.net.toUri
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import persona.core.domain.PERSONA_REPO_URL
import persona.core.domain.STAR_PROMPT_THRESHOLD
import persona.core.domain.StarPromptEvent
import persona.core.domain.StarPromptState
import persona.core.domain.starPromptAfter

/**
 * This phone's GitHub star prompts (#5761, docs/design/DESIGN_SYSTEM.md "GitHub star prompts"; the rules are core:domain's
 * `StarPrompt.kt`). Device-wide like the appearance ([AppStyleStore]): not tied to a hub, never cleared on unpairing, so
 * a re-pairing asks nothing again. One per process (the app's), so every page reads the same [state].
 */
class StarPromptStore(context: Context, private val clock: () -> Long = System::currentTimeMillis) {
    private val preferences = context.applicationContext.getSharedPreferences(NAME, Context.MODE_PRIVATE)
    private val current = MutableStateFlow(read())
    val state: StateFlow<StarPromptState> = current.asStateFlow()
    private val owed = MutableStateFlow(false)
    /**
     * The onboarding toast is owed: setup just finished ([onboarded]) and the app shell's toast host has not shown it
     * yet. Kept here, not in the checklist, so it outlives the checklist page that finished setup.
     */
    val welcome: StateFlow<Boolean> = owed.asStateFlow()

    /** The hub took an Agent this phone started (not a resume). */
    fun launched() { apply(StarPromptEvent.Launch) }

    /** Later (or ✕) on the card. */
    fun later() { apply(StarPromptEvent.Later) }

    /** The pill's ✕. */
    fun hidePill() { apply(StarPromptEvent.HidePill) }

    /** Star on GitHub was tapped; [star] also opens the page. */
    fun starred() { apply(StarPromptEvent.Star) }

    /** First setup finished: true when the toast shows, once per phone and never after a star; it is then [welcome]. */
    fun onboarded(): Boolean = apply(StarPromptEvent.Onboarded).also { if (it) owed.value = true }

    /** The app shell showed the onboarding toast. */
    fun welcomeShown() { owed.value = false }

    @Synchronized
    private fun apply(event: StarPromptEvent): Boolean {
        val step = starPromptAfter(current.value, event, clock())
        write(step.state)
        current.value = step.state
        return step.toast
    }

    private fun read(): StarPromptState {
        val launches = preferences.getInt(KEY_LAUNCHES, 0).coerceAtLeast(0)
        val threshold = preferences.getInt(KEY_THRESHOLD, STAR_PROMPT_THRESHOLD).takeIf { it > 0 } ?: STAR_PROMPT_THRESHOLD
        return StarPromptState(
            launches = launches,
            threshold = threshold,
            snoozedUntil = if (preferences.contains(KEY_SNOOZED_UNTIL)) preferences.getLong(KEY_SNOOZED_UNTIL, 0) else null,
            done = preferences.getBoolean(KEY_DONE, false),
            pillHidden = preferences.getBoolean(KEY_PILL_HIDDEN, false),
            welcomed = preferences.getBoolean(KEY_WELCOMED, false),
        )
    }

    private fun write(s: StarPromptState) {
        preferences.edit {
            putInt(KEY_LAUNCHES, s.launches)
            putInt(KEY_THRESHOLD, s.threshold)
            s.snoozedUntil?.let { putLong(KEY_SNOOZED_UNTIL, it) } ?: remove(KEY_SNOOZED_UNTIL)
            putBoolean(KEY_DONE, s.done)
            putBoolean(KEY_PILL_HIDDEN, s.pillHidden)
            putBoolean(KEY_WELCOMED, s.welcomed)
        }
    }

    companion object {
        /** Its own preferences file: not `settings-readiness`, which unpairing clears. */
        const val NAME = "persona-star-prompt"
        private const val KEY_LAUNCHES = "launches"
        private const val KEY_THRESHOLD = "threshold"
        private const val KEY_SNOOZED_UNTIL = "snoozed-until"
        private const val KEY_DONE = "done"
        private const val KEY_PILL_HIDDEN = "pill-hidden"
        private const val KEY_WELCOMED = "welcomed"
    }
}

/**
 * Star on GitHub: the prompts stop on this phone and the project opens in the browser. Never the in-app link sheet
 * (`ChatLinkHost`'s), which has no GitHub session to star with.
 */
fun StarPromptStore.star(context: Context) {
    starred()
    openStarPage(context)
}

/** Opens [PERSONA_REPO_URL] in the browser; false when nothing can open it. */
fun openStarPage(context: Context): Boolean = runCatching {
    val intent = Intent(Intent.ACTION_VIEW, PERSONA_REPO_URL.toUri()).addCategory(Intent.CATEGORY_BROWSABLE)
    if (context.activity() == null) intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    context.startActivity(intent)
}.isSuccess

private fun Context.activity(): Activity? {
    var context: Context? = this
    while (context is ContextWrapper) {
        if (context is Activity) return context
        context = context.baseContext
    }
    return null
}

/** The app's [StarPromptStore]; null (previews, tests, the capsule) shows no prompt. */
val LocalStarPrompt = staticCompositionLocalOf<StarPromptStore?> { null }
