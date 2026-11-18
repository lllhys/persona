package persona.core.contracts

import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.*

/** Renaming and trashing files (docs/protocol/protocol.md "Changing files"): the answers' shapes and what a transport without them does. */
class SessionFileEditsContractTest {
    @Test fun theHubsAnswersDecode() {
        assertEquals("~/a/b.txt", JsonCodec.decodeFromString<SessionFilePath>("""{"path":"~/a/b.txt","future":1}""").path)
        val trashed = JsonCodec.decodeFromString<SessionTrashedFiles>("""{"trashed":[{"path":"~/a","trash_id":"t1"}]}""").trashed
        assertEquals(listOf(SessionTrashedFile("~/a", "t1")), trashed)
        assertTrue(JsonCodec.decodeFromString<SessionTrashedFiles>("""{"trashed":null}""").trashed.isEmpty())
        assertEquals(listOf("~/a"), JsonCodec.decodeFromString<SessionRestoredFiles>("""{"restored":[{"path":"~/a"}]}""").restored.map { it.path })
        assertTrue(JsonCodec.decodeFromString<SessionRestoredFiles>("{}").restored.isEmpty())
    }

    @Test fun aTransportWithoutEditsSaysSo() = runBlocking {
        val readOnly = object : SessionFiles {
            override suspend fun directory(machineId: String, cwd: String, path: String) = error("unused")
            override suspend fun file(machineId: String, cwd: String, path: String) = error("unused")
            override suspend fun status(machineId: String, cwd: String) = error("unused")
            override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) = error("unused")
            override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int) = error("unused")
            override suspend fun show(machineId: String, cwd: String, hash: String) = error("unused")
        }
        // The browser offers no Rename or Move to Trash, and a call fails as unsupported rather than doing anything.
        assertFalse(readOnly.editsFiles("m"))
        for (call in listOf<suspend () -> Unit>({ readOnly.rename("m", "/w", "a", "b") }, { readOnly.trash("m", "/w", listOf("a")) },
                { readOnly.restore("m", listOf("t1")) })) {
            try { call(); fail("Expected UnsupportedOperationException") } catch (_: UnsupportedOperationException) {}
        }
    }
}
