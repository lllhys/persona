package persona.feature.settings

import androidx.activity.ComponentActivity
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextClearance
import androidx.compose.ui.test.performTextInput
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
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
import persona.core.contracts.api.HubConnector
import persona.core.designsystem.PersonaTheme
import persona.core.hub.HubError
import persona.core.hub.HubFailure

private fun ComposeContentTestRule.textField(tag: String) =
    onNode(hasSetTextAction() and hasAnyAncestor(hasTestTag(tag)))

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w360dp-h3000dp-xhdpi")
class KnowledgeSelectionPageTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private class FakeSelection(var stored: JsonObject) : KnowledgeSelection {
        override val changes = MutableSharedFlow<KnowledgeSelectionChange>(extraBufferCapacity = 1)
        val puts = mutableListOf<JsonObject>()
        var putFailure: Exception? = null
        var readFailure: Exception? = null
        var reads = 0
        override suspend fun selection(): JsonObject {
            reads++
            readFailure?.let { throw it }
            return stored
        }
        override suspend fun put(document: JsonObject): JsonObject {
            puts += document
            putFailure?.let { throw it }
            stored = document
            return document
        }
        override suspend fun candidates(source: String) = KnowledgeSelectionCandidates(source = source)
        var documentList = KnowledgeList()
        var detail: KnowledgeDetail? = null
        var detailGate: CompletableDeferred<Unit>? = null
        var detailReads = 0
        override suspend fun documents() = documentList
        override suspend fun document(id: String): KnowledgeDetail {
            detailReads++
            detailGate?.await()
            return detail!!
        }
    }

    private fun show(api: FakeSelection) {
        rule.setContent { PersonaTheme(reduceMotion = true) { KnowledgeSelectionPage(api, null, onBack = {}) } }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("Knowledge selection").fetchSemanticsNodes().isNotEmpty() }
        rule.waitUntil(5_000) { api.reads > 0 }
        rule.waitForIdle()
    }

    private fun putDocument(api: FakeSelection) = KnowledgeSelectionDocument(api.puts.last())

    @Test fun ruleRowsShowOffOnAndDaysAndOpenTheirSheet() {
        val original = KnowledgeSelectionDocument.defaults().setRuleDays(KnowledgeSelectionPart.Messages, "dm", 30)
        val api = FakeSelection(original.root)
        show(api)
        rule.onNodeWithTag("knowledge-rule-messages-group").assert(hasText("Off"))
        rule.onNodeWithTag("knowledge-rule-messages-mentions").assert(hasText("On"))
        rule.onNodeWithTag("knowledge-rule-messages-dm").assert(hasText("30 days")).performClick()
        rule.onNodeWithTag("knowledge-rule-sheet-on").assertIsOn().performClick()
        rule.onNodeWithTag("knowledge-rule-save").performClick()
        rule.waitUntil(5_000) { api.puts.size == 1 }
        rule.waitForIdle()
        assertFalse(putDocument(api).rule(KnowledgeSelectionPart.Messages, "dm")!!.on)
        assertEquals(30, putDocument(api).rule(KnowledgeSelectionPart.Messages, "dm")!!.days)
        assertEquals(2, api.reads) // initial read, then the save's fresh read
        rule.onNodeWithTag("knowledge-rule-messages-dm").assert(hasText("Off"))
        assertTrue(rule.onAllNodesWithText("Save").fetchSemanticsNodes().isEmpty()) // no page-level Save
    }

    @Test fun priorityButtonsMoveJoinAndSplitWholeLevels() {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        show(api)
        val dmActions = rule.onNodeWithTag("knowledge-rule-messages-dm").fetchSemanticsNode()
            .config[SemanticsActions.CustomActions].map { it.label }
        assertTrue("Move up" in dmActions)
        assertTrue("Move down" in dmActions)
        assertTrue(dmActions.any { it.startsWith("Merge Direct chats into Priority 1") })

        rule.onNodeWithTag("knowledge-priority-messages-1").performClick()
        rule.onNodeWithText("Move up").performClick()
        rule.waitUntil(5_000) { api.puts.size == 1 }
        assertEquals(listOf("dm"), putDocument(api).order(KnowledgeSelectionPart.Messages)[0])
        assertEquals(listOf("mentions", "mine"), putDocument(api).order(KnowledgeSelectionPart.Messages)[1])

        rule.onNodeWithTag("knowledge-priority-messages-1").performClick()
        rule.onNodeWithText("Give Your messages its own level").performClick()
        rule.waitUntil(5_000) { api.puts.size == 2 }
        assertEquals(listOf("mentions"), putDocument(api).order(KnowledgeSelectionPart.Messages)[1])
        assertEquals(listOf("mine"), putDocument(api).order(KnowledgeSelectionPart.Messages)[2])

        rule.onNodeWithTag("knowledge-priority-messages-2").performClick()
        rule.onNodeWithText("Merge Your messages into Priority 2").performClick()
        rule.waitUntil(5_000) { api.puts.size == 3 }
        assertEquals(listOf("mentions", "mine"), putDocument(api).order(KnowledgeSelectionPart.Messages)[1])
        assertTrue(putDocument(api).order(KnowledgeSelectionPart.Messages).none { it.isEmpty() })
    }

    @Test fun headerButtonIsAbsentWhenOneLevelContainsOneRule() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                KnowledgeSelectionLevelHeader(KnowledgeSelectionPart.Messages, index = 0,
                    levelCount = 1, ruleCount = 1, enabled = true, onActions = {})
            }
        }
        rule.onNodeWithTag("knowledge-priority-messages-0").assertDoesNotExist()
    }

    @Test fun levelSheetMovesDownAndDisablesMovementPastEitherEnd() {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        show(api)
        rule.onNodeWithTag("knowledge-priority-messages-1").performClick()
        rule.onNodeWithText("Move down").assertIsEnabled().performClick()
        rule.waitUntil(5_000) { api.puts.size == 1 }
        rule.waitForIdle()
        val moved = putDocument(api).order(KnowledgeSelectionPart.Messages)
        assertEquals(listOf("mentions", "mine"), moved[0])
        assertEquals(listOf("group"), moved[1])
        assertEquals(listOf("dm"), moved[2])

        rule.onNodeWithTag("knowledge-priority-messages-0").performClick()
        rule.onNodeWithText("Move up").assertIsNotEnabled()
        rule.onNodeWithText("Move down").assertIsEnabled().performClick()
        rule.waitUntil(5_000) { api.puts.size == 2 }
        rule.waitForIdle()
        rule.onNodeWithTag("knowledge-priority-messages-7").performClick()
        rule.onNodeWithText("Move down").assertIsNotEnabled()
        rule.onNodeWithText("Move up").assertIsEnabled()
    }

    @Test fun openLevelActionsFollowTheirFirstRuleAcrossAReloadReorder() {
        val original = KnowledgeSelectionDocument.defaults()
        val api = FakeSelection(original.root)
        show(api)
        rule.onNodeWithTag("knowledge-priority-messages-1").performClick()
        rule.onNodeWithText("Merge Direct chats into Priority 1").assertExists()
        api.stored = original.moveLevel(KnowledgeSelectionPart.Messages, "dm", 1).root
        api.changes.tryEmit(KnowledgeSelectionChange())
        rule.waitUntil(5_000) {
            rule.onAllNodesWithText("Merge Direct chats into Priority 2").fetchSemanticsNodes().isNotEmpty()
        }
        rule.onNodeWithText("Move up").performClick()
        rule.waitUntil(5_000) { api.puts.size == 1 }
        assertEquals(listOf("mentions", "mine"), putDocument(api).order(KnowledgeSelectionPart.Messages)[0])
        assertEquals(listOf("dm"), putDocument(api).order(KnowledgeSelectionPart.Messages)[1])
        assertEquals(listOf("group"), putDocument(api).order(KnowledgeSelectionPart.Messages)[2])
    }

    @Test fun pickerUsesPlainChatKindNamesAndOmitsUnknownKinds() {
        val candidates = listOf("dm", "group", "bot", "official", "system").mapIndexed { index, kind ->
            KnowledgeSelectionCandidate(id = kind, name = "Chat $index", kind = kind, source = "lark", read = "whole")
        }
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                KnowledgeChatPickerContent(
                    sources = listOf(HubConnector("lark", "Lark")), source = "lark",
                    candidates = KnowledgeSelectionCandidates(source = "lark", chats = candidates),
                    loading = false, unavailable = false, error = null,
                    onSource = {}, onCandidate = {}, onRetry = {},
                )
            }
        }
        for (title in listOf("Direct chats", "Group chats", "Bots and apps", "Official accounts")) {
            assertTrue(title, rule.onAllNodesWithText(title, substring = true).fetchSemanticsNodes().isNotEmpty())
        }
        assertTrue(rule.onAllNodesWithText("Kind:", substring = true).fetchSemanticsNodes().isEmpty())
        assertTrue(rule.onAllNodesWithText("system", substring = true).fetchSemanticsNodes().isEmpty())
    }

    @Test fun versionTwoDocumentShowsAPlainNoticeWithoutControlsOrRetry() {
        val api = FakeSelection(JsonObject(KnowledgeSelectionDocument.defaults().root + ("version" to JsonPrimitive(2))))
        show(api)
        rule.onNodeWithText("This version of the app can't edit this hub's knowledge selection. Update the app to make changes.").assertExists()
        rule.onNodeWithText("Add chat…").assertDoesNotExist()
        rule.onNodeWithText(RuntimeEnvironment.getApplication().resources.getString(R.string.settings_retry_read)).assertDoesNotExist()
        assertTrue(api.puts.isEmpty())
    }

    @Test fun transportFailureKeepsTheReadErrorAndRetry() {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root).apply {
            readFailure = HubFailure(HubError.Timeout)
        }
        show(api)
        rule.onNodeWithText("Couldn't read knowledge selection:", substring = true).assertExists()
        rule.onNodeWithText(RuntimeEnvironment.getApplication().resources.getString(R.string.settings_retry_read)).assertIsEnabled()
    }

    @Test fun perItemSettingsHaveNoPriorityMovement() {
        val item = KnowledgeSelectionItem.newChat("source", "one", "One")
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().setItem(item).root)
        show(api)
        val actions = rule.onNodeWithTag("knowledge-item-one").fetchSemanticsNode()
            .config.getOrNull(SemanticsActions.CustomActions).orEmpty()
        assertTrue(actions.isEmpty())
        rule.onNodeWithTag("knowledge-priority-items-0").assertDoesNotExist()
    }

    @Test fun aFutureRuleUsesItsOwnNameInsteadOfDoneChats() {
        val base = KnowledgeSelectionDocument.defaults()
        val messages = base.root.getValue("messages").jsonObject
        val rules = messages.getValue("rules").jsonObject
        val order = JsonArray(base.order(KnowledgeSelectionPart.Messages).map { level ->
            JsonArray(level.map(::JsonPrimitive))
        } + listOf(JsonArray(listOf(JsonPrimitive("future_rule")))))
        val document = KnowledgeSelectionDocument(JsonObject(base.root + ("messages" to JsonObject(messages +
            ("rules" to JsonObject(rules + ("future_rule" to JsonObject(mapOf("on" to JsonPrimitive(true)))))) +
            ("order" to order)
        ))))
        val api = FakeSelection(document.root)
        show(api)
        rule.onNodeWithText("future_rule").assertExists()
        assertEquals(1, rule.onAllNodesWithText("Done chats").fetchSemanticsNodes().size)
    }

    @Test fun ruleSheetValidatesDaysBeforeSending() {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root)
        show(api)
        rule.onNodeWithTag("knowledge-rule-messages-mentions").performClick()
        rule.textField("knowledge-rule-days").performTextInput("0")
        rule.onNodeWithTag("knowledge-rule-save").assertIsNotEnabled()
        assertTrue(api.puts.isEmpty())
        rule.textField("knowledge-rule-days").performTextClearance()
        rule.textField("knowledge-rule-days").performTextInput("30")
        rule.textField("knowledge-rule-attachment-days").performTextInput("10")
        rule.onNodeWithTag("knowledge-rule-save").performClick()
        rule.waitUntil(5_000) { api.puts.size == 1 }
        val rule = putDocument(api).rule(KnowledgeSelectionPart.Messages, "mentions")!!
        assertEquals(30, rule.days)
        assertEquals(10, rule.attachmentDays)
        assertEquals(true, rule.includeAll)
    }

    @Test fun readOnlyRefusalLeavesAnOpenSheetDismissible() {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root).apply {
            putFailure = HubFailure(HubError.Server, "read-only device", 403)
        }
        show(api)
        rule.onNodeWithTag("knowledge-rule-messages-mentions").performClick()
        rule.textField("knowledge-rule-days").performTextInput("30")
        rule.onNodeWithTag("knowledge-rule-save").performClick()
        rule.waitUntil(5_000) { api.puts.size == 1 }
        rule.onNodeWithTag("knowledge-rule-save").assertIsNotEnabled()
        rule.onNodeWithText("Cancel").assertIsEnabled().performClick()
        rule.onNodeWithTag("knowledge-rule-save").assertDoesNotExist()
    }

    @Test fun ruleSheetShowsTheHubRefusalInsideTheModal() {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root).apply {
            putFailure = HubFailure(HubError.Server, "days must be from 1 to 36500", 400)
        }
        show(api)
        rule.onNodeWithTag("knowledge-rule-messages-mentions").performClick()
        rule.textField("knowledge-rule-days").performTextInput("30")
        rule.onNodeWithTag("knowledge-rule-save").performClick()
        rule.waitUntil(5_000) { api.puts.size == 1 }
        rule.onNodeWithTag("knowledge-rule-problem").assertExists()
        rule.onNodeWithText("Cancel").assertIsEnabled()
    }

    @Test fun sourceFailureOffersAWorkingRetry() {
        var retries = 0
        val res = RuntimeEnvironment.getApplication().resources
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                KnowledgeChatPickerContent(
                    sources = null, source = null, candidates = null, loading = false, unavailable = false, error = null,
                    sourceError = ResText.of(R.string.settings_knowledge_selection_picker_failed, "offline"),
                    onRetrySources = { retries++ }, onSource = {}, onCandidate = {}, onRetry = {},
                )
            }
        }
        rule.onNodeWithText(res.getString(R.string.settings_retry_read)).performClick()
        assertEquals(1, retries)
    }

    @Test fun dismissingADocumentPickerCancelsItsLateDetailCompletion() {
        val gate = CompletableDeferred<Unit>()
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root).apply {
            documentList = KnowledgeList(items = listOf(KnowledgeEntry(kind = "docs", id = "kb-file", name = "Plan")))
            detail = KnowledgeDetail(kind = "docs", id = "kb-file", path = "docs/kb-file.md", meta = JsonObject(mapOf(
                "source" to JsonPrimitive("drive"), "id" to JsonPrimitive("doc-1"),
            )))
            detailGate = gate
        }
        show(api)
        rule.onNodeWithText("Add document…").performClick()
        rule.onNodeWithText("Plan").performClick()
        rule.waitUntil(5_000) { api.detailReads > 0 }
        rule.onNodeWithText("Cancel").performClick()
        rule.waitForIdle()
        assertTrue(rule.onAllNodesWithText("Plan").fetchSemanticsNodes().isEmpty())
        gate.complete(Unit)
        rule.waitForIdle()
        val title = RuntimeEnvironment.getApplication().resources.getString(R.string.settings_knowledge_selection_document_setting)
        assertTrue(rule.onAllNodesWithText(title).fetchSemanticsNodes().isEmpty())
    }

    @Test fun readOnlyPickerChoicesAreDisabled() {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                KnowledgeChatPickerContent(
                    sources = listOf(HubConnector("lark", "Lark")), source = "lark",
                    candidates = KnowledgeSelectionCandidates(source = "lark", chats = listOf(
                        KnowledgeSelectionCandidate(id = "one", name = "One", kind = "group", source = "lark", read = "whole"),
                    )), loading = false, unavailable = false, error = null, enabled = false,
                    onSource = {}, onCandidate = {}, onRetry = {},
                )
            }
        }
        rule.onNodeWithTag("knowledge-candidate-one").assertIsNotEnabled()
    }

    @Test fun readOnlyRollsBackAndDisablesEveryEditingEntryPoint() {
        val api = FakeSelection(KnowledgeSelectionDocument.defaults().root).apply {
            putFailure = HubFailure(HubError.Server, "read-only device", 403)
        }
        show(api)
        rule.onNodeWithTag("knowledge-rule-messages-mentions").performClick()
        rule.textField("knowledge-rule-days").performTextInput("30")
        rule.onNodeWithTag("knowledge-rule-save").performClick()
        rule.waitUntil(5_000) { api.puts.size == 1 }
        rule.onNodeWithText("Cancel").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("This device can read knowledge selection but can't change it. Use a device with full access to make changes.").assertExists()
        rule.onNodeWithTag("knowledge-rule-messages-mentions").assertIsNotEnabled()
        rule.onNodeWithText("Add chat…").assertIsNotEnabled()
        rule.onNodeWithText("Add document…").assertIsNotEnabled()
        rule.onNodeWithTag("knowledge-priority-messages-0").assertIsNotEnabled()
        val actions = rule.onNodeWithTag("knowledge-rule-messages-dm").fetchSemanticsNode()
            .config[SemanticsActions.CustomActions]
        assertTrue(actions.isEmpty())
        rule.onNodeWithTag("knowledge-rule-messages-mentions").performClick()
        rule.waitForIdle()
        assertEquals(1, api.puts.size)
    }
}

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w360dp-h3000dp-xhdpi")
class KnowledgeSelectionEntryTest {
    @get:Rule val rule = createComposeRule()

    private fun api(oldHub: Boolean) = object : KnowledgeSelection {
        override val changes: Flow<KnowledgeSelectionChange> = emptyFlow()
        var reads = 0
        override suspend fun selection(): JsonObject {
            reads++
            if (oldHub) throw KnowledgeSelectionUnsupported()
            return KnowledgeSelectionDocument.defaults().root
        }
        override suspend fun put(document: JsonObject) = document
        override suspend fun candidates(source: String) = KnowledgeSelectionCandidates(source = source)
        override suspend fun documents() = KnowledgeList()
        override suspend fun document(id: String): KnowledgeDetail = error("not used")
    }

    private fun show(api: KnowledgeSelection, page: String = "settings") {
        val res = RuntimeEnvironment.getApplication().resources
        val services = demoSettingsServices(res).copy(demo = false, knowledgeSelection = api)
        rule.setContent { PersonaTheme(reduceMotion = true) { SettingsScreen(services, initialPage = page) } }
    }

    @Test fun oldHubHidesTheRealSettingsEntryAndSearchResult() {
        val api = api(oldHub = true)
        show(api)
        rule.waitUntil(5_000) { api.reads > 0 }
        rule.waitForIdle()
        rule.textField("settings-search").performTextInput("retention")
        rule.waitForIdle()
        assertTrue(rule.onAllNodesWithText("Knowledge selection").fetchSemanticsNodes().isEmpty())
    }

    @Test fun supportedHubShowsTheSameEntryInSearch() {
        val api = api(oldHub = false)
        show(api)
        rule.waitUntil(5_000) { api.reads > 0 }
        rule.waitForIdle()
        rule.textField("settings-search").performTextInput("retention")
        rule.waitUntil(5_000) { rule.onAllNodesWithText("Knowledge selection").fetchSemanticsNodes().size == 1 }
    }

    @Test fun switchingOnlineHubsReprobesAvailability() {
        val supported = api(oldHub = false)
        val old = api(oldHub = true)
        val res = RuntimeEnvironment.getApplication().resources
        val first = demoSettingsServices(res).copy(demo = false, knowledgeSelection = supported, hubAddress = "https://one.example.com")
        val second = demoSettingsServices(res).copy(demo = false, knowledgeSelection = old, hubAddress = "https://two.example.com")
        var switchHub: () -> Unit = {}
        rule.setContent {
            val services = remember { mutableStateOf(first) }
            switchHub = { services.value = second }
            PersonaTheme(reduceMotion = true) { SettingsScreen(services.value) }
        }
        rule.waitUntil(5_000) { supported.reads > 0 }
        rule.runOnUiThread { switchHub() }
        rule.waitForIdle()
        rule.waitUntil(5_000) { old.reads > 0 }
        rule.textField("settings-search").performTextInput("retention")
        rule.waitForIdle()
        assertTrue(rule.onAllNodesWithText("Knowledge selection").fetchSemanticsNodes().isEmpty())
    }

    @Test fun directOldHubPageShowsTheNoticeInsteadOfAnEditor() {
        val api = api(oldHub = true)
        show(api, page = "knowledge-selection")
        rule.waitUntil(5_000) {
            rule.onAllNodesWithText("This hub doesn't have knowledge selection yet. Update the hub, then choose here what it keeps.")
                .fetchSemanticsNodes().isNotEmpty()
        }
        assertTrue(rule.onAllNodesWithText("Add chat…").fetchSemanticsNodes().isEmpty())
    }
}
