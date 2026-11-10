package persona.core.data

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import persona.core.contracts.TodoSpace

/** The to-dos kept in the pairing's list snapshots read back as the hub gave them (#4533). */
class TodoSnapshotTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test fun oversizedSnapshotIsSkippedWithoutChangingItsOriginal() = runTest {
        val store = ListSnapshotStore(temporary.newFolder())
        store.write("large", "123456789")
        assertNull(store.read("large", maxBytes = 8))
        assertEquals("123456789", store.read("large", maxBytes = 9))
    }

    @Test fun toDosWrittenToTheSnapshotsReadBackTheSame() = runTest {
        val directory = temporary.newFolder()
        assertNull(ListSnapshotStore(directory).todoCache().read())
        val todos = listOf(
            Todo(id = "a", title = "写周报", notes = "周五前", createdAt = "2026-11-05T08:00:00Z",
                agents = listOf(TodoAgent("mac", "s1")), spaces = listOf(TodoSpace("mac", "web"))),
            Todo(id = "b", title = "done", doneAt = "2026-11-05T09:30:00Z", doneBy = "user", quiet = true),
        )
        ListSnapshotStore(directory).todoCache().write(todos)
        // A new store over the same directory is the next start.
        assertEquals(todos, ListSnapshotStore(directory).todoCache().read())
    }
}
