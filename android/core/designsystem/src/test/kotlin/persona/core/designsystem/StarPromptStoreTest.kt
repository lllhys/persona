package persona.core.designsystem

import android.content.Context
import android.content.Intent
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import persona.core.domain.PERSONA_REPO_URL
import persona.core.domain.STAR_PROMPT_WAIT_MS
import persona.core.domain.StarPromptState
import persona.core.domain.showsStarCard
import persona.core.domain.showsStarPill

/** The GitHub star prompts' state on this phone (#5761): kept across launches in its own preferences, as the rules move it. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class StarPromptStoreTest {
    private val context: Context = ApplicationProvider.getApplicationContext()
    private var now = 1_798_761_600_000L

    @After
    fun reset() {
        context.getSharedPreferences(StarPromptStore.NAME, Context.MODE_PRIVATE).edit().clear().commit()
    }

    @Test
    fun aFreshPhoneStartsWithThePillOnly() {
        val store = StarPromptStore(context) { now }
        assertEquals(StarPromptState(), store.state.value)
        assertTrue(showsStarPill(store.state.value))
        assertFalse(showsStarCard(store.state.value, now))
    }

    @Test
    fun theStateRoundTripsThroughAnotherStore() {
        val store = StarPromptStore(context) { now }
        repeat(20) { store.launched() }
        assertTrue("the 20th launch brings the card", showsStarCard(store.state.value, now))
        store.later()
        store.hidePill()
        val expected = StarPromptState(launches = 20, threshold = 40, snoozedUntil = now + STAR_PROMPT_WAIT_MS, pillHidden = true)
        assertEquals(expected, store.state.value)
        assertEquals("read back as written", expected, StarPromptStore(context) { now }.state.value)

        store.starred()
        val reopened = StarPromptStore(context) { now }
        assertTrue(reopened.state.value.done)
        assertEquals(now + STAR_PROMPT_WAIT_MS, reopened.state.value.snoozedUntil)
    }

    @Test
    fun theOnboardingToastIsOwedOncePerPhone() {
        val store = StarPromptStore(context) { now }
        assertFalse(store.welcome.value)
        assertTrue(store.onboarded())
        assertTrue("owed until the shell shows it", store.welcome.value)
        store.welcomeShown()
        assertFalse(store.welcome.value)

        val again = StarPromptStore(context) { now }
        assertFalse("a second setup (a re-pairing) shows nothing", again.onboarded())
        assertFalse(again.welcome.value)
    }

    @Test
    fun aDamagedRecordFallsBackToTheStart() {
        context.getSharedPreferences(StarPromptStore.NAME, Context.MODE_PRIVATE).edit()
            .putInt("launches", -3).putInt("threshold", 0).commit()
        assertEquals(StarPromptState(), StarPromptStore(context) { now }.state.value)
    }

    @Test
    fun starOpensTheProjectInTheBrowserAndEndsThePrompts() {
        val store = StarPromptStore(context) { now }
        store.star(context)
        assertTrue(store.state.value.done)
        val opened = shadowOf(ApplicationProvider.getApplicationContext<android.app.Application>()).nextStartedActivity
        assertEquals(Intent.ACTION_VIEW, opened.action)
        assertEquals(PERSONA_REPO_URL, opened.dataString)
        assertTrue(opened.hasCategory(Intent.CATEGORY_BROWSABLE))
        assertTrue("an application context starts it in a task of its own", opened.flags and Intent.FLAG_ACTIVITY_NEW_TASK != 0)
    }
}
