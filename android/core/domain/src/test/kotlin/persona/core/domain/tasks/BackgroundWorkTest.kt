package persona.core.domain.tasks

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Session

class BackgroundWorkTest {
    @Test fun aLegacyRawCountKeepsWorkActiveWithoutInventingAnIndependentJobCount() {
        val session = Session("fixture-device", "worker", state = "working", status = "working", waiting = true, backgroundCountHint = 7)
        assertNotNull(session.backgroundWork())
        assertEquals(0, session.backgroundWork()?.tasks)
        assertEquals(BackgroundWork(2, 0, 0), session.copy(backgroundTasksCount = 2).backgroundWork())
        assertNull(session.copy(backgroundTasksCount = 0).backgroundWork())
    }
}
