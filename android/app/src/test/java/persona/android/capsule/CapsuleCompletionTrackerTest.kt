package persona.android.capsule

import java.time.Instant
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Session
import persona.core.contracts.SessionsEvent

class CapsuleCompletionTrackerTest {
    private fun task(state: String, rev: Long = 1, machine: String = "machine") =
        Session(machine, "task", title = "Task", state = state, updatedAt = "2026-11-05T09:00:00Z", rev = rev)
    private fun CapsuleCompletionTracker.accept(vararg sessions: Session, muted: Boolean = false) =
        update(SessionsEvent(sessions.first().machineId, sessions.toList())) { muted }

    @Test fun initialUnreadAndRevisionChangesAreOnlyABaseline() {
        val tracker = CapsuleCompletionTracker()
        assertTrue(tracker.accept(task("unread")).isEmpty())
        assertTrue(tracker.accept(task("unread", 2)).isEmpty())
        assertTrue(tracker.accept(task("working", 3)).isEmpty())
        assertEquals(listOf(task("unread", 4)), tracker.accept(task("unread", 4)))
    }

    @Test fun eachWorkingEdgeCompletesOnceAndVersionsAloneNeverComplete() {
        val tracker = CapsuleCompletionTracker()
        tracker.accept(task("working"))
        assertEquals(1, tracker.accept(task("unread", 2)).size)
        assertTrue(tracker.accept(task("unread", 3)).isEmpty())
        tracker.accept(task("working", 3))
        assertTrue(tracker.accept(task("unread", 2)).isEmpty())
        tracker.accept(task("working", 3))
        assertEquals(1, tracker.accept(task("unread", 4)).size)
    }

    @Test fun aKnownWorkingRoundStaysArmedThroughQuestionsAndApprovals() {
        val tracker = CapsuleCompletionTracker()
        tracker.accept(task("working"))
        assertTrue(tracker.accept(task("needs_you", 2)).isEmpty())
        assertTrue(tracker.accept(task("needs_you", 3)).isEmpty())
        assertEquals(listOf(task("unread", 4)), tracker.accept(task("unread", 4)))
        assertTrue(tracker.accept(task("needs_you", 5)).isEmpty())
        assertTrue(tracker.accept(task("unread", 6)).isEmpty())
    }

    @Test fun aRequestBaselineCannotClaimAnUnobservedWorkingRound() {
        val tracker = CapsuleCompletionTracker()
        assertTrue(tracker.accept(task("needs_you")).isEmpty())
        assertTrue(tracker.accept(task("unread", 2)).isEmpty())
        tracker.accept(task("working", 3))
        tracker.accept(task("needs_you", 4))
        assertEquals(1, tracker.accept(task("unread", 5)).size)
    }

    @Test fun idleOfflineRemovalResetAndFilteredCompletionClearTheArmedRound() {
        for (state in listOf("idle", "offline", "unknown")) {
            val tracker = CapsuleCompletionTracker()
            tracker.accept(task("working"))
            tracker.accept(task("needs_you", 2))
            tracker.accept(task(state, 3))
            assertTrue(tracker.accept(task("needs_you", 4)).isEmpty())
            assertTrue(tracker.accept(task("unread", 5)).isEmpty())
        }
        val tracker = CapsuleCompletionTracker()
        tracker.accept(task("working"))
        tracker.accept(task("needs_you", 2))
        tracker.update(SessionsEvent("machine")) { false }
        assertTrue(tracker.accept(task("unread", 3)).isEmpty())
        tracker.accept(task("working", 4))
        tracker.accept(task("needs_you", 5))
        tracker.reset("machine")
        assertTrue(tracker.accept(task("unread", 6)).isEmpty())
        tracker.accept(task("working", 7))
        tracker.accept(task("needs_you", 8))
        assertTrue(tracker.accept(task("unread", 9), muted = true).isEmpty())
        assertTrue(tracker.accept(task("needs_you", 10)).isEmpty())
        assertTrue(tracker.accept(task("unread", 11)).isEmpty())
    }

    @Test fun reconnectAndRemovedSessionsForgetTheirWorkingEdge() {
        val tracker = CapsuleCompletionTracker()
        tracker.accept(task("working"))
        tracker.reset("machine")
        assertTrue(tracker.accept(task("unread", 2)).isEmpty())
        tracker.accept(task("working", 3))
        tracker.update(SessionsEvent("machine")) { false }
        assertTrue(tracker.accept(task("unread", 4)).isEmpty())
        tracker.accept(task("working", 5))
        assertEquals(1, tracker.accept(task("unread", 6)).size)
    }

    @Test fun quietMutedAndInterruptedCompletionsAreConsumedWithoutLaterReplay() {
        for (suppressed in listOf(task("unread", 2).copy(quiet = true), task("unread", 2).copy(turnError = "Model stopped"), task("unread", 2))) {
            val tracker = CapsuleCompletionTracker()
            tracker.accept(task("working"))
            assertTrue(tracker.accept(suppressed, muted = !suppressed.quiet && !suppressed.interrupted).isEmpty())
            tracker.accept(task("working", 3))
            assertTrue(tracker.accept(task("unread", 2)).isEmpty())
            tracker.accept(task("working", 3))
            assertEquals(1, tracker.accept(task("unread", 4)).size)
        }
    }

    @Test fun aUsageLimitedUnreadTurnIsNotAnnounced() {
        val tracker = CapsuleCompletionTracker()
        tracker.accept(task("working"))
        val limited = task("unread", 2).copy(turnError = "Weekly limit reached", turnErrorKind = "usage_limit",
            turnErrorResetsAt = Instant.now().plusSeconds(3600).toString())
        assertTrue(limited.interrupted)
        assertTrue(limited.isUsageLimited())
        assertTrue(tracker.accept(limited).isEmpty())
    }

    @Test fun qualifiedGroupsAndAssistantCompletionsStayIndependent() {
        val tracker = CapsuleCompletionTracker()
        val primary = task("working")
        val main = task("working", machine = "machine~g0a1b2c3d").copy(assistant = true)
        tracker.accept(primary)
        tracker.accept(main)
        val first = tracker.accept(primary.copy(state = "unread"))
        val second = tracker.accept(main.copy(state = "unread"))
        assertEquals(primary.key, first.single().key)
        assertEquals(main.key, second.single().key)
        assertNotEquals(completionId(first.single()), completionId(second.single()))
        assertTrue(second.single().assistant)
    }
}
