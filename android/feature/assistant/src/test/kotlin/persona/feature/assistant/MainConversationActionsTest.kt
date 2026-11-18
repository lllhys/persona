package persona.feature.assistant

import persona.core.contracts.SessionCompaction
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.LocalDate
import java.util.Locale
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class MainConversationActionsTest {
    private val first = "2026-11-04T00:30:00Z"
    private val second = "2026-11-04T01:00:00Z"
    private fun running(at: String = first) = SessionCompaction("running", startedAt = at)
    private fun done(at: String = first) = SessionCompaction("done", startedAt = at, finishedAt = at)
    private fun failed(at: String = first, error: String = "Not enough messages to compact") =
        SessionCompaction("failed", error, startedAt = at, finishedAt = at)

    @Test fun theActionsFollowTheHubsFeatures() {
        assertEquals(MainActions(compact = true, newMain = true), MainActions.of(setOf("client_ops", CompactFeature, NewMainFeature)))
        assertEquals(MainActions(compact = true, newMain = false), MainActions.of(setOf(CompactFeature)))
        assertEquals(MainActions(compact = false, newMain = false), MainActions.of(emptySet()))
        // Clear context (#5100) only with the hub's assistant_main_clear, apart from the other two.
        assertEquals(MainActions(compact = false, newMain = false, clear = true), MainActions.of(setOf("assistant_main_clear")))
        assertEquals(MainActions(compact = true, newMain = true, clear = true), MainActions.of(setOf(CompactFeature, NewMainFeature, ClearMainFeature)))
    }

    /** Seen running here: "Compacting", then "Compacted" once, not again on the next update. */
    @Test fun aCompactionSeenRunningAnnouncesItsEndOnce() {
        var status = CompactionStatus().observe(running())
        assertEquals(CompactionLine.Running, status.line)
        status = status.observe(done())
        assertEquals(CompactionLine.Done, status.line)
        status = status.dismissed().observe(done())
        assertNull(status.line)
    }

    /** An end from before the screen opened is old news, whether it went well or not. */
    @Test fun anOldEndIsNotAnnounced() {
        assertNull(CompactionStatus().observe(done()).line)
        assertNull(CompactionStatus().observe(failed()).line)
        assertNull(CompactionStatus().observe(null).line)
    }

    @Test fun aFailureSaysWhyAndStaysUntilClosed() {
        var status = CompactionStatus().observe(running()).observe(failed())
        assertEquals(CompactionLine.Failed("Not enough messages to compact"), status.line)
        status = status.observe(failed())
        assertEquals(CompactionLine.Failed("Not enough messages to compact"), status.line)
        assertNull(status.dismissed().line)
    }

    /** Asked for here: "Compacting" at once; the old compaction still on the session is not this one's end. */
    @Test fun askingShowsRunningAndWaitsForItsOwnCompaction() {
        var status = CompactionStatus().observe(done()).asked(done())
        assertEquals(CompactionLine.Running, status.line)
        status = status.observe(done()) // an update before the hub took it in
        assertEquals(CompactionLine.Running, status.line)
        status = status.observe(running(second)).observe(done(second))
        assertEquals(CompactionLine.Done, status.line)
    }

    /** One so quick that the screen never saw it running is still announced when it was asked for here. */
    @Test fun aQuickCompactionAskedForHereIsAnnounced() {
        val status = CompactionStatus().asked(null).observe(failed(second, "对话太短"))
        assertEquals(CompactionLine.Failed("对话太短"), status.line)
    }

    /** A refused request (409 busy) puts the line back to what the session says. */
    @Test fun aRefusalClearsTheLine() {
        assertNull(CompactionStatus().asked(done()).refused(done()).line)
        assertEquals(CompactionLine.Running, CompactionStatus().asked(null).refused(running(second)).line)
    }

    /** "Compacting" does not close by hand, and goes when the session no longer says it runs. */
    @Test fun runningStaysUntilTheSessionSaysOtherwise() {
        val status = CompactionStatus().observe(running())
        assertEquals(CompactionLine.Running, status.dismissed().line)
        assertNull(status.observe(null).line)
    }

    @Test fun theDialogNamesTheDayAndTheNoticeWhatCarriedOver() {
        val resources = ApplicationProvider.getApplicationContext<android.content.Context>().resources
        assertEquals(true, newMainText(resources, LocalDate.of(2026, 11, 4), Locale.forLanguageTag("zh-Hans")).contains("「旧主对话 · 至 11月4日」"))
        fun notice(carried: Boolean, asked: Boolean) = resources.getString(newMainNotice(carried, asked))
        assertEquals("已新建主对话，会带上旧对话的摘要", notice(carried = true, asked = true))
        assertEquals("已新建主对话，旧对话没有能带过来的内容", notice(carried = false, asked = true))
        assertEquals("已新建主对话", notice(carried = false, asked = false))
    }
}
