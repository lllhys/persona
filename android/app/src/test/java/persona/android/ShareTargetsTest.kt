package persona.android

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.SessionKey

/** The share sheet's recent targets (#2395): kept, put in front and preselected. */
class ShareTargetsTest {
    private val a = SessionKey("machine-a", "session-a")
    private val b = SessionKey("machine-b", "session-b")
    private val c = SessionKey("machine-a", "session-c")
    private val d = SessionKey("machine-b", "session-d")
    private val hour = 60 * 60 * 1000L
    private val now = 1_000 * hour

    @Test fun aTargetGoesInFrontOnceAndOnlyTheLatestAreKept() {
        var targets = emptyList<RecentShareTarget>()
        targets = rememberShareTarget(targets, a, 1)
        targets = rememberShareTarget(targets, null, 2)
        targets = rememberShareTarget(targets, a, 3)
        assertEquals(listOf(RecentShareTarget(a, 3), RecentShareTarget(null, 2)), targets)
        repeat(RECENT_SHARE_KEPT + 2) { targets = rememberShareTarget(targets, SessionKey("m", "s$it"), 10L + it) }
        assertEquals(RECENT_SHARE_KEPT, targets.size)
        assertEquals(SessionKey("m", "s${RECENT_SHARE_KEPT + 1}"), targets.first().key)
    }

    @Test fun theLatestListedTargetsComeFirstAndTheRestKeepTheirOrder() {
        val rows = listOf(a, b, c, d, SessionKey("m", "e"))
        // The main conversation (its own row) and a session no longer listed take no place; a, the fourth, keeps its own.
        val targets = listOf(RecentShareTarget(d, 9), RecentShareTarget(null, 8), RecentShareTarget(SessionKey("gone", "x"), 7),
            RecentShareTarget(c, 6), RecentShareTarget(SessionKey("m", "e"), 5), RecentShareTarget(a, 4))
        assertEquals(listOf(d, c, SessionKey("m", "e"), a, b), recentFirst(rows, { it }, targets))
        assertEquals(rows, recentFirst(rows, { it }, emptyList()))
    }

    @Test fun theSheetStartsOnTheLastTargetOnlyWithinADayAndWhileListed() {
        val listed = setOf(a, b)
        assertEquals(a, preselectedShareTarget(listOf(RecentShareTarget(a, now - 23 * hour), RecentShareTarget(b, now)), listed, now))
        assertNull("over a day ago", preselectedShareTarget(listOf(RecentShareTarget(a, now - 25 * hour)), listed, now))
        assertNull("not running any more", preselectedShareTarget(listOf(RecentShareTarget(c, now - hour)), listed, now))
        assertNull("the main conversation", preselectedShareTarget(listOf(RecentShareTarget(null, now - hour), RecentShareTarget(a, now - 2 * hour)), listed, now))
        assertNull("a clock set back", preselectedShareTarget(listOf(RecentShareTarget(a, now + hour)), listed, now))
        assertNull(preselectedShareTarget(emptyList(), listed, now))
    }

    @Test fun targetsReadBackAsWrittenAndBadLinesAreSkipped() {
        val targets = listOf(RecentShareTarget(a, 5), RecentShareTarget(null, 4), RecentShareTarget(SessionKey("m#g2", "s/1"), 3))
        assertEquals(targets, decodeShareTargets(encodeShareTargets(targets)))
        assertEquals(listOf(RecentShareTarget(a, 5)),
            decodeShareTargets("x\tmachine-a\tsession-a\n5\tmachine-a\tsession-a\n6\tmachine-a\t\n7\tonly-two"))
        assertEquals(emptyList<RecentShareTarget>(), decodeShareTargets(null))
    }

    @Test fun theStoreKeepsTheLatestFirst() = runTest {
        val store = ShareTargetStore(MemoryPreferences())
        store.remember(a, 1)
        store.remember(null, 2)
        store.remember(b, 3)
        assertEquals(listOf(RecentShareTarget(b, 3), RecentShareTarget(null, 2), RecentShareTarget(a, 1)), store.read())
    }

    private class MemoryPreferences : DataStore<Preferences> {
        private val state = MutableStateFlow(emptyPreferences())
        override val data = state
        override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
            transform(state.value).also { state.value = it }
    }
}
