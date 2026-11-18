package persona.core.domain.tasks

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Session
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import java.time.Instant

/**
 * In progress's rows that only wait in the background (iOS `AssistantTrayActivity.reportedBackgroundWork`, `TaskState.waiting`
 * and `DockAgent.backgroundWaiting`, #4339 C18): such a row is a clock and "Waiting", not "Working (Background)".
 */
class TrayWaitingTest {
    private val now = Instant.parse("2026-11-12T10:00:00Z")
    private val at = now.minusSeconds(3_600).toString()

    private fun worker(waiting: Boolean, tasks: Int? = null) = Session("m", "worker", title = "Worker", state = "working", status = "working",
        updatedAt = at, backgroundWorking = true, backgroundWaiting = waiting, backgroundTasksCount = tasks)

    private fun entries(session: Session, todos: List<Todo> = emptyList()) =
        assistantEntries("main", "main", todos, listOf(session), emptyList(), emptyList(), emptyList(), now.toEpochMilli())

    private val task = Todo("t", "Task", origin = "main", createdAt = at, updatedAt = at, agents = listOf(TodoAgent("m", "worker")))

    @Test fun backgroundWorkThatOnlyWaitsReportsNoWorkOfItsOwn() {
        assertNull(worker(waiting = true).backgroundWork())
        assertTrue(worker(waiting = true).waitsInBackground)
        // still running: the generic line, with no counts to say more
        assertEquals(BackgroundWork(0, 0, 0), worker(waiting = false).backgroundWork())
        assertFalse(worker(waiting = false).waitsInBackground)
        // its commands are counted: the counts stand, as the hub says them (iOS reads the summary's counts)
        assertEquals(BackgroundWork(2, 0, 0), worker(waiting = true, tasks = 2).backgroundWork())
    }

    @Test fun aTaskWhoseAgentOnlyWaitsInTheBackgroundIsWaiting() {
        val row = entries(worker(waiting = true), listOf(task)).running.single() as RunningEntry.Task
        assertNull(row.activity)
        assertTrue(row.waiting)
    }

    @Test fun aTaskWhoseAgentRunsBackgroundWorkIsNotWaiting() {
        val running = entries(worker(waiting = false), listOf(task)).running.single() as RunningEntry.Task
        assertEquals(BackgroundWork(0, 0, 0), running.activity)
        assertFalse(running.waiting)
        // counted commands that sleep are work the tray lists, as on iOS
        val counted = entries(worker(waiting = true, tasks = 1), listOf(task)).running.single() as RunningEntry.Task
        assertEquals(BackgroundWork(1, 0, 0), counted.activity)
        assertFalse(counted.waiting)
    }

    @Test fun anAgentOpenedByHandThatOnlyWaitsInTheBackgroundIsWaiting() {
        val waiting = entries(worker(waiting = true)).running.single() as RunningEntry.Agent
        assertNull(waiting.activity)
        assertTrue(waiting.waiting)
        val busy = entries(worker(waiting = false)).running.single() as RunningEntry.Agent
        assertEquals(BackgroundWork(0, 0, 0), busy.activity)
        assertFalse(busy.waiting)
    }
}
