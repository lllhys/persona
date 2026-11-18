package persona.platform.phonetools

import android.app.NotificationManager
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(AndroidJUnit4::class)
class PhoneConfirmationsTest {
    private val context get() = ApplicationProvider.getApplicationContext<Context>()

    /**
     * minSdk is 30: the confirmation must post there too (setAuthenticationRequired is API 31, #4221). The unlock flag
     * does not exist on API 30, so only posting is checked there.
     */
    @Test @Config(sdk = [30]) fun postsTheConfirmationOnApi30() = postsAndDenies(expectAuthentication = false)

    @Test @Config(sdk = [35]) fun allowNeedsAnUnlockFromApi31() = postsAndDenies(expectAuthentication = true)

    private fun postsAndDenies(expectAuthentication: Boolean) = runTest {
        val confirmations = PhoneConfirmations(context)
        val answer = async { confirmations.confirm { "Add an event" } }
        runCurrent()
        val token = confirmations.pending.value?.token
        assertNotNull("confirmation should be pending", token)
        val posted = shadowOf(context.getSystemService(NotificationManager::class.java)).allNotifications
        assertEquals(1, posted.size)
        val actions = posted.single().actions
        assertEquals(2, actions.size)
        if (expectAuthentication) {
            assertFalse(actions[0].isAuthenticationRequired)
            assertTrue(actions[1].isAuthenticationRequired)
        }
        confirmations.resolve(token!!, false)
        assertEquals(PhoneConfirmation.Denied, answer.await())
        confirmations.close()
    }

    private val notifications get() = shadowOf(context.getSystemService(NotificationManager::class.java))

    @Test @Config(sdk = [35]) fun anAllowAnswersAllowed() = runTest {
        val confirmations = PhoneConfirmations(context)
        val answer = async { confirmations.confirm { "Add an event" } }
        runCurrent()
        confirmations.resolve(confirmations.pending.value!!.token, true)
        assertEquals(PhoneConfirmation.Allowed, answer.await())
    }

    /**
     * #4456: there is no app-side timeout that withdraws a card. The hub owns how long a confirmed write may
     * wait; advancing the clock alone never completes the confirmation.
     */
    @Test @Config(sdk = [35]) fun waitingIsNotInterruptedByTheClock() = runTest {
        val confirmations = PhoneConfirmations(context)
        val answer = async { confirmations.confirm { "Add an event" } }
        runCurrent()
        assertEquals(1, notifications.allNotifications.size)
        // Far longer than the old 90-second countdown, and than the hub's confirmed-write wait: still pending.
        advanceTimeBy(30 * 60 * 1000L + 5_000)
        runCurrent()
        assertFalse(answer.isCompleted)
        assertNotNull(confirmations.pending.value)
        assertEquals(1, notifications.allNotifications.size)
        // Only the user's answer ends it.
        confirmations.resolve(confirmations.pending.value!!.token, false)
        assertEquals(PhoneConfirmation.Denied, answer.await())
    }

    /**
     * #3861: swiping the notification away ends the confirmation at once as Dismissed (not as a Deny). The
     * notification no longer carries a system-side timeout: the hub decides how long the call may be answered.
     */
    @Test @Config(sdk = [35]) fun swipingTheNotificationAwayDismissesItAtOnce() = runTest {
        val confirmations = PhoneConfirmations(context)
        val answer = async { confirmations.confirm { "Add an event" } }
        runCurrent()
        val notification = notifications.allNotifications.single()
        assertNotNull("the notification needs a delete intent", notification.deleteIntent)
        // what the system sends when the user swipes the notification away
        PhoneConfirmationReceiver().onReceive(context, shadowOf(notification.deleteIntent).savedIntent)
        runCurrent()
        assertTrue("a dismissal must not wait for any timer", answer.isCompleted)
        assertEquals(PhoneConfirmation.Dismissed, answer.await())
        assertNull(confirmations.pending.value)
        assertEquals(0, notifications.allNotifications.size)
    }

    /** A delete intent left over from an earlier confirmation does not end the current one. */
    @Test @Config(sdk = [35]) fun aStaleDismissalIsIgnored() = runTest {
        val confirmations = PhoneConfirmations(context)
        val first = async { confirmations.confirm { "Add an event" } }
        runCurrent()
        val stale = shadowOf(notifications.allNotifications.single().deleteIntent).savedIntent
        confirmations.resolve(confirmations.pending.value!!.token, false)
        assertEquals(PhoneConfirmation.Denied, first.await())
        val second = async { confirmations.confirm { "Add another event" } }
        runCurrent()
        PhoneConfirmationReceiver().onReceive(context, stale)
        runCurrent()
        assertFalse(second.isCompleted)
        confirmations.close()
        assertEquals(PhoneConfirmation.Withdrawn, second.await())
    }

    @Test @Config(sdk = [35]) fun inTheBackgroundWithNotificationsOffItIsNotShown() = runTest {
        notifications.setNotificationsEnabled(false)
        val confirmations = PhoneConfirmations(context)
        assertEquals(PhoneConfirmation.NotShown, confirmations.confirm { "Add an event" })
        assertNull(confirmations.pending.value)
    }

    @Test @Config(sdk = [35]) fun leavingTheDialogWhenNoNotificationCanBePostedIsNotShown() = runTest {
        val confirmations = PhoneConfirmations(context)
        confirmations.setForeground(true)
        val answer = async { confirmations.confirm { "Add an event" } }
        runCurrent()
        assertNotNull(confirmations.pending.value)
        assertEquals(0, notifications.allNotifications.size) // the dialog in the app asks
        notifications.setNotificationsEnabled(false)
        confirmations.setForeground(false)
        assertEquals(PhoneConfirmation.NotShown, answer.await())
    }

    @Test @Config(sdk = [35]) fun closingWithdrawsIt() = runTest {
        val confirmations = PhoneConfirmations(context)
        val answer = async { confirmations.confirm { "Add an event" } }
        runCurrent()
        confirmations.close()
        assertEquals(PhoneConfirmation.Withdrawn, answer.await())
    }

    /**
     * #4456: a second write arriving while one is on screen waits behind it instead of taking its place.
     * Each answer goes to the token it names; an answer to an already-finished token does not affect the next.
     */
    @Test @Config(sdk = [35]) fun aSecondConfirmationQueuesBehindTheFirstAndAnswersDoNotCross() = runTest {
        val confirmations = PhoneConfirmations(context)
        val first = async { confirmations.confirm { "Add event one" } }
        runCurrent()
        val firstToken = confirmations.pending.value!!.token
        val second = async { confirmations.confirm { "Add event two" } }
        runCurrent()
        // Only one card visible; the second is queued, not shown.
        assertEquals(firstToken, confirmations.pending.value?.token)
        assertEquals(1, notifications.allNotifications.size)
        // Answering the first promotes the second; the first resolves, the second stays open.
        confirmations.resolve(firstToken, true)
        runCurrent()
        assertEquals(PhoneConfirmation.Allowed, first.await())
        val secondToken = confirmations.pending.value!!.token
        assertNotEquals(firstToken, secondToken)
        assertFalse(second.isCompleted)
        // A late answer to the first token must not resolve the second.
        confirmations.resolve(firstToken, false)
        runCurrent()
        assertFalse(second.isCompleted)
        // A stale dismissal of the first token must not dismiss the second either.
        confirmations.dismiss(firstToken)
        runCurrent()
        assertFalse(second.isCompleted)
        confirmations.resolve(secondToken, false)
        assertEquals(PhoneConfirmation.Denied, second.await())
        assertNull(confirmations.pending.value)
    }

    /** close() finishes the current card and every queued one as Withdrawn, and cancels their notifications. */
    @Test @Config(sdk = [35]) fun closeWithdrawsCurrentAndEveryQueuedEntry() = runTest {
        val confirmations = PhoneConfirmations(context)
        val first = async { confirmations.confirm { "Add event one" } }
        runCurrent()
        val second = async { confirmations.confirm { "Add event two" } }
        val third = async { confirmations.confirm { "Add event three" } }
        runCurrent()
        confirmations.close()
        assertEquals(PhoneConfirmation.Withdrawn, first.await())
        assertEquals(PhoneConfirmation.Withdrawn, second.await())
        assertEquals(PhoneConfirmation.Withdrawn, third.await())
        assertNull(confirmations.pending.value)
        assertEquals(0, notifications.allNotifications.size)
    }

    /**
     * A cancelled awaiter (the call was dropped) removes only its own entry: if it was still queued the current
     * card stays; if it was current the next queued one is promoted.
     */
    @Test @Config(sdk = [35]) fun aCancelledAwaiterLeavesOnlyItsOwnEntry() = runTest {
        val confirmations = PhoneConfirmations(context)
        val first = async { confirmations.confirm { "Add event one" } }
        runCurrent()
        val firstToken = confirmations.pending.value!!.token
        val second = async { confirmations.confirm { "Add event two" } }
        runCurrent()
        // Cancel the queued one: the current card is unaffected.
        second.cancel()
        runCurrent()
        assertEquals(firstToken, confirmations.pending.value?.token)
        assertFalse(first.isCompleted)
        // Cancel the current one: it withdraws (Cancelled on the awaiter), and the queue is empty so nothing promotes.
        first.cancel()
        runCurrent()
        assertNull(confirmations.pending.value)
    }

    /** Moving to background posts a notification for the current card only; queued entries wait their turn. */
    @Test @Config(sdk = [35]) fun foregroundToBackgroundPostsOnlyTheCurrentCard() = runTest {
        val confirmations = PhoneConfirmations(context)
        confirmations.setForeground(true)
        val first = async { confirmations.confirm { "Add event one" } }
        runCurrent()
        val second = async { confirmations.confirm { "Add event two" } }
        runCurrent()
        assertEquals(0, notifications.allNotifications.size) // foreground dialog
        confirmations.setForeground(false)
        runCurrent()
        assertEquals(1, notifications.allNotifications.size) // only the current one
        // Back to foreground: the dialog takes over and the notification goes.
        confirmations.setForeground(true)
        runCurrent()
        assertEquals(0, notifications.allNotifications.size)
        // Resolve and the queued one becomes current.
        confirmations.resolve(confirmations.pending.value!!.token, true)
        runCurrent()
        assertEquals(PhoneConfirmation.Allowed, first.await())
        // In the foreground the promoted card posts no notification.
        assertEquals(0, notifications.allNotifications.size)
        confirmations.resolve(confirmations.pending.value!!.token, false)
        assertEquals(PhoneConfirmation.Denied, second.await())
    }

    /**
     * Going to background with notifications off ends only the current card as NotShown; a queued entry stays
     * queued and is then promoted (where it also cannot post and resolves as NotShown on its own).
     */
    @Test @Config(sdk = [35]) fun notificationsOffEndsOnlyTheCurrentCard() = runTest {
        val confirmations = PhoneConfirmations(context)
        confirmations.setForeground(true)
        val first = async { confirmations.confirm { "Add event one" } }
        runCurrent()
        val second = async { confirmations.confirm { "Add event two" } }
        runCurrent()
        notifications.setNotificationsEnabled(false)
        confirmations.setForeground(false)
        runCurrent()
        assertEquals(PhoneConfirmation.NotShown, first.await())
        // The second one was promoted and also could not post: NotShown too, but on its own.
        assertEquals(PhoneConfirmation.NotShown, second.await())
        assertNull(confirmations.pending.value)
    }

    /**
     * In the background, answering the current card must post the next queued card's notification. The old
     * code only updated StateFlow on promote, leaving the second write's await hanging forever with no
     * system notification. Exactly one notification is visible at any time: the current card's.
     */
    @Test @Config(sdk = [35]) fun backgroundAnswerPostsTheNextCardsNotification() = runTest {
        val confirmations = PhoneConfirmations(context)
        val first = async { confirmations.confirm { "Add event one" } }
        runCurrent()
        assertEquals(1, notifications.allNotifications.size)
        val second = async { confirmations.confirm { "Add event two" } }
        runCurrent()
        // Still only the first card's notification; the second is queued, not posted.
        assertEquals(1, notifications.allNotifications.size)
        val firstToken = confirmations.pending.value!!.token
        confirmations.resolve(firstToken, true)
        runCurrent()
        assertEquals(PhoneConfirmation.Allowed, first.await())
        // The first card's notification is gone; the second card's notification is posted.
        val secondToken = confirmations.pending.value!!.token
        assertNotEquals(firstToken, secondToken)
        assertEquals(1, notifications.allNotifications.size)
        assertFalse(second.isCompleted)
        confirmations.resolve(secondToken, false)
        runCurrent()
        assertEquals(PhoneConfirmation.Denied, second.await())
        assertEquals(0, notifications.allNotifications.size)
    }

    /** Dismissing the current card's notification also promotes and posts the next one. */
    @Test @Config(sdk = [35]) fun backgroundDismissPostsTheNextCardsNotification() = runTest {
        val confirmations = PhoneConfirmations(context)
        val first = async { confirmations.confirm { "Add event one" } }
        runCurrent()
        val second = async { confirmations.confirm { "Add event two" } }
        runCurrent()
        val firstToken = confirmations.pending.value!!.token
        confirmations.dismiss(firstToken)
        runCurrent()
        assertEquals(PhoneConfirmation.Dismissed, first.await())
        assertEquals(1, notifications.allNotifications.size)
        assertFalse(second.isCompleted)
        confirmations.resolve(confirmations.pending.value!!.token, false)
        assertEquals(PhoneConfirmation.Denied, second.await())
        assertEquals(0, notifications.allNotifications.size)
    }

    /** A cancelled current awaiter (watcher killed the call) promotes and posts the next card too. */
    @Test @Config(sdk = [35]) fun aCancelledCurrentCardLetsTheNextOnePost() = runTest {
        val confirmations = PhoneConfirmations(context)
        val first = async { confirmations.confirm { "Add event one" } }
        runCurrent()
        val second = async { confirmations.confirm { "Add event two" } }
        runCurrent()
        first.cancel()
        runCurrent()
        assertNotNull(confirmations.pending.value)
        assertEquals(1, notifications.allNotifications.size)
        confirmations.resolve(confirmations.pending.value!!.token, false)
        assertEquals(PhoneConfirmation.Denied, second.await())
        assertEquals(0, notifications.allNotifications.size)
    }

    /**
     * Resurrect race: a resolve/close that lands while `showNotification` is mid-post would otherwise leave
     * the answered card's notification on screen (the answer's `cancel` ran before the post). The post-time
     * recheck cancels it again, so no stale notification survives.
     */
    @Test @Config(sdk = [35]) fun aNotificationPostedMidAnswerIsCancelledAgain() = runTest {
        val confirmations = PhoneConfirmations(context)
        val answer = async { confirmations.confirm { "Add an event" } }
        runCurrent()
        assertEquals(1, notifications.allNotifications.size)
        // Mid-post answer: the hook fires inside showNotification, right before NotificationManager.notify.
        // resolve() cancels the (not yet re-posted) notification, then the post lands, then the recheck
        // cancels it again. Final state: answered, no notification left.
        PhoneConfirmations.beforeNotify = { token -> confirmations.resolve(token, true) }
        try {
            confirmations.setForeground(false)  // re-settle, driving another showNotification pass
            runCurrent()
            assertEquals(PhoneConfirmation.Allowed, answer.await())
            assertEquals(0, notifications.allNotifications.size)
        } finally { PhoneConfirmations.beforeNotify = null }
    }
}
