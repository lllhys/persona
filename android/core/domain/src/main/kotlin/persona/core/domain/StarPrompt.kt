package persona.core.domain

// Asking for a GitHub star in three quiet places (#5761, docs/design/DESIGN_SYSTEM.md "GitHub star prompts"): a pill on
// the empty Agents page, a card in the Agents list's notice slot once this device has started enough Agents, and a toast
// when first setup finishes. The rules are spec/fixtures/star-prompt's, which the web replays too (iOS: #5910); the
// state is this device's and never goes to the hub. Whether the user really starred can't be known: a tap on Star counts.

/** The project's page, where Star on GitHub goes (in the browser, where the user's GitHub session is). */
const val PERSONA_REPO_URL = "https://github.com/nwverse/persona"

/** The card's first threshold: the Agents this device starts before it shows. */
const val STAR_PROMPT_THRESHOLD = 20

/** How long Later holds the card back: three days. */
const val STAR_PROMPT_WAIT_MS = 3L * 24 * 60 * 60 * 1000

data class StarPromptState(
    /** Agents this device started. */
    val launches: Int = 0,
    /** The launches the card waits for; Later doubles it. */
    val threshold: Int = STAR_PROMPT_THRESHOLD,
    /** Unix ms before which the card stays away (Later's three days), or null. */
    val snoozedUntil: Long? = null,
    /** Star on GitHub was tapped: every prompt stops. */
    val done: Boolean = false,
    val pillHidden: Boolean = false,
    /** The onboarding toast was shown. */
    val welcomed: Boolean = false,
)

/** What happens to the prompts; [key] is the fixtures' name for it. */
enum class StarPromptEvent(val key: String) {
    /** The hub took an Agent this device started (not a resume). */
    Launch("launch"),
    /** Later (or ✕) on the card. */
    Later("later"),
    /** Star on GitHub, from any prompt. */
    Star("star"),
    /** The pill's ✕. */
    HidePill("hidePill"),
    /** First setup finished. */
    Onboarded("onboarded"),
}

/** The state after an event, and whether it showed the onboarding toast. */
data class StarPromptStep(val state: StarPromptState, val toast: Boolean = false)

/** The state after [event] at [now] (Unix ms), and whether it showed the onboarding toast. */
fun starPromptAfter(s: StarPromptState, event: StarPromptEvent, now: Long): StarPromptStep = when (event) {
    StarPromptEvent.Launch -> StarPromptStep(s.copy(launches = s.launches + 1))
    StarPromptEvent.Later -> {
        // Doubled at least once, then until it is past the launches so far (a card first seen long after its threshold).
        var threshold = maxOf(1, s.threshold)
        do threshold = if (threshold > Int.MAX_VALUE / 2) Int.MAX_VALUE else threshold * 2
        while (threshold <= s.launches && threshold < Int.MAX_VALUE)
        StarPromptStep(s.copy(threshold = threshold, snoozedUntil = now + STAR_PROMPT_WAIT_MS))
    }
    StarPromptEvent.Star -> StarPromptStep(s.copy(done = true))
    StarPromptEvent.HidePill -> StarPromptStep(s.copy(pillHidden = true))
    StarPromptEvent.Onboarded -> StarPromptStep(s.copy(welcomed = true), toast = !s.done && !s.welcomed)
}

/** The empty Agents page's pill: until it is hidden or the user starred. */
fun showsStarPill(s: StarPromptState): Boolean = !s.done && !s.pillHidden

/** The Agents list's card at [now] (Unix ms): from the threshold, outside Later's wait, never after a star. */
fun showsStarCard(s: StarPromptState, now: Long): Boolean =
    !s.done && s.launches >= s.threshold && (s.snoozedUntil == null || now >= s.snoozedUntil)
