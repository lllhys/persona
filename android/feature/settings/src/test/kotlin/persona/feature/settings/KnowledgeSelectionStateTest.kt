package persona.feature.settings

import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import persona.core.contracts.api.KnowledgeDetail
import persona.core.contracts.api.KnowledgeEntry
import persona.core.contracts.api.KnowledgeList
import persona.core.contracts.api.KnowledgeSelection
import persona.core.contracts.api.KnowledgeSelectionCandidate
import persona.core.contracts.api.KnowledgeSelectionCandidates
import persona.core.contracts.api.KnowledgeSelectionChange
import persona.core.contracts.api.KnowledgeSelectionUnsupported
import persona.core.hub.HubError
import persona.core.hub.HubFailure

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class KnowledgeSelectionStateTest {
    private val res = RuntimeEnvironment.getApplication().resources

    private class FakeSelection(initial: JsonObject) : KnowledgeSelection {
        override val changes: Flow<KnowledgeSelectionChange> = emptyFlow()
        var stored = initial
        val reads = mutableListOf<JsonObject>()
        val readFailures = mutableListOf<Exception>()
        val puts = mutableListOf<JsonObject>()
        val calls = mutableListOf<String>()
        var readGate: CompletableDeferred<Unit>? = null
        var putGate: CompletableDeferred<Unit>? = null
        var putFailure: Exception? = null
        var putResponse: JsonObject? = null
        var candidatesResult: KnowledgeSelectionCandidates? = null
        var candidatesFailure: Exception? = null
        var documentList = KnowledgeList()
        val details = mutableMapOf<String, KnowledgeDetail>()

        override suspend fun selection(): JsonObject {
            calls += "GET"
            readGate?.await()
            readFailures.removeFirstOrNull()?.let { throw it }
            return reads.removeFirstOrNull() ?: stored
        }

        override suspend fun put(document: JsonObject): JsonObject {
            calls += "PUT"
            puts += document
            putGate?.await()
            putFailure?.let { throw it }
            stored = document
            return putResponse ?: document
        }

        override suspend fun candidates(source: String): KnowledgeSelectionCandidates {
            candidatesFailure?.let { throw it }
            return candidatesResult ?: KnowledgeSelectionCandidates(source = source)
        }

        override suspend fun documents(): KnowledgeList = documentList
        override suspend fun document(id: String): KnowledgeDetail = details.getValue(id)
    }

    private fun model(api: FakeSelection): KnowledgeSelectionModel = KnowledgeSelectionModel(api).also {
        runBlocking { it.load() }
    }

    @Test fun optimisticEditReadsFreshPreservesExternalFieldsAndDisplaysTheReturnedDocument() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        val state = model(api)
        val fresh = original.setRuleDays(KnowledgeSelectionPart.Messages, "group", 7)
        api.reads += JsonObject(fresh.root + ("future_root" to JsonPrimitive("kept")))
        val gate = CompletableDeferred<Unit>()
        api.readGate = gate
        val job = launch(start = CoroutineStart.UNDISPATCHED) {
            state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) }
        }
        assertFalse(state.state.value.document!!.rule(KnowledgeSelectionPart.Messages, "mentions")!!.on)
        assertTrue(state.state.value.saving)
        assertEquals(listOf("GET", "GET"), api.calls)
        gate.complete(Unit)
        job.join()
        assertEquals(listOf("GET", "GET", "PUT"), api.calls)
        val sent = KnowledgeSelectionDocument(api.puts.single())
        assertFalse(sent.rule(KnowledgeSelectionPart.Messages, "mentions")!!.on)
        assertEquals(7, sent.rule(KnowledgeSelectionPart.Messages, "group")!!.days)
        assertEquals("kept", sent.root["future_root"]!!.jsonPrimitive.content)
        api.putResponse = JsonObject(sent.root + ("stored_by_hub" to JsonPrimitive(true)))
        // The next no-op does not write; a different change proves the returned document, not the request, is displayed.
        state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mine", false) }
        assertEquals(true, state.state.value.document!!.root["stored_by_hub"]?.jsonPrimitive?.content?.toBoolean())
        assertNull(state.state.value.saveError)
    }

    @Test fun aFreshReadThatAlreadyHasTheChangeDoesNotPut() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        val state = model(api)
        api.reads += original.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false).root
        assertTrue(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) })
        assertEquals(listOf("GET", "GET"), api.calls)
        assertTrue(api.puts.isEmpty())
        assertFalse(state.state.value.document!!.rule(KnowledgeSelectionPart.Messages, "mentions")!!.on)
    }

    @Test fun aTrueNoOpHasNoNetworkTraffic() = runBlocking {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        val state = model(api)
        assertTrue(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", true) })
        assertTrue(state.edit { it.moveLevel(KnowledgeSelectionPart.Messages, "mentions", -1) })
        assertEquals(listOf("GET"), api.calls)
    }

    @Test fun aRefusedPutRestoresTheExactShownDocumentAndUsesTheHubText() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        val state = model(api)
        api.putFailure = HubFailure(HubError.Server, "days must be from 1 to 36500", 400)
        assertFalse(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) })
        assertEquals(original.root, state.state.value.document!!.root)
        assertFalse(state.state.value.saving)
        assertFalse(state.state.value.readOnly)
        assertEquals("Couldn't save the change: days must be from 1 to 36500", state.state.value.saveError?.resolve(res))
    }

    @Test fun aFailedFreshReadRestoresWithoutPutting() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        val state = model(api)
        api.readFailures += HubFailure(HubError.Timeout)
        assertFalse(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) })
        assertEquals(original.root, state.state.value.document!!.root)
        assertTrue(api.puts.isEmpty())
        assertNotNull(state.state.value.saveError)
    }

    @Test fun aFreshRead403RollsBackWithoutLatchingReadOnly() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        val state = model(api)
        api.readFailures += HubFailure(HubError.Server, "temporary access failure", 403)
        assertFalse(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) })
        assertEquals(original.root, state.state.value.document!!.root)
        assertFalse(state.state.value.readOnly)
        assertNotNull(state.state.value.saveError)
        assertTrue(api.puts.isEmpty())
    }

    @Test fun aStaleRuleSheetPreservesFieldsItDidNotEdit() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val opened = original.rule(KnowledgeSelectionPart.Messages, "mentions")!!
        val api = FakeSelection(original.root)
        val state = model(api)
        api.reads += original.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false)
            .setRuleDays(KnowledgeSelectionPart.Messages, "group", 7).root
        assertTrue(state.saveRule(opened, KnowledgeSelectionPart.Messages, "mentions",
            on = true, days = 30, attachmentDays = null, includeAll = true, context = null))
        val sent = KnowledgeSelectionDocument(api.puts.single())
        assertFalse(sent.rule(KnowledgeSelectionPart.Messages, "mentions")!!.on)
        assertEquals(30, sent.rule(KnowledgeSelectionPart.Messages, "mentions")!!.days)
        assertEquals(7, sent.rule(KnowledgeSelectionPart.Messages, "group")!!.days)
        assertFalse(state.state.value.document!!.rule(KnowledgeSelectionPart.Messages, "mentions")!!.on)
    }

    @Test fun aReloadedRuleSheetKeepsEveryUnchangedFieldAndWritesEveryChangedField() = runBlocking {
        data class SheetValues(
            val on: Boolean,
            val days: Int?,
            val attachmentDays: Int?,
            val includeAll: Boolean?,
            val context: Int?,
        )
        data class FieldCase(
            val label: String,
            val rule: String,
            val external: (KnowledgeSelectionDocument) -> KnowledgeSelectionDocument,
            val input: (SheetValues, Boolean) -> SheetValues,
            val value: (KnowledgeSelectionRule) -> Any?,
            val kept: Any?,
            val written: Any?,
        )

        val original = KnowledgeSelectionDocument.defaults()
            .setRuleDays(KnowledgeSelectionPart.Messages, "mentions", 10)
            .setAttachmentDays("mentions", 11)
            .setIncludeAll(true)
            .setMineContext(50)
        val cases = listOf(
            FieldCase("on", "mentions",
                external = { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) },
                input = { values, changed -> values.copy(on = if (changed) false else values.on, days = 60) },
                value = { it.on }, kept = false, written = false),
            FieldCase("days", "mentions",
                external = { it.setRuleDays(KnowledgeSelectionPart.Messages, "mentions", 30) },
                input = { values, changed -> values.copy(on = false, days = if (changed) 60 else values.days) },
                value = { it.days }, kept = 30, written = 60),
            FieldCase("attachment days", "mentions",
                external = { it.setAttachmentDays("mentions", 31) },
                input = { values, changed -> values.copy(on = false,
                    attachmentDays = if (changed) 61 else values.attachmentDays) },
                value = { it.attachmentDays }, kept = 31, written = 61),
            FieldCase("include all", "mentions",
                external = { it.setIncludeAll(false) },
                input = { values, changed -> values.copy(on = false,
                    includeAll = if (changed) false else values.includeAll) },
                value = { it.includeAll }, kept = false, written = false),
            FieldCase("context", "mine",
                external = { it.setMineContext(80) },
                input = { values, changed -> values.copy(on = false, context = if (changed) 100 else values.context) },
                value = { it.context }, kept = 80, written = 100),
        )

        for (case in cases) for (changed in listOf(false, true)) {
            val label = "${case.label}, changed=$changed"
            val opened = original.rule(KnowledgeSelectionPart.Messages, case.rule)!!
            val api = FakeSelection(original.root)
            val state = model(api)
            api.stored = (if (changed) original else case.external(original)).root
            state.onExternalChange()
            val values = SheetValues(opened.on, opened.days, opened.attachmentDays, opened.includeAll, opened.context)
            val requested = case.input(values, changed)
            assertTrue(label, state.saveRule(opened, KnowledgeSelectionPart.Messages, case.rule,
                requested.on, requested.days, requested.attachmentDays, requested.includeAll, requested.context))
            val sent = KnowledgeSelectionDocument(api.puts.single()).rule(KnowledgeSelectionPart.Messages, case.rule)!!
            assertEquals(label, if (changed) case.written else case.kept, case.value(sent))
            if (case.label == "on") assertEquals(label, 60, sent.days) else assertFalse(label, sent.on)
        }
    }

    @Test fun aStaleItemSheetKeepsFreshIdentityMetadataAndUnknownFields() = runBlocking {
        val opened = KnowledgeSelectionItem.newChat("source", "chat-1", "Original")
        val original = KnowledgeSelectionDocument.defaults().setItem(opened)
        val api = FakeSelection(original.root)
        val state = model(api)
        val freshItem = JsonObject(opened.copy(name = "Fresh name").raw + ("future_item" to JsonPrimitive("kept")))
        api.reads += JsonObject(original.root + ("items" to JsonArray(listOf(freshItem))))
        val desired = opened.copy(mode = KnowledgeItemMode.All, days = 45)
        assertTrue(state.saveItem(opened, desired, isNew = false))
        val sent = KnowledgeSelectionDocument(api.puts.single()).item(opened.id)!!
        assertEquals(KnowledgeItemMode.All, sent.mode)
        assertEquals(45, sent.days)
        assertEquals("Fresh name", sent.name)
        assertEquals("kept", sent.raw["future_item"]!!.jsonPrimitive.content)
    }

    @Test fun a403RollsBackLatchesReadOnlyAndRefusesLaterEdits() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        val state = model(api)
        api.putFailure = HubFailure(HubError.Server, "read-only device", 403)
        assertFalse(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) })
        assertEquals(original.root, state.state.value.document!!.root)
        assertTrue(state.state.value.readOnly)
        assertFalse(state.state.value.editable)
        assertNull(state.state.value.saveError)
        val calls = api.calls.toList()
        assertFalse(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mine", false) })
        assertEquals(calls, api.calls)
    }

    @Test fun anOldHubRetiresTheDocumentAndTheAvailabilityProbe() = runBlocking {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root).apply {
            readFailures += KnowledgeSelectionUnsupported()
        }
        val state = KnowledgeSelectionModel(api)
        state.load()
        assertTrue(state.state.value.unsupported)
        assertNull(state.state.value.document)
        assertFalse(state.state.value.editable)
        assertEquals(res.getString(R.string.settings_knowledge_selection_old_hub), state.state.value.readError?.resolve(res))

        val available = KnowledgeSelectionAvailability(api)
        api.readFailures += KnowledgeSelectionUnsupported()
        available.probe()
        assertEquals(false, available.state.value)
    }

    @Test fun versionTwoIsUnsupportedWithoutAReadErrorOrAnyWrite() = runBlocking {
        val api = FakeSelection(JsonObject(KnowledgeSelectionDocument.defaults().root + ("version" to JsonPrimitive(2))))
        val state = KnowledgeSelectionModel(api)
        state.load()
        assertTrue(state.state.value.unsupportedDocument)
        assertNull(state.state.value.document)
        assertNull(state.state.value.readError)
        assertFalse(state.state.value.editable)
        assertFalse(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) })
        assertEquals(listOf("GET"), api.calls)
        assertTrue(api.puts.isEmpty())
    }

    @Test fun anOrderNamingARuleTwiceIsUnsupportedWithoutAReadErrorOrAnyWrite() = runBlocking {
        val base = KnowledgeSelectionDocument.defaults()
        val messages = base.root.getValue("messages").jsonObject
        val duplicated = JsonArray(base.order(KnowledgeSelectionPart.Messages).mapIndexed { index, level ->
            JsonArray((if (index == 0) level + "mentions" else level).map(::JsonPrimitive))
        })
        val root = JsonObject(base.root + ("messages" to JsonObject(messages + ("order" to duplicated))))
        val api = FakeSelection(root)
        val state = KnowledgeSelectionModel(api)
        state.load()
        assertTrue(state.state.value.unsupportedDocument)
        assertNull(state.state.value.document)
        assertNull(state.state.value.readError)
        assertFalse(state.state.value.editable)
        assertFalse(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) })
        assertEquals(listOf("GET"), api.calls)
        assertTrue(api.puts.isEmpty())
    }

    @Test fun aTransportReadFailureKeepsItsRetryableReadError() = runBlocking {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root).apply {
            readFailures += HubFailure(HubError.Timeout)
        }
        val state = KnowledgeSelectionModel(api)
        state.load()
        assertFalse(state.state.value.unsupportedDocument)
        assertNull(state.state.value.document)
        assertNotNull(state.state.value.readError)
        assertTrue(api.puts.isEmpty())
    }

    @Test fun anUnsupportedFreshDocumentStopsBeforePutAndRetiresEditing() = runBlocking {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        val state = model(api)
        api.reads += JsonObject(KnowledgeSelectionDocument.defaults().root + ("version" to JsonPrimitive(2)))
        assertFalse(state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) })
        assertTrue(state.state.value.unsupportedDocument)
        assertNull(state.state.value.document)
        assertEquals(listOf("GET", "GET"), api.calls)
        assertTrue(api.puts.isEmpty())
    }

    @Test fun availabilitySurvivesTransientFailures() = runBlocking {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        val available = KnowledgeSelectionAvailability(api)
        available.probe()
        assertEquals(true, available.state.value)
        api.readFailures += HubFailure(HubError.Timeout)
        available.probe()
        assertEquals(true, available.state.value)
    }

    @Test fun anIdleEventReloadsButASaveDefersItsEventUntilIdle() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        val state = model(api)
        api.stored = original.setRuleDays(KnowledgeSelectionPart.Messages, "group", 9).root
        state.onExternalChange()
        assertEquals(9, state.state.value.document!!.rule(KnowledgeSelectionPart.Messages, "group")!!.days)
        val readsBeforeSave = api.calls.count { it == "GET" }

        val gate = CompletableDeferred<Unit>()
        api.putGate = gate
        val save = launch(start = CoroutineStart.UNDISPATCHED) {
            state.edit { it.setRuleOn(KnowledgeSelectionPart.Messages, "mentions", false) }
        }
        state.onExternalChange()
        assertEquals(readsBeforeSave + 1, api.calls.count { it == "GET" }) // only the save's fresh read
        assertTrue(state.state.value.saving)
        gate.complete(Unit)
        save.join()
        assertEquals(readsBeforeSave + 2, api.calls.count { it == "GET" })
        assertFalse(state.state.value.saving)
        assertFalse(state.state.value.document!!.rule(KnowledgeSelectionPart.Messages, "mentions")!!.on)
    }

    @Test fun anInvalidOptimisticEditReportsInsteadOfThrowingOrWriting() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        val state = model(api)
        val calls = api.calls.toList()
        assertFalse(state.edit { it.setItem(KnowledgeSelectionItem.newChat("", "bad", "Bad")) })
        assertEquals(calls, api.calls)
        assertEquals(original.root, state.state.value.document!!.root)
        assertNotNull(state.state.value.saveError)
        assertTrue(api.puts.isEmpty())
    }

    @Test fun pickersReopenExistingOverridesInsteadOfResettingThem() = runBlocking {
        val chat = KnowledgeSelectionItem.newChat("messages", "chat-1", "Chat").copy(mode = KnowledgeItemMode.Skip)
        val document = KnowledgeSelectionItem.newDocument("drive", "doc-1", "Document")
            .copy(mode = KnowledgeItemMode.All, days = 30)
        val original = KnowledgeSelectionDocument.defaults().setItem(chat).setItem(document)
        val api = FakeSelection(original.root)
        val state = model(api)
        val candidate = KnowledgeSelectionCandidate(id = "chat-1", name = "Candidate", kind = "group", source = "messages", read = "none")
        assertEquals(KnowledgeItemMode.Skip, state.chatItem(candidate).mode)
        api.details["kb-file"] = KnowledgeDetail(kind = "docs", id = "kb-file", path = "docs/kb-file.md",
            meta = buildJsonObject { put("source", JsonPrimitive("drive")); put("id", JsonPrimitive("doc-1")) })
        val reopened = state.documentItem(KnowledgeEntry(kind = "docs", id = "kb-file", name = "New display name"))!!
        assertEquals(KnowledgeItemMode.All, reopened.mode)
        assertEquals(30, reopened.days)
        assertEquals("Document", reopened.name)
    }

    @Test fun candidateIdentityUsesItsFilingSourceAndA404HasNoPicker() = runBlocking {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        val state = model(api)
        api.candidatesResult = KnowledgeSelectionCandidates(
            source = "queried-connector", cached = true,
            chats = listOf(KnowledgeSelectionCandidate(id = "chat-1", name = "Design", kind = "group", source = "filed-source",
                estimate = 120, read = "personal", days = 30)),
        )
        state.loadCandidates("queried-connector")
        assertTrue(state.state.value.candidates!!.cached)
        val item = state.chatItem(state.state.value.candidates!!.chats.single())
        assertEquals(KnowledgeItemId("filed-source", "chat", "chat-1"), item.id)
        assertEquals(KnowledgeItemMode.Rules, item.mode)
        assertEquals("personal", state.state.value.candidates!!.chats.single().read)
        assertEquals(120, state.state.value.candidates!!.chats.single().estimate)

        api.candidatesFailure = KnowledgeSelectionUnsupported()
        state.loadCandidates("old-source")
        assertTrue(state.state.value.candidateUnavailable)
        assertNull(state.state.value.candidates)
        assertNull(state.state.value.candidateError)
    }

    @Test fun documentIdentityComesFromDetailMetadata() = runBlocking {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        val state = model(api)
        api.details["kb-file-name"] = KnowledgeDetail(kind = "docs", id = "kb-file-name", path = "docs/kb-file-name.md",
            meta = buildJsonObject {
                put("source", JsonPrimitive("drive")); put("id", JsonPrimitive("source-document-id"))
            })
        val document = state.documentItem(KnowledgeEntry(kind = "docs", id = "kb-file-name", name = "Product plan"))!!
        assertEquals(KnowledgeItemId("drive", "doc", "source-document-id"), document.id)
        assertEquals("Product plan", document.name)
    }

    @Test fun missingDocumentIdentityExplainsAndCannotAdd() = runBlocking {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        val state = model(api)
        api.details["legacy"] = KnowledgeDetail(kind = "docs", id = "legacy", path = "docs/legacy.md", meta = JsonObject(emptyMap()))
        assertNull(state.documentItem(KnowledgeEntry(kind = "docs", id = "legacy", name = "Legacy")))
        assertEquals(res.getString(R.string.settings_knowledge_selection_identity_missing), state.state.value.documentsError?.resolve(res))
        assertTrue(api.puts.isEmpty())
    }

    @Test fun anUnchangedExistingItemCannotResurrectAnExternalRemoval() = runBlocking {
        val item = KnowledgeSelectionItem.newChat("source", "chat", "Design")
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        val state = model(api)
        assertTrue(state.saveItem(item, item, isNew = false))
        assertEquals(listOf("GET"), api.calls)
        assertNull(state.state.value.document!!.item(item.id))
        assertTrue(api.puts.isEmpty())
    }

    @Test fun anUnchangedNewItemStillAdds() = runBlocking {
        val item = KnowledgeSelectionItem.newChat("source", "chat", "Design")
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        val state = model(api)
        assertTrue(state.saveItem(item, item, isNew = true))
        assertNotNull(state.state.value.document!!.item(item.id))
        assertEquals(1, api.puts.size)
    }

    @Test fun removingAnAlreadyAbsentItemOffersNoUndoOrWrite() = runBlocking {
        val item = KnowledgeSelectionItem.newChat("source", "chat", "Design")
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        val state = model(api)
        assertTrue(state.removeItem(item))
        assertNull(state.state.value.removedItem)
        state.undoRemoval()
        assertEquals(listOf("GET"), api.calls)
        assertTrue(api.puts.isEmpty())
    }

    @Test fun removalCanBeUndoneOnlyAfterItWasStored() = runBlocking {
        val item = KnowledgeSelectionItem.newChat("source", "chat", "Design")
        val original = KnowledgeSelectionDocument.defaults().setItem(item)
        val api = FakeSelection(original.root)
        val state = model(api)
        state.removeItem(item)
        assertNull(state.state.value.document!!.item(item.id))
        assertEquals(item, state.state.value.removedItem)
        state.undoRemoval()
        assertEquals(item, state.state.value.document!!.item(item.id))
        assertNull(state.state.value.removedItem)
        assertEquals(2, api.puts.size)
    }
}
