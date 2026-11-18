package persona.core.domain.tasks

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.PhoneQuickResults
import persona.core.contracts.api.TasksApi

/**
 * #9340, as iOS `TasksStore.undo(_:)`: the assistant's calendar event comes off the phone, the card turns Undone as soon
 * as the local delete returned, a failure leaves it as it was, and only a calendar event this phone made is offered.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class QuickResultUndoTest {
    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }

    private class Api : TasksApi {
        override suspend fun todos(): List<Todo> = emptyList()
        override suspend fun feed(conversation: String, since: String?) = FeedResponse()
        override suspend fun startPlan(id: String, remember: Boolean): TaskPlan = error("unused")
        override suspend fun dismissPlan(id: String): TaskPlan = error("unused")
        override suspend fun retryTask(todoId: String): Todo = error("unused")
        override suspend fun removeTask(todoId: String) = Unit
        override suspend fun route(conversation: String, todoId: String, text: String): FeedItem = error("unused")
        override suspend fun today(conversation: String): FeedItem = error("unused")
        override suspend fun mainConversation(): String? = null
        override suspend fun inboxReply(itemId: String, text: String, card: String?) = Unit
    }

    /** The phone's delete: every id it was asked for, held on [gate] while the card is Undoing, failing on [failure]. */
    private class Phone(var failure: Exception? = null) : PhoneQuickResults {
        val removed = mutableListOf<Long>()
        var gate: CompletableDeferred<Unit>? = null
        override suspend fun removeCalendarEvent(id: Long) {
            removed += id
            gate?.await()
            failure?.let { throw it }
        }
    }

    private val event = QuickResult(reminder = false, callId = "call-1", itemId = 42, title = "Dentist",
        time = "2026-11-13T15:30:00Z", place = "Personal")

    private fun store(scope: kotlinx.coroutines.CoroutineScope, phone: PhoneQuickResults?): TasksStore =
        TasksStore(Api(), Connection(), scope, quickResults = phone)

    @Test fun undoRunsAddedUndoingUndoneAndDeletesTheCardsEvent() = runTest {
        val phone = Phone().apply { gate = CompletableDeferred() }
        val store = store(backgroundScope, phone)
        assertEquals(QuickResultUndo.Added, store.quickResultUndo(event))

        val undo = async { store.undoQuickResult(event) }
        runCurrent()
        // While the delete runs: Undoing, and the card takes no second tap.
        assertEquals(QuickResultUndo.Undoing, store.quickResultUndo(event))
        assertFalse(store.undoQuickResult(event))
        assertEquals(listOf(42L), phone.removed)

        phone.gate!!.complete(Unit)
        assertTrue(undo.await())
        runCurrent()
        assertEquals(QuickResultUndo.Undone, store.quickResultUndo(event))
        assertEquals(listOf(42L), phone.removed)
    }

    @Test fun aFailedUndoLeavesTheCardAsItWasAndCanBeTriedAgain() = runTest {
        val phone = Phone(failure = IllegalStateException("no event with id \"42\""))
        val store = store(backgroundScope, phone)
        assertFalse(store.undoQuickResult(event))
        assertEquals(QuickResultUndo.Added, store.quickResultUndo(event))
        // Still the assistant's event: a later tap tries the delete again.
        phone.failure = null
        assertTrue(store.undoQuickResult(event))
        assertEquals(QuickResultUndo.Undone, store.quickResultUndo(event))
        assertEquals(listOf(42L, 42L), phone.removed)
    }

    @Test fun onlyThisPhonesCalendarEventsAreOffered() = runTest {
        val phone = Phone()
        val store = store(backgroundScope, phone)
        // A reminder is not: Android has no reminders store, so `phone_reminders_create` ran on an iPhone.
        val reminder = event.copy(reminder = true)
        assertFalse(store.canUndoQuickResult(reminder))
        assertFalse(store.undoQuickResult(reminder))
        // A card whose result named no event id (an old message, another phone's id) has nothing to delete.
        assertFalse(store.canUndoQuickResult(event.copy(itemId = null)))
        assertFalse(store.undoQuickResult(event.copy(itemId = null)))
        assertTrue(phone.removed.isEmpty())
        // The calendar event is offered, and only where the phone wired its delete.
        assertTrue(store.canUndoQuickResult(event))
        assertFalse(store(backgroundScope, null).canUndoQuickResult(event))
    }
}
