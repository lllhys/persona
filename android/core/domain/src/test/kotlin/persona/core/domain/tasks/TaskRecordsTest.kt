package persona.core.domain.tasks

import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.FeedItem
import persona.core.contracts.JsonCodec
import persona.core.contracts.PlanTask
import persona.core.contracts.TaskPlan
import persona.core.contracts.Todo
import persona.core.domain.lines.ChatLine

/**
 * #4339 C13: on Glass the conversation draws the hub's task records as iOS's `FeedLine` does, message cards included; only
 * plans and task cards keep their other places. Brief and Board keep every record behind the trays, as before.
 */
class TaskRecordsTest {
    private fun at(n: Int) = "2026-11-12T10:00:${n.toString().padStart(2, '0')}Z"
    private val plan = TaskPlan("plan", "main", listOf(PlanTask("Fix the login", todoId = "t")), "started", at(1))
    private val todos = listOf(Todo("t", "Fix the login", origin = "main", planId = "plan", createdAt = at(1)))
    private fun item(id: String, n: Int, kind: String, ref: String? = "t", vararg fields: Pair<String, String>) =
        FeedItem(id, "main", at(n), kind, ref, buildJsonObject { fields.forEach { (k, v) -> put(k, v) } })
    private val feed = listOf(
        item("plan-feed", 1, "plan", "plan"),
        item("task-feed", 2, "task"),
        item("handoff", 3, "routed", "t", "from" to "assistant", "text" to "Fix the login crash"),
        item("asked", 4, "routed", "t", "text" to "Use the staging key"),
        item("report", 5, "report", "t", "kind" to "question", "text" to "Which key?"),
        item("review", 6, "review", "t", "verdict" to "rework"),
        item("receipt", 7, "receipt", "t", "title" to "Fix the login", "summary" to "Done"),
        item("notice", 8, "notice", null, "text" to "The morning brief runs again"),
        item("message", 9, "message", "inbox-1", "text" to "When is it fixed?", "draft" to "Today"),
    )
    private fun feedIds(lines: List<ChatLine>) =
        lines.filterIsInstance<ChatLine.Extra>().mapNotNull { (it.payload as? AssistantExtra.Feed)?.item?.id }
    private fun hints(lines: List<ChatLine>) = lines.filterIsInstance<ChatLine.Extra>().mapNotNull { it.payload as? DispatchHint }

    @Test fun glassDrawsTheRecordsAmongTheLines() {
        val lines = AssistantTaskCards.lines(emptyList(), feed, mapOf(plan.id to plan), todos, inline = true)
        assertEquals(listOf("handoff", "asked", "notice", "message"), feedIds(lines))
        assertEquals(listOf("handoff", "asked", "report", "review", "receipt", "notice", "message"), feed.map { it.id }.filter { it in setOf("handoff", "asked", "report", "review", "receipt", "notice", "message") })
        // the plan and the task card are the dispatched line, once
        assertEquals(listOf(listOf("Fix the login")), hints(lines).map { hint -> hint.tasks.map { it.title } })
    }

    @Test fun glassKeepsOnlyPlansAndTaskCardsBehind() {
        assertEquals(listOf("plan-feed", "task-feed"), AssistantTaskCards.backgroundFeed(feed, inline = true).map { it.id })
    }

    @Test fun briefAndBoardKeepTheRecordsBehindTheTrays() {
        val lines = AssistantTaskCards.lines(emptyList(), feed, mapOf(plan.id to plan), todos)
        // the user's own word to the task stays, as before
        assertEquals(listOf("asked"), feedIds(lines))
        assertEquals(listOf("plan-feed", "task-feed", "handoff", "notice", "message"),
            AssistantTaskCards.backgroundFeed(feed).map { it.id })
    }

    // A plan nobody started yet: the hub's `plan` item and its tasks, none of them a to-do yet.
    private val open = TaskPlan("open", "main", listOf(PlanTask("Write the release notes", machine = "mac", cwd = "/w/shop"),
        PlanTask("Look into the crash report", machine = "box", cwd = "/srv/app", readonly = true)), "open", at(10))
    private val openFeed = listOf(item("open-feed", 10, "plan", "open"))

    @Test fun glassDrawsAPlanNobodyStartedAsACardAmongTheLines() {
        val lines = AssistantTaskCards.lines(emptyList(), feed + openFeed, mapOf(plan.id to plan, open.id to open), todos, inline = true)
        // iOS's `FeedLine`: the open plan is `PlanCard`, at the end of the exchange it happened in; the started one is the dispatched line
        assertEquals(listOf("handoff", "asked", "notice", "message", "open-feed"), feedIds(lines))
        assertEquals(listOf(listOf("Fix the login")), hints(lines).map { hint -> hint.tasks.map { it.title } })
        // Brief and Board keep every plan behind the trays
        val behind = AssistantTaskCards.lines(emptyList(), feed + openFeed, mapOf(plan.id to plan, open.id to open), todos)
        assertEquals(listOf("asked"), feedIds(behind))
        assertEquals(emptyList<DispatchHint>(), hints(behind).filter { hint -> hint.tasks.any { it.title == "Write the release notes" } })
    }

    @Test fun theRecordKeepsEveryPlanWhetherOrNotTheConversationDrawsIt() {
        // the Background Records sheet lists plans and task cards as the hub's records (iOS has no such sheet; Android keeps it)
        assertEquals(listOf("plan-feed", "task-feed", "open-feed"), AssistantTaskCards.backgroundFeed(feed + openFeed, inline = true).map { it.id })
    }

    @Test fun aPlanIsTheCardUntilItIsStartedThenTheDispatchedLineInItsPlace() {
        val tasks = open.tasks
        fun lines(plan: TaskPlan, todos: List<Todo> = emptyList()) =
            AssistantTaskCards.lines(emptyList(), openFeed, mapOf(plan.id to plan), todos, inline = true)
        // open: the card, no dispatched line
        lines(open).let { assertEquals(listOf("open-feed"), feedIds(it)); assertEquals(0, hints(it).size) }
        // Start came back: the plan is started and its to-dos exist: the dispatched line, in the card's place, no card
        val started = open.copy(state = "started", tasks = listOf(tasks[0].copy(todoId = "n1"), tasks[1].copy(todoId = "n2")))
        val made = listOf(Todo("n1", "Write the release notes", origin = "main", planId = "open", createdAt = at(11)),
            Todo("n2", "Look into the crash report", origin = "main", planId = "open", createdAt = at(11)))
        lines(started, made).let {
            assertEquals(emptyList<String>(), feedIds(it))
            assertEquals(listOf(listOf("Write the release notes", "Look into the crash report")), hints(it).map { hint -> hint.tasks.map { t -> t.title } })
        }
        // the hub started it but a task did not come up: still the dispatched line, the failed task as plain text (no to-do to open)
        val partly = open.copy(state = "started", tasks = listOf(tasks[0].copy(todoId = "n1"), tasks[1].copy(error = "launcher not found")))
        lines(partly, made.take(1)).let {
            assertEquals(emptyList<String>(), feedIds(it))
            assertEquals(listOf(listOf("n1" to "Write the release notes", null to "Look into the crash report")),
                hints(it).map { hint -> hint.tasks.map { t -> t.todoId to t.title } })
        }
        // Don't start: the line stays, as "Not started · N tasks"
        lines(open.copy(state = "dismissed")).let { assertEquals(listOf("open-feed"), feedIds(it)); assertEquals(0, hints(it).size) }
        // a plan this build can't read leaves nothing to draw
        assertEquals(emptyList<String>(), feedIds(AssistantTaskCards.lines(emptyList(), openFeed, emptyMap(), emptyList(), inline = true)))
        // the item may carry its plan itself, as the hub's event does (docs/protocol/protocol.md: a plan card's body)
        val carrying = JsonCodec.decodeFromString(FeedItem.serializer(), """{"id":"open-feed","conversation":"main","at":"${at(10)}","kind":"plan","ref":"open",
            "body":{"plan":{"id":"open","conversation":"main","state":"open","tasks":[{"title":"Write the release notes","machine":"mac","cwd":"/w/shop"}]}}}""")
        assertEquals(listOf("open-feed"), feedIds(AssistantTaskCards.lines(emptyList(), listOf(carrying), emptyMap(), emptyList(), inline = true)))
    }

    @Test fun aPlanLandsAfterTheAnswerOfItsExchangeAndBeforeTheNextMessage() {
        val ask = { id: String, n: Int -> persona.core.domain.lines.LineItem(persona.core.contracts.Item(id, n, ts = at(n), role = "user", kind = "text", text = id)) }
        val answer = persona.core.domain.lines.LineItem(persona.core.contracts.Item("answer", 2, ts = at(2), role = "assistant", kind = "text", text = "Here is a plan"))
        val transcript = persona.core.domain.lines.chatLines(listOf(ask("first", 1), answer, ask("second", 20))).lines
        val ids = AssistantTaskCards.lines(transcript, openFeed, mapOf(open.id to open), emptyList(), inline = true).map { it.id }
        // the plan item sits after the first exchange's answer and before the second message, as every other record does
        assertEquals(listOf("feed-open-feed"), ids.filter { it.startsWith("feed-") })
        val at = ids.indexOf("feed-open-feed")
        assertEquals(true, ids.indexOf(answer.id) < at && at < ids.indexOf("second"))
    }

    @Test fun aNoticeKeepsItsKindOnTheWire() {
        val decoded = JsonCodec.decodeFromString(FeedItem.serializer(),
            """{"id":"n","conversation":"main","at":"${at(8)}","kind":"notice","body":{"text":"Ready again"}}""")
        assertEquals("notice", decoded.kind)
        assertEquals("Ready again", decoded.field("text"))
        // a kind this build doesn't know still reads as unknown
        assertEquals("unknown", JsonCodec.decodeFromString(FeedItem.serializer(), """{"id":"x","kind":"later_kind"}""").kind)
    }
}
