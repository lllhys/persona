package persona.core.contracts

import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.api.KnowledgeDetail
import persona.core.contracts.api.KnowledgeList
import persona.core.contracts.api.KnowledgeSelection
import persona.core.contracts.api.KnowledgeSelectionCandidates
import persona.core.contracts.api.KnowledgeSelectionChange
import persona.core.contracts.api.RequestManager
import persona.core.contracts.api.forManager

class KnowledgeSelectionContractTest {
    private class RecordingSelection : KnowledgeSelection {
        val calls = mutableListOf<Pair<String, String?>>()
        val allChanges = listOf(KnowledgeSelectionChange(), KnowledgeSelectionChange("m-work"), KnowledgeSelectionChange("m-other"))
        override val changes = flowOf(allChanges[0], allChanges[1], allChanges[2])

        override suspend fun selection(): JsonObject = record("selection", buildJsonObject { put("version", 1) })
        override suspend fun put(document: JsonObject): JsonObject = record("put", document)
        override suspend fun candidates(source: String) = record("candidates", KnowledgeSelectionCandidates(source, chats = emptyList()))
        override suspend fun documents() = record("documents", KnowledgeList())
        override suspend fun document(id: String) = record("document", KnowledgeDetail("docs", id, "data/docs/$id"))

        private suspend fun <T> record(name: String, answer: T): T {
            calls += name to currentCoroutineContext()[RequestManager]?.id
            return answer
        }
    }

    @Test fun everyRequestAndOnlyMatchingChangesAreManagerScoped() = runBlocking {
        val source = RecordingSelection()
        val scoped = source.forManager("m-work")

        assertEquals(listOf(KnowledgeSelectionChange("m-work")), scoped.changes.toList())
        assertEquals(source.allChanges, source.changes.toList())
        assertEquals(
            listOf(KnowledgeSelectionChange()),
            source.forManager(persona.core.contracts.api.ManagerRoster.ASSISTANT_MANAGER).changes.toList(),
        )

        scoped.withPinnedConnection {
            selection()
            put(buildJsonObject { put("version", 1) })
        }
        scoped.candidates("chat")
        scoped.documents()
        scoped.document("doc/id")

        assertEquals(listOf("selection", "put", "candidates", "documents", "document"), source.calls.map { it.first })
        assertEquals(List(source.calls.size) { "m-work" }, source.calls.map { it.second })
    }

    @Test fun selectionEventsDecodeAndEncodeWithTheirManager() {
        val plain = KnowledgeSelectionEvent()
        val stamped = KnowledgeSelectionEvent("m-work")
        assertEquals(plain, ServerEvent.decode("""{"type":"kb_selection"}"""))
        assertEquals(stamped, ServerEvent.decode("""{"type":"kb_selection","manager":"m-work"}"""))
        assertEquals("""{"type":"kb_selection"}""", JsonCodec.encodeToString(ServerEvent.serializer(), plain))
        assertEquals("""{"manager":"m-work","type":"kb_selection"}""", JsonCodec.encodeToString(ServerEvent.serializer(), stamped))
    }
}
