package persona.feature.chat

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.SessionKey

/**
 * A task's or an agent's detail is a bottom sheet on a phone and a page pushed into the chat's column on a wide window,
 * as on iOS (`DetailPresentation`, #9347). "All" is a sheet at any width, as iOS's board is.
 */
class TaskDetailPresentationTest {
    private val task = TaskSheet.Task("t1")
    private val agent = TaskSheet.Agent(SessionKey("mac", "lead"))

    @Test fun aPhoneKeepsEveryDetailASheet() {
        assertFalse(task.pushesOnWide(wide = false))
        assertFalse(agent.pushesOnWide(wide = false))
        assertFalse(TaskSheet.Board.pushesOnWide(wide = false))
    }

    @Test fun aWideWindowPushesATaskAndAnAgentIntoTheChatsColumn() {
        assertTrue(task.pushesOnWide(wide = true))
        assertTrue(agent.pushesOnWide(wide = true))
    }

    @Test fun allStaysASheetAtEveryWidth() {
        assertFalse(TaskSheet.Board.pushesOnWide(wide = true))
    }
}
