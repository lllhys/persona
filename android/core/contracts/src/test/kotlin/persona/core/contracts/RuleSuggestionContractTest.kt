package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.ModelCatalog

/**
 * The fields the availability follow-ups added (docs/protocol/protocol.md `RuleSuggestion`, `for_briefing`, a brief's
 * `setup`, a purpose's `description`): each decodes where the hub puts it, and a hub from before them, or one field of
 * another shape, leaves the card as it was.
 */
class RuleSuggestionContractTest {
    private val standing = """{"id":"rs_1","kind":"standing_authorization",
        "text":"You approved this 3 times. Always allow merging in /code/example on Example node?",
        "scope":{"machine_id":"example-node","machine":"Example node","directory":"/code/example","action":"merge",
            "conditions":["ci_green","no_conflicts"],"quote":"Merge the PR now? → Yes"},
        "creates":"standing_authorization","count":3}"""

    @Test fun aSuggestionDecodesOnAPlanADecisionAndAQuestion() {
        val plan = JsonCodec.decodeFromString<TaskPlan>("""{"id":"p1","conversation":"main","state":"open","tasks":[],
            "suggestion":{"id":"rs_p","kind":"plan_start","text":"You started tasks like this 3 times.",
                "scope":{"machine_id":"example-node","machine":"Example node","directory":"/code/example"},"creates":"tasks.trust","count":3}}""")
        assertEquals("rs_p", plan.suggestion?.id)
        assertEquals(RuleSuggestion.TASK_TRUST, plan.suggestion?.creates)
        assertEquals("/code/example", plan.suggestion?.scope?.directory)
        assertNull(plan.suggestion?.scope?.action)

        val todo = JsonCodec.decodeFromString<Todo>("""{"id":"t1","ask":{"outcome":"decide","verdict":"Merge?","asked_at":"2026-11-10T08:00:00Z",
            "for_briefing":true,"suggestion":$standing}}""")
        val suggestion = todo.ask?.suggestion ?: error("no suggestion on the ask")
        assertTrue(todo.ask?.forBriefing == true)
        assertEquals(RuleSuggestion.STANDING_AUTHORIZATION, suggestion.creates)
        assertEquals("Example node", suggestion.scope.machine)
        assertEquals(listOf("ci_green", "no_conflicts"), suggestion.scope.conditions)
        assertEquals("Merge the PR now? → Yes", suggestion.scope.quote)
        assertEquals(3, suggestion.count)
        assertTrue(suggestion.isShown)

        val card = JsonCodec.decodeFromString<Interaction>("""{"id":"q1","machine_id":"example-node","session_id":"s1","kind":"question",
            "suggestion":{"id":"rs_d","kind":"delegate","text":"You took the recommendation 3 times.","scope":{},"creates":"","setting":"agents.delegate","count":3}}""")
        assertEquals("agents.delegate", card.suggestion?.setting)
        assertEquals("", card.suggestion?.creates)
        assertNull(card.suggestion?.scope?.directory)
    }

    @Test fun anOlderHubAndAFieldOfAnotherShapeLeaveTheCardAsItWas() {
        assertNull(JsonCodec.decodeFromString<TaskPlan>("""{"id":"p1","state":"open","tasks":[]}""").suggestion)
        val ask = JsonCodec.decodeFromString<Todo>("""{"id":"t1","ask":{"outcome":"decide","verdict":"Merge?","asked_at":"2026-11-10T08:00:00Z"}}""").ask
        assertNull(ask?.suggestion)
        assertFalse(ask?.forBriefing ?: true)
        assertNull(JsonCodec.decodeFromString<Interaction>("""{"id":"q1","kind":"question"}""").suggestion)
        // a suggestion without its id, or one that is not an object, is dropped; the rest of the card stays
        val broken = JsonCodec.decodeFromString<Todo>("""{"id":"t1","title":"Kept","ask":{"outcome":"decide","verdict":"Merge?",
            "asked_at":"2026-11-10T08:00:00Z","for_briefing":"soon","suggestion":{"text":"no id"}}}""")
        assertEquals("Kept", broken.title)
        assertEquals("Merge?", broken.ask?.verdict)
        assertNull(broken.ask?.suggestion)
        assertFalse(broken.ask?.forBriefing ?: true)
        val plan = JsonCodec.decodeFromString<TaskPlan>("""{"id":"p1","state":"open","tasks":[],"suggestion":"always"}""")
        assertEquals("open", plan.state)
        assertNull(plan.suggestion)
        // a line with no words has nothing to draw
        assertFalse(RuleSuggestion("rs_1").isShown)
    }

    @Test fun whatAlwaysMadeDecodesForEveryKind() {
        val trust = JsonCodec.decodeFromString<RuleSuggestionMade>("""{"ok":true,"kind":"plan_start","creates":"tasks.trust","key":"k"}""")
        assertEquals(RuleSuggestion.TASK_TRUST, trust.creates)
        val standing = JsonCodec.decodeFromString<RuleSuggestionMade>(
            """{"ok":true,"kind":"standing_authorization","creates":"standing_authorization","authorization":{"id":"a1"},"revision":2}""")
        assertEquals(RuleSuggestion.STANDING_AUTHORIZATION, standing.creates)
        val delegate = JsonCodec.decodeFromString<RuleSuggestionMade>("""{"ok":true,"kind":"delegate","creates":"","setting":"agents.delegate"}""")
        assertEquals("", delegate.creates)
        assertEquals("agents.delegate", delegate.setting)
    }

    @Test fun aBriefsRecommendedSetupDecodesRowByRow() {
        val item = JsonCodec.decodeFromString<FeedItem>("""{"id":"brief1","kind":"brief","body":{"title":"Morning briefing",
            "setup":[{"id":"triage_decide","setting":"agents.triage.decide","count":4,"text":"Card for decisions would have asked you 4 times less."},
                {"id":"supervisor_tier","setting":"assistant.supervise","count":12,"text":"The background supervisor held on its model."},
                {"id":"leaders","count":3,"minutes":31,"text":"A leader would have taken 3 rounds off the main chat.","link":"https://hub.example.com/settings?tab=persona"},
                {"id":"future_mechanism","count":1,"text":"Something a newer hub names."},
                {"id":"task_trust","count":2,"text":""},{"count":2,"text":"no id"},"bad"]}}""")
        val setup = item.brief?.setup ?: error("no brief")
        assertEquals(listOf("triage_decide", "supervisor_tier", "leaders", "future_mechanism"), setup.map { it.id })
        assertEquals(listOf(BriefSetupPage.SessionTriage, BriefSetupPage.SupervisorModel, null, null), setup.map { it.page })
        assertEquals(4, setup.first().count)
        assertEquals(BriefSetupPage.SessionTriage, BriefSetup("triage_auto_continue", "t").page)
        assertEquals(BriefSetupPage.SessionTriage, BriefSetup("triage_wake", "t").page)
        assertEquals(BriefSetupPage.StandingAuthorizations, BriefSetup("standing_authorizations", "t").page)
        // the mechanisms this app has no page for say where in their text
        for (id in listOf("delegate", "task_trust", "review_sample", "independent_review")) assertNull(BriefSetup(id, "t").page)
        // every other briefing, and an older hub
        val plain = JsonCodec.decodeFromString<FeedItem>("""{"id":"brief2","kind":"brief","body":{"title":"Evening briefing","setup":"none"}}""")
        assertTrue(plain.brief?.setup?.isEmpty() == true)
        assertTrue(JsonCodec.decodeFromString<FeedItem>("""{"id":"brief3","kind":"brief","body":{}}""").brief?.setup?.isEmpty() == true)
    }

    @Test fun aPurposeSaysWhatItIsForWhenTheHubDoes() {
        val catalog = JsonCodec.decodeFromString<ModelCatalog>("""{"purposes":[
            {"id":"assistant","name":"Main chat","group":"Main chat","description":" Answers you in the Persona conversation. "},
            {"id":"task.t1","name":"A task","group":"Tasks"},
            {"id":"agent.new","name":"Agents","description":""},
            {"id":"lark.agent","name":"Lark /agent","description":7}]}""")
        assertEquals(listOf("Answers you in the Persona conversation.", null, null, null), catalog.purposes.map { it.about })
        assertEquals(listOf("assistant", "task.t1", "agent.new", "lark.agent"), catalog.purposes.map { it.id })
    }
}
