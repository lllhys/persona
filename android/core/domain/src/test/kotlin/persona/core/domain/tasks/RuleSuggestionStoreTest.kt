package persona.core.domain.tasks

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.FeedItem
import persona.core.contracts.FeedResponse
import persona.core.contracts.RuleScope
import persona.core.contracts.RuleSuggestion
import persona.core.contracts.RuleSuggestionMade
import persona.core.contracts.ServerEvent
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubCodedFailure
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.RequestManager
import persona.core.contracts.api.TasksApi

/**
 * Always and Not now on a card's suggested rule (#4929, docs/assistant/unified-entry.md "A rule after the same answer three
 * times"): what Always made stays with the card it was tapped on, a stale suggestion only loses its line, any other
 * refusal leaves the line to answer again, and an answer is the card's own, never every card's with the same id.
 */
class RuleSuggestionStoreTest {
    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }

    private class Refused(override val status: Int?) : RuntimeException("refused"), HubCodedFailure {
        override val code: String? = null
    }

    private class Api : TasksApi {
        val calls = mutableListOf<Triple<String, String, String?>>()
        var failure: Throwable? = null
        var gate: CompletableDeferred<Unit>? = null
        var made = RuleSuggestionMade("standing_authorization", RuleSuggestion.STANDING_AUTHORIZATION)
        var reads = 0
        override suspend fun ruleAlways(suggestionId: String): RuleSuggestionMade {
            calls += Triple("always", suggestionId, currentCoroutineContext()[RequestManager]?.id)
            gate?.await()
            failure?.let { throw it }
            return made
        }
        override suspend fun ruleNotNow(suggestionId: String) {
            calls += Triple("not-now", suggestionId, currentCoroutineContext()[RequestManager]?.id)
            failure?.let { throw it }
        }
        override suspend fun todos(): List<Todo> { reads++; return emptyList() }
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

    private val merge = RuleSuggestion("rs_merge", "standing_authorization", "You approved this 3 times. Always allow merging?",
        RuleScope("example-node", "Example node", "/code/example", "merge", listOf("ci_green", "no_conflicts"), "Merge the PR now? → Yes"),
        RuleSuggestion.STANDING_AUTHORIZATION, count = 3)

    private fun TasksStore.outcome(card: String) = ruleAnswers.value[card]?.outcome

    @Test fun alwaysKeepsWhatWasMadeForTheCardItWasTappedOn() = runTest {
        val api = Api().apply { gate = CompletableDeferred() }
        val store = TasksStore(api, Connection(), backgroundScope)
        store.ruleAlways("todo:t1", merge, { throw it }, manager = "work")
        // one answer at a time: a second tap and Not now wait for the first
        store.ruleAlways("todo:t1", merge, { throw it }, manager = "work")
        store.ruleNotNow("todo:t1", merge, { throw it }, manager = "work")
        runCurrent()
        assertEquals(listOf(Triple("always", "rs_merge", "work")), api.calls)
        // only the action that was taken is on its way
        assertEquals(setOf(ruleAlwaysKey("todo:t1")), store.busy.value)
        assertNull(store.outcome("todo:t1"))
        api.gate!!.complete(Unit); runCurrent()
        assertEquals(RuleAnswer(merge, RuleOutcome.Made(api.made)), store.ruleAnswers.value["todo:t1"])
        assertTrue(store.busy.value.isEmpty())
        // the to-dos are read again: the hub words the decision without its line now
        assertEquals(1, api.reads)
        // answered: neither action is sent again for this card
        store.ruleAlways("todo:t1", merge, { throw it })
        store.ruleNotNow("todo:t1", merge, { throw it })
        runCurrent()
        assertEquals(1, api.calls.size)
        // and the hub sending the card without its line does not take what was made away
        store.ruleSettled("todo:t1")
        assertEquals(RuleOutcome.Made(api.made), store.outcome("todo:t1"))
    }

    @Test fun notNowPutsTheLineAwayWithoutARule() = runTest {
        val api = Api()
        val store = TasksStore(api, Connection(), backgroundScope)
        store.ruleNotNow("plan:p1", merge, { throw it }); runCurrent()
        assertEquals(listOf(Triple("not-now", "rs_merge", null as String?)), api.calls)
        assertEquals(RuleAnswer(merge, RuleOutcome.NotNow), store.ruleAnswers.value["plan:p1"])
    }

    @Test fun aStaleSuggestionIsClearedAndIsNotAnError() = runTest {
        for (action in listOf("always", "not-now")) {
            val api = Api().apply { failure = IllegalStateException("wrapped", Refused(404)) }
            val store = TasksStore(api, Connection(), backgroundScope)
            val errors = mutableListOf<Throwable>()
            if (action == "always") store.ruleAlways("plan:p1", merge, { errors += it }) else store.ruleNotNow("plan:p1", merge, { errors += it })
            runCurrent()
            assertTrue("$action reported a stale suggestion as an error", errors.isEmpty())
            assertEquals(RuleOutcome.Gone, store.outcome("plan:p1"))
            assertTrue(store.busy.value.isEmpty())
        }
    }

    /**
     * The hub's ids name a kind and a scope and come back: on the next card once the answers earn the rule again, and,
     * for the delegate (one id for every assistant), on another assistant's card. Each card is answered on its own.
     */
    @Test fun anAnswerIsTheCardsOwnAndTheSameIdIsAnsweredAgainOnAnotherCard() = runTest {
        val api = Api()
        val store = TasksStore(api, Connection(), backgroundScope)
        // stale on the first card: the oldest of the three answers left the window
        api.failure = Refused(404)
        store.ruleAlways("todo:t1:r1", merge, { throw it }, manager = "work"); runCurrent()
        assertEquals(RuleOutcome.Gone, store.outcome("todo:t1:r1"))
        // earned again: the same id on the to-do's next decision is sent, and made
        api.failure = null
        store.ruleAlways("todo:t1:r2", merge, { throw it }, manager = "work"); runCurrent()
        assertEquals(RuleOutcome.Made(api.made), store.outcome("todo:t1:r2"))
        assertEquals(RuleOutcome.Gone, store.outcome("todo:t1:r1"))
        // Not now for one assistant's question says nothing about another assistant's with the same id
        store.ruleNotNow("card:a", merge, { throw it }, manager = "work"); runCurrent()
        assertNull(store.outcome("card:b"))
        store.ruleAlways("card:b", merge, { throw it }, manager = "home"); runCurrent()
        assertEquals(RuleOutcome.Made(api.made), store.outcome("card:b"))
        assertEquals(RuleOutcome.NotNow, store.outcome("card:a"))
        assertEquals(listOf("work", "work", "work", "home"), api.calls.map { it.third })
    }

    @Test fun aLinePutAwayIsSettledOnceTheHubSendsTheCardWithoutIt() = runTest {
        val api = Api()
        val store = TasksStore(api, Connection(), backgroundScope)
        store.ruleNotNow("plan:p1", merge, { throw it }); runCurrent()
        // the card still carries the line the hub has not taken off yet: not sent again
        store.ruleNotNow("plan:p1", merge, { throw it }); runCurrent()
        assertEquals(1, api.calls.size)
        store.ruleSettled("plan:p1")
        assertNull(store.outcome("plan:p1"))
        // the same card earns it again later: it is there to answer
        store.ruleAlways("plan:p1", merge, { throw it }); runCurrent()
        assertEquals(RuleOutcome.Made(api.made), store.outcome("plan:p1"))
    }

    @Test fun anyOtherRefusalLeavesTheLineToAnswerAgain() = runTest {
        val api = Api()
        val store = TasksStore(api, Connection(), backgroundScope)
        val errors = mutableListOf<Throwable>()
        // the standing authorizations changed meanwhile (409), no full access (403), storage unavailable (503)
        for (status in listOf(409, 403, 503)) {
            api.failure = Refused(status)
            store.ruleAlways("card:q1", merge, { errors += it }); runCurrent()
            assertNull(store.outcome("card:q1"))
            assertTrue(store.busy.value.isEmpty())
        }
        assertEquals(listOf(409, 403, 503), errors.map { (it as Refused).status })
        api.failure = null
        store.ruleAlways("card:q1", merge, { throw it }); runCurrent()
        assertEquals(RuleOutcome.Made(api.made), store.outcome("card:q1"))
        assertEquals(4, api.calls.size)
    }

    @Test fun aSuggestionWithoutAnIdIsNeverSent() = runTest {
        val api = Api()
        val store = TasksStore(api, Connection(), backgroundScope)
        store.ruleAlways("plan:p1", RuleSuggestion(""), { throw it })
        store.ruleNotNow("plan:p1", RuleSuggestion(" "), { throw it })
        runCurrent()
        assertTrue(api.calls.isEmpty())
    }
}
