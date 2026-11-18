package persona.feature.settings

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.int
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class KnowledgeSelectionDocumentTest {
    private val base = KnowledgeSelectionDocument.defaults()

    @Test fun defaultsMatchTheHub() {
        assertHubValid(base)
        assertEquals(1, base.version)
        assertEquals(listOf(listOf("mentions", "mine"), listOf("dm"), listOf("group"), listOf("bot"), listOf("official"), listOf("muted"), listOf("collapsed"), listOf("done")), base.order(KnowledgeSelectionPart.Messages))
        assertEquals(listOf(listOf("mentions", "comment_mentions"), listOf("authored"), listOf("viewed")), base.order(KnowledgeSelectionPart.Documents))
        assertTrue(base.rule(KnowledgeSelectionPart.Messages, "mentions")!!.on)
        assertEquals(true, base.rule(KnowledgeSelectionPart.Messages, "mentions")!!.includeAll)
        assertEquals(50, base.rule(KnowledgeSelectionPart.Messages, "mine")!!.context)
        assertTrue(base.rule(KnowledgeSelectionPart.Messages, "dm")!!.on)
        assertFalse(base.rule(KnowledgeSelectionPart.Messages, "group")!!.on)
        assertTrue(KnowledgeSelectionPart.Documents.rules.all { base.rule(KnowledgeSelectionPart.Documents, it)!!.on })
    }

    @Test fun unknownFieldsAndRuleSurroundTripAndAnUnrelatedEdit() {
        val original = KnowledgeSelectionDocument.parse(UNKNOWN_RICH_JSON)
        assertEquals(Json.parseToJsonElement(UNKNOWN_RICH_JSON), original.root)
        assertEquals(original.root, KnowledgeSelectionDocument.parse(original.encoded()).root)

        val edited = original.setRuleOn(KnowledgeSelectionPart.Messages, "dm", false)
        val expected = UNKNOWN_RICH_JSON.setAt(listOf("messages", "rules", "dm", "on"), JsonPrimitive(false))
        assertEquals(expected, edited.root)
        assertEquals("kept-null", edited.root["future_root"]?.jsonObject?.get("kind")?.jsonPrimitive?.content)
        assertEquals(JsonNull, edited.root["future_root"]?.jsonObject?.get("null_value"))
        assertEquals("future_rule", edited.order(KnowledgeSelectionPart.Messages).flatten().last())
        assertEquals("future-rule-value", edited.rule(KnowledgeSelectionPart.Messages, "future_rule")!!.raw["future_rule_field"]?.jsonPrimitive?.content)
        assertHubShapeAllowingFutureFields(edited)
    }

    @Test fun moveLevelSwapsTheWholeLevel() {
        val source = base.regroup(KnowledgeSelectionPart.Messages, "mine", joinPrevious = false)
        val moved = source.moveLevel(KnowledgeSelectionPart.Messages, "mine", -1)
        assertValid(moved)
        assertEquals(listOf("mine"), moved.order(KnowledgeSelectionPart.Messages)[0])
        assertEquals(listOf("mentions"), moved.order(KnowledgeSelectionPart.Messages)[1])
        assertEquals(listOf("dm"), moved.order(KnowledgeSelectionPart.Messages)[2])
    }

    @Test fun moveUpAtTheTopIsANoOp() {
        assertSame(base, base.moveLevel(KnowledgeSelectionPart.Messages, "mentions", -1))
    }

    @Test fun moveDownAtTheBottomIsANoOp() {
        assertSame(base, base.moveLevel(KnowledgeSelectionPart.Messages, "done", 1))
    }

    @Test fun moveMissingRuleIsANoOp() {
        assertSame(base, base.moveLevel(KnowledgeSelectionPart.Messages, "not_a_rule", -1))
    }

    @Test fun joinPreviousAppendsAndRemovesAnEmptyLevel() {
        val edited = base.regroup(KnowledgeSelectionPart.Messages, "dm", joinPrevious = true)
        assertValid(edited)
        assertEquals(listOf("mentions", "mine", "dm"), edited.order(KnowledgeSelectionPart.Messages)[0])
        assertEquals(listOf("group"), edited.order(KnowledgeSelectionPart.Messages)[1])
        assertFalse(edited.order(KnowledgeSelectionPart.Messages).any { it.isEmpty() })
    }

    @Test fun joinPreviousCanTakeOneRuleOutOfASharedLevel() {
        val edited = base.regroup(KnowledgeSelectionPart.Messages, "mine", joinPrevious = false)
            .regroup(KnowledgeSelectionPart.Messages, "dm", joinPrevious = true)
        assertValid(edited)
        assertEquals(listOf("mentions"), edited.order(KnowledgeSelectionPart.Messages)[0])
        assertEquals(listOf("mine", "dm"), edited.order(KnowledgeSelectionPart.Messages)[1])
    }

    @Test fun joinPreviousKeepsTheOtherRulesInTheirOriginalOrder() {
        val shared = base.regroup(KnowledgeSelectionPart.Messages, "dm", joinPrevious = true)
            .moveLevel(KnowledgeSelectionPart.Messages, "mentions", 1)
        val edited = shared.regroup(KnowledgeSelectionPart.Messages, "mine", joinPrevious = true)
        assertEquals(listOf("group", "mine"), edited.order(KnowledgeSelectionPart.Messages)[0])
        assertEquals(listOf("mentions", "dm"), edited.order(KnowledgeSelectionPart.Messages)[1])
        assertEquals(listOf("bot"), edited.order(KnowledgeSelectionPart.Messages)[2])
        assertValid(edited)
    }

    @Test fun joinPreviousAtTheTopIsANoOp() {
        assertSame(base, base.regroup(KnowledgeSelectionPart.Messages, "mentions", joinPrevious = true))
    }

    @Test fun splitPutsTheRuleImmediatelyBelowItsOriginalLevel() {
        val edited = base.regroup(KnowledgeSelectionPart.Messages, "mine", joinPrevious = false)
        assertValid(edited)
        assertEquals(listOf("mentions"), edited.order(KnowledgeSelectionPart.Messages)[0])
        assertEquals(listOf("mine"), edited.order(KnowledgeSelectionPart.Messages)[1])
        assertEquals(listOf("dm"), edited.order(KnowledgeSelectionPart.Messages)[2])
    }

    @Test fun splitASingletonIsANoOp() {
        assertSame(base, base.regroup(KnowledgeSelectionPart.Messages, "dm", joinPrevious = false))
    }

    @Test fun documentLevelsMoveJoinAndSplitToo() {
        val split = base.regroup(KnowledgeSelectionPart.Documents, "comment_mentions", joinPrevious = false)
        assertValid(split)
        val joined = split.regroup(KnowledgeSelectionPart.Documents, "authored", joinPrevious = true)
        assertValid(joined)
        assertEquals(listOf("comment_mentions", "authored"), joined.order(KnowledgeSelectionPart.Documents)[1])
        val moved = joined.moveLevel(KnowledgeSelectionPart.Documents, "viewed", -1)
        assertValid(moved)
        assertEquals(listOf("viewed"), moved.order(KnowledgeSelectionPart.Documents)[1])
    }

    @Test fun togglesChangeOnlyTheNamedRule() {
        val message = base.setRuleOn(KnowledgeSelectionPart.Messages, "group", true)
        assertValid(message)
        assertTrue(message.rule(KnowledgeSelectionPart.Messages, "group")!!.on)
        assertEquals(base.rule(KnowledgeSelectionPart.Messages, "mine"), message.rule(KnowledgeSelectionPart.Messages, "mine"))
        val document = base.setRuleOn(KnowledgeSelectionPart.Documents, "viewed", false)
        assertValid(document)
        assertFalse(document.rule(KnowledgeSelectionPart.Documents, "viewed")!!.on)
    }

    @Test fun onOnlyRuleEditKeepsDaysAndAttachmentDaysAbsent() {
        val messages = base.root.getValue("messages").jsonObject
        val rules = messages.getValue("rules").jsonObject
        val sparse = JsonObject(rules.getValue("dm").jsonObject - "days" - "attachment_days")
        val document = KnowledgeSelectionDocument(JsonObject(base.root + ("messages" to JsonObject(
            messages + ("rules" to JsonObject(rules + ("dm" to sparse)))
        ))))
        val edited = document.setRule(KnowledgeSelectionPart.Messages, "dm", on = false, days = null)
        assertEquals(JsonObject(sparse + ("on" to JsonPrimitive(false))),
            edited.rule(KnowledgeSelectionPart.Messages, "dm")!!.raw)
    }

    @Test fun ruleDaysAndAttachmentDaysSetAndClear() {
        val set = base.setRuleDays(KnowledgeSelectionPart.Messages, "group", 90)
            .setAttachmentDays("group", 14)
        assertValid(set)
        assertEquals(90, set.rule(KnowledgeSelectionPart.Messages, "group")!!.days)
        assertEquals(14, set.rule(KnowledgeSelectionPart.Messages, "group")!!.attachmentDays)
        val cleared = set.setRuleDays(KnowledgeSelectionPart.Messages, "group", null)
            .setAttachmentDays("group", null)
        assertValid(cleared)
        assertNull(cleared.rule(KnowledgeSelectionPart.Messages, "group")!!.days)
        assertNull(cleared.rule(KnowledgeSelectionPart.Messages, "group")!!.attachmentDays)
        assertEquals(JsonNull, cleared.rule(KnowledgeSelectionPart.Messages, "group")!!.raw["days"])
    }

    @Test fun documentDaysSetAndClear() {
        val set = base.setRuleDays(KnowledgeSelectionPart.Documents, "authored", 365)
        assertValid(set)
        assertEquals(365, set.rule(KnowledgeSelectionPart.Documents, "authored")!!.days)
        assertValid(set.setRuleDays(KnowledgeSelectionPart.Documents, "authored", null))
    }

    @Test fun includeAllAndMineContextSetAndClear() {
        val edited = base.setIncludeAll(false).setMineContext(0)
        assertValid(edited)
        assertEquals(false, edited.rule(KnowledgeSelectionPart.Messages, "mentions")!!.includeAll)
        assertEquals(0, edited.rule(KnowledgeSelectionPart.Messages, "mine")!!.context)
        val cleared = edited.setMineContext(null)
        assertValid(cleared)
        assertNull(cleared.rule(KnowledgeSelectionPart.Messages, "mine")!!.context)
    }

    @Test fun itemAddReplaceAndRemoveUseTheWholeStableIdentity() {
        val first = KnowledgeSelectionItem.newChat("lark", "chat-1", "One")
        val sameIdAnotherSource = KnowledgeSelectionItem.newChat("mail", "chat-1", "Mail")
        val added = base.setItem(first).setItem(sameIdAnotherSource)
        assertValid(added)
        assertEquals(2, added.items().size)
        val changed = added.setItem(first.copy(mode = KnowledgeItemMode.All, days = 30))
        assertValid(changed)
        assertEquals(2, changed.items().size)
        assertEquals(KnowledgeItemMode.All, changed.item(first.id)!!.mode)
        assertEquals(30, changed.item(first.id)!!.days)
        assertEquals(KnowledgeItemMode.Rules, changed.item(sameIdAnotherSource.id)!!.mode)
        val removed = changed.removeItem(first.id)
        assertValid(removed)
        assertNull(removed.item(first.id))
        assertNotNull(removed.item(sameIdAnotherSource.id))
    }

    @Test fun removeItemUsesTheWholeIdentityAndPreservesOtherRawItems() {
        val removed = KnowledgeSelectionItem.newChat("lark", "shared", "Remove")
        val sameIdDocument = KnowledgeSelectionItem.newDocument("lark", "shared", "Keep document")
        val sameSourceChat = KnowledgeSelectionItem.newChat("lark", "other", "Keep chat")
        fun raw(item: KnowledgeSelectionItem, marker: String) = JsonObject(item.raw +
            ("future" to JsonPrimitive(marker)) + ("nullable" to JsonNull))
        val removedRaw = raw(removed, "removed")
        val documentRaw = raw(sameIdDocument, "document")
        val chatRaw = raw(sameSourceChat, "chat")
        val document = KnowledgeSelectionDocument(JsonObject(base.root +
            ("items" to JsonArray(listOf(removedRaw, documentRaw, chatRaw)))))
        val edited = document.removeItem(removed.id)
        assertNull(edited.item(removed.id))
        assertEquals(JsonArray(listOf(documentRaw, chatRaw)), edited.root["items"])
    }

    @Test fun itemModesMapToReadStoreAndClearDaysWhenNeeded() {
        val id = KnowledgeItemId("lark", "chat", "one")
        val all = KnowledgeSelectionItem(id, "One", KnowledgeItemMode.All, 20)
        assertEquals(all, base.setItem(all).item(id))
        val rules = base.setItem(all).setItem(all.copy(mode = KnowledgeItemMode.Rules))
        assertValid(rules)
        assertTrue(rules.item(id)!!.read)
        assertFalse(rules.item(id)!!.store)
        assertNull(rules.item(id)!!.days)
        val skip = rules.setItem(rules.item(id)!!.copy(mode = KnowledgeItemMode.Skip, days = 10))
        assertValid(skip)
        assertFalse(skip.item(id)!!.read)
        assertFalse(skip.item(id)!!.store)
        assertNull(skip.item(id)!!.days)
    }

    @Test fun itemUnknownFieldsSurviveModeAndDayEdits() {
        val document = KnowledgeSelectionDocument.parse(UNKNOWN_RICH_JSON)
        val id = KnowledgeItemId("lark", "chat", "chat-1")
        val changed = document.setItem(document.item(id)!!.copy(mode = KnowledgeItemMode.All, days = 45))
        assertEquals("item-future", changed.item(id)!!.raw["future_item"]?.jsonPrimitive?.content)
        assertEquals(45, changed.item(id)!!.days)
        assertHubShapeAllowingFutureFields(changed)
    }

    @Test fun invalidNumbersAreRefusedBeforeADocumentIsProduced() {
        assertThrowsIllegalArgument { base.setRuleDays(KnowledgeSelectionPart.Messages, "dm", 0) }
        assertThrowsIllegalArgument { base.setRuleDays(KnowledgeSelectionPart.Messages, "dm", 36501) }
        assertThrowsIllegalArgument { base.setAttachmentDays("dm", -1) }
        assertThrowsIllegalArgument { base.setMineContext(-1) }
        assertThrowsIllegalArgument { base.setMineContext(201) }
        assertThrowsIllegalArgument { base.setItem(KnowledgeSelectionItem.newChat("lark", "one", "One").copy(mode = KnowledgeItemMode.All, days = 0)) }
    }

    @Test fun ruleSheetsLeaveUntouchedOptionalSpellingsAlone() {
        val rules = base.root.getValue("messages").jsonObject.getValue("rules").jsonObject
        val sparse = KnowledgeSelectionDocument(JsonObject(base.root + ("messages" to JsonObject(
            base.root.getValue("messages").jsonObject + ("rules" to JsonObject(rules + (
                "mentions" to JsonObject(rules.getValue("mentions").jsonObject - "include_all")
            )))
        ))))
        assertSame(sparse, sparse.setRule(KnowledgeSelectionPart.Messages, "mentions", true, null, null, null, null))
        val changed = sparse.setRule(KnowledgeSelectionPart.Messages, "mentions", true, null, null, false, null)
        assertEquals(false, changed.rule(KnowledgeSelectionPart.Messages, "mentions")!!.includeAll)
        assertFalse("include_all" in sparse.rule(KnowledgeSelectionPart.Messages, "mentions")!!.raw)
    }

    @Test fun numberFieldsAcceptOnlyAsciiWholeNumbersInRange() {
        assertNull(knowledgeSelectionNumber(" \n", 1..36500))
        assertEquals(30, knowledgeSelectionNumber(" 30 ", 1..36500))
        assertEquals(0, knowledgeSelectionNumber("0", 0..200))
        assertEquals(36500, knowledgeSelectionNumber("36500", 1..36500))
        listOf("+1", "-1", "1.5", "1e2", "１２", "1 2", "999999999999999999999", "0", "36501").forEach {
            assertThrowsIllegalArgument { knowledgeSelectionNumber(it, 1..36500) }
        }
        assertThrowsIllegalArgument { knowledgeSelectionNumber("201", 0..200) }
    }

    @Test fun anUnchangedItemKeepsItsExactOptionalFields() {
        val id = KnowledgeItemId("lark", "chat", "optional")
        val raw = JsonObject(mapOf(
            "source" to JsonPrimitive(id.source), "kind" to JsonPrimitive(id.kind), "id" to JsonPrimitive(id.id),
            "name" to JsonNull, "read" to JsonPrimitive(true), "store" to JsonPrimitive(false),
            "days" to JsonPrimitive(30), "future" to JsonPrimitive("kept"),
        ))
        val document = KnowledgeSelectionDocument(JsonObject(base.root + ("items" to JsonArray(listOf(raw)))))
        assertSame(document, document.setItem(document.item(id)!!))
        assertEquals(raw, document.item(id)!!.raw)
        val changed = document.setItem(document.item(id)!!.copy(mode = KnowledgeItemMode.Skip))
        assertEquals(JsonNull, changed.item(id)!!.raw["name"])
        assertEquals(JsonNull, changed.item(id)!!.raw["days"])
        assertEquals("kept", changed.item(id)!!.raw["future"]!!.jsonPrimitive.content)
    }

    @Test fun nameOnlyItemEditsKeepUnusedDaysSpellings() {
        val id = KnowledgeItemId("lark", "chat", "one")
        fun document(days: JsonElement?): KnowledgeSelectionDocument {
            val raw = JsonObject(buildMap {
                put("source", JsonPrimitive(id.source)); put("kind", JsonPrimitive(id.kind)); put("id", JsonPrimitive(id.id))
                put("name", JsonPrimitive("Old")); put("read", JsonPrimitive(true)); put("store", JsonPrimitive(false))
                days?.let { put("days", it) }
            })
            return KnowledgeSelectionDocument(JsonObject(base.root + ("items" to JsonArray(listOf(raw)))))
        }
        val numeric = document(JsonPrimitive(30))
        val renamed = numeric.setItem(numeric.item(id)!!.copy(name = "New"))
        assertEquals(30, renamed.item(id)!!.raw["days"]!!.jsonPrimitive.int)
        assertHubValid(renamed)
        val absent = document(null)
        assertFalse("days" in absent.setItem(absent.item(id)!!.copy(name = "New")).item(id)!!.raw)
    }

    @Test fun nullItemsAndHistoryContainersSurviveAnUnrelatedEdit() {
        val document = KnowledgeSelectionDocument(JsonObject(base.root + ("items" to JsonNull) + ("history" to JsonNull)))
        assertTrue(document.items().isEmpty())
        assertHubValid(document)
        val edited = document.setRuleOn(KnowledgeSelectionPart.Messages, "dm", false)
        assertEquals(JsonNull, edited.root["items"])
        assertEquals(JsonNull, edited.root["history"])
    }

    @Test fun nullOptionalMessageRuleFieldsAreValidAndPreserved() {
        val messages = base.root.getValue("messages").jsonObject
        val rules = messages.getValue("rules").jsonObject
        val updatedRules = JsonObject(rules + mapOf(
            "dm" to JsonObject(rules.getValue("dm").jsonObject + ("include_all" to JsonNull)),
            "group" to JsonObject(rules.getValue("group").jsonObject + ("context" to JsonNull)),
        ))
        val document = KnowledgeSelectionDocument(JsonObject(base.root + ("messages" to JsonObject(
            messages + ("rules" to updatedRules)
        ))))
        assertHubValid(document)
        val edited = document.setRuleOn(KnowledgeSelectionPart.Messages, "dm", false)
        assertEquals(JsonNull, edited.rule(KnowledgeSelectionPart.Messages, "dm")!!.raw["include_all"])
        assertEquals(JsonNull, edited.rule(KnowledgeSelectionPart.Messages, "group")!!.raw["context"])
    }

    @Test fun trueNoOpEditsReturnTheSameDocument() {
        assertSame(base, base.setRuleOn(KnowledgeSelectionPart.Messages, "dm", true))
        assertSame(base, base.setRuleDays(KnowledgeSelectionPart.Messages, "dm", null))
        assertSame(base, base.removeItem(KnowledgeItemId("missing", "chat", "missing")))
    }

    @Test fun strictValidationTestHelperRejectsTheHubsExamples() {
        val missing = base.root.withOrder(KnowledgeSelectionPart.Messages, base.order(KnowledgeSelectionPart.Messages).dropLast(1))
        assertStrictHubInvalid(missing)
        val duplicate = base.root.withOrder(KnowledgeSelectionPart.Messages, base.order(KnowledgeSelectionPart.Messages) + listOf(listOf("dm")))
        assertStrictHubInvalid(duplicate)
        val empty = base.root.withOrder(KnowledgeSelectionPart.Messages, base.order(KnowledgeSelectionPart.Messages) + listOf(emptyList()))
        assertStrictHubInvalid(empty)
        val storeWithoutRead = base.setItem(KnowledgeSelectionItem.newChat("lark", "one", "One")).root.let { root ->
            val items = root["items"]!!.jsonArray.toMutableList()
            val item = items[0].jsonObject
            items[0] = JsonObject(item + ("read" to JsonPrimitive(false)) + ("store" to JsonPrimitive(true)))
            JsonObject(root + ("items" to JsonArray(items)))
        }
        assertStrictHubInvalid(storeWithoutRead)
    }

    private fun assertSame(expected: KnowledgeSelectionDocument, actual: KnowledgeSelectionDocument) {
        assertEquals(expected.root, actual.root)
    }

    private fun assertValid(document: KnowledgeSelectionDocument) {
        assertHubShapeAllowingFutureFields(document)
        assertHubValid(document)
    }

    private fun assertThrowsIllegalArgument(block: () -> Unit) {
        try {
            block()
            fail("Expected IllegalArgumentException")
        } catch (_: IllegalArgumentException) {
        }
    }
}

private val KnowledgeSelectionPart.rules: List<String>
    get() = when (this) {
        KnowledgeSelectionPart.Messages -> listOf("mentions", "mine", "dm", "group", "bot", "official", "muted", "collapsed", "done")
        KnowledgeSelectionPart.Documents -> listOf("mentions", "comment_mentions", "authored", "viewed")
    }

private val KnowledgeSelectionDocument.version: Int
    get() = root["version"]!!.jsonPrimitive.int

private fun JsonObject.withOrder(part: KnowledgeSelectionPart, order: List<List<String>>): JsonObject {
    val group = this[part.wire]!!.jsonObject
    val wireOrder = JsonArray(order.map { level -> JsonArray(level.map(::JsonPrimitive)) })
    return JsonObject(this + (part.wire to JsonObject(group + ("order" to wireOrder))))
}

private fun String.setAt(path: List<String>, value: JsonElement): JsonObject {
    fun update(element: JsonElement, remaining: List<String>): JsonElement {
        if (remaining.isEmpty()) return value
        val objectValue = element.jsonObject
        val key = remaining.first()
        return JsonObject(objectValue + (key to update(objectValue.getValue(key), remaining.drop(1))))
    }
    return update(Json.parseToJsonElement(this), path).jsonObject
}

private fun assertStrictHubInvalid(root: JsonObject) {
    try {
        assertHubValid(KnowledgeSelectionDocument(root))
        fail("Expected the strict hub-validation mirror to refuse $root")
    } catch (_: IllegalArgumentException) {
    }
}

private fun assertHubValid(document: KnowledgeSelectionDocument) {
    val root = document.root
    require(root["version"]?.jsonPrimitive?.intOrNull == 1)
    for (part in KnowledgeSelectionPart.entries) {
        val group = root[part.wire]!!.jsonObject
        val rules = group["rules"]!!.jsonObject
        require(rules.keys == part.rules.toSet()) { "${part.wire} rule names differ" }
        val flattened = group["order"]!!.jsonArray.flatMap { level ->
            require(level.jsonArray.isNotEmpty()) { "empty level" }
            level.jsonArray.map { it.jsonPrimitive.content }
        }
        require(flattened.size == part.rules.size)
        require(flattened.toSet() == part.rules.toSet())
        require(flattened.size == flattened.distinct().size)
        for (name in part.rules) {
            val rule = rules.getValue(name).jsonObject
            requireDays(rule["days"])
            if (part == KnowledgeSelectionPart.Messages) {
                requireDays(rule["attachment_days"])
                if (name != "mentions") require(rule["include_all"].let { it == null || it == JsonNull })
                if (name != "mine") require(rule["context"].let { it == null || it == JsonNull })
                rule["include_all"]?.takeIf { it != JsonNull }?.let { require(it.jsonPrimitive.booleanOrNull != null) }
            } else {
                require("include_all" !in rule && "context" !in rule && "attachment_days" !in rule)
            }
            rule["context"]?.let { if (it != JsonNull) require(it.jsonPrimitive.int in 0..200) }
        }
    }
    val seen = mutableSetOf<Triple<String, String, String>>()
    root["items"].nullableArray().forEach { element ->
        val item = element.jsonObject
        val source = item.getValue("source").jsonPrimitive.content
        val kind = item.getValue("kind").jsonPrimitive.content
        val id = item.getValue("id").jsonPrimitive.content
        require(source.isNotEmpty() && id.isNotEmpty())
        require(kind == "chat" || kind == "doc")
        require(seen.add(Triple(source, kind, id))) { "duplicate item" }
        val read = item.getValue("read").jsonPrimitive.boolean
        val store = item.getValue("store").jsonPrimitive.boolean
        require(read || !store) { "store needs read" }
        requireDays(item["days"])
    }
    root["history"].nullableObject().forEach { (source, history) ->
        require(source.isNotEmpty())
        requireDays(history.jsonObject["back_days"])
    }
}

// This is a future-friendly shape check used by the production document. The strict
// assertHubValid above mirrors selection_validate.go and therefore rejects unknown rules.
private fun assertHubShapeAllowingFutureFields(document: KnowledgeSelectionDocument) {
    require(document.root["version"]?.jsonPrimitive?.intOrNull == 1)
    for (part in KnowledgeSelectionPart.entries) {
        val group = document.root[part.wire]!!.jsonObject
        val ruleNames = group["rules"]!!.jsonObject.keys
        require(ruleNames.containsAll(part.rules))
        val order = group["order"]!!.jsonArray.flatMap { level ->
            require(level.jsonArray.isNotEmpty())
            level.jsonArray.map { it.jsonPrimitive.content }
        }
        require(order.size == order.distinct().size)
        require(order.toSet() == ruleNames)
        require(order.containsAll(part.rules))
    }
}

private fun JsonElement?.nullableArray(): List<JsonElement> =
    if (this == null || this == JsonNull) emptyList() else jsonArray

private fun JsonElement?.nullableObject(): Map<String, JsonElement> =
    if (this == null || this == JsonNull) emptyMap() else jsonObject

private fun requireDays(value: JsonElement?) {
    if (value == null || value == JsonNull) return
    require(value.jsonPrimitive.int in 1..36500)
}

private const val UNKNOWN_RICH_JSON = """
{
  "version": 1,
  "future_root": {"kind": "kept-null", "null_value": null},
  "messages": {
    "order": [["mentions", "mine"], ["dm"], ["group"], ["bot"], ["official"], ["muted"], ["collapsed"], ["done"], ["future_rule"]],
    "rules": {
      "mentions": {"on": true, "days": null, "attachment_days": null, "include_all": true, "future_mentions": {"x": 1}},
      "mine": {"on": true, "days": null, "attachment_days": null, "context": 50},
      "dm": {"on": true, "days": null, "attachment_days": null},
      "group": {"on": false, "days": null, "attachment_days": null},
      "bot": {"on": false, "days": null, "attachment_days": null},
      "official": {"on": false, "days": null, "attachment_days": null},
      "muted": {"on": false, "days": null, "attachment_days": null},
      "collapsed": {"on": false, "days": null, "attachment_days": null},
      "done": {"on": false, "days": null, "attachment_days": null},
      "future_rule": {"on": true, "days": null, "attachment_days": null, "future_rule_field": "future-rule-value"}
    },
    "future_messages": [1, null, {"kept": true}]
  },
  "docs": {
    "order": [["mentions", "comment_mentions"], ["authored"], ["viewed"]],
    "rules": {
      "mentions": {"on": true, "days": null},
      "comment_mentions": {"on": true, "days": null},
      "authored": {"on": true, "days": null, "future_doc_rule": null},
      "viewed": {"on": true, "days": null}
    },
    "future_docs": {"kept": "yes"}
  },
  "items": [
    {"source": "lark", "kind": "chat", "id": "chat-1", "name": "One", "read": true, "store": false, "days": null, "future_item": "item-future"}
  ],
  "history": {
    "lark": {"back_days": 365, "future_history": {"kept": null}}
  }
}
"""
