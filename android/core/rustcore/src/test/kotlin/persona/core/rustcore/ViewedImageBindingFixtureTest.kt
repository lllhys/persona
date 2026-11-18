package persona.core.rustcore

import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

/** Real Kotlin/JNI viewed-image leaf replay, with expected values from the shared corpus and checked-in iOS overlay. */
class ViewedImageBindingFixtureTest {
    private val root: File = generateSequence(File(".").canonicalFile) { it.parentFile }
        .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
        ?: error("Cannot locate the repository")
    private val fixtures = root.resolve("spec/fixtures/lines")

    private fun replay(input: JsonObject): JsonElement {
        val output = NativeBridge.projectViewedImages(input.toString())
        assertTrue("JNI returned no viewed-image line; turns=${NativeBridge.projectTurns(input.toString())}", output.isNotEmpty())
        return Json.parseToJsonElement(output)
    }

    private fun assertLineEqual(label: String, expected: JsonElement, actual: JsonElement) {
        assertEquals(label, expected, actual)
    }

    @Test fun replayToolImagesWithTheCurrentAppleOverlay() {
        val case = fixture("tool_images_belong_to_call")
        val input = case.getValue("input").jsonObject
        val actual = replay(input)
        val overlay = Json.parseToJsonElement(fixtures.resolve("ios/differences.json").readText()).jsonObject
        val entry = overlay.getValue("cases").jsonArray.map { it.jsonObject }.single {
            it.getValue("name").jsonPrimitive.content == "tool_images_belong_to_call"
        }
        val expected = entry.getValue("replace").jsonObject.getValue("lines").jsonArray.map { it.jsonObject }.single {
            it.getValue("type").jsonPrimitive.content == "viewed"
        }
        assertLineEqual("tool_images_belong_to_call/current iOS viewed", expected, actual)
        assertTrue("the corpus still needs its Apple overlay", overlay.getValue("turn_wake_state").jsonObject.containsKey("drop"))
        val withoutPatch = case.getValue("expected").jsonObject.getValue("lines").jsonArray.map { it.jsonObject }.single {
            it.getValue("type").jsonPrimitive.content == "viewed"
        }
        assertNotEquals("the overlay must change the outer line id", withoutPatch, actual)
    }

    @Test fun rejectsMalformedInputWrongExpectationAndNonViewableCalls() {
        assertEquals("", NativeBridge.projectViewedImages("{\"invalid\":true}"))
        assertEquals("", NativeBridge.projectViewedImages("not JSON"))
        val case = fixture("tool_images_belong_to_call")
        val actual = replay(case.getValue("input").jsonObject)
        val wrong = buildJsonObject {
            put("id", "vi-2#0")
            put("image_ids", JsonArray(emptyList()))
            put("is_input", false)
            put("type", "viewed")
        }
        assertNotEquals("the negative viewed line differs", wrong, actual)
        val failure = assertThrows("the complete viewed-line comparison accepted a wrong expectation", AssertionError::class.java) {
            assertLineEqual("tool_images_belong_to_call/current iOS viewed", wrong, actual)
        }
        assertTrue(failure.message.orEmpty().contains("viewed"))
        for ((label, update) in mapOf<String, (MutableList<JsonObject>) -> Unit>(
            "running Read without images" to { items ->
                edit(items, "3#0") { use ->
                    use["tool_name"] = kotlinx.serialization.json.JsonPrimitive("Read")
                    use["tool_input"] = buildJsonObject { put("file_path", "picture.png") }
                }
                items.removeAll { it.getValue("kind").jsonPrimitive.content == "tool_result" }
                items.removeAll { it.getValue("kind").jsonPrimitive.content == "image" }
            },
            "failed Read without images" to { items ->
                edit(items, "3#0") { use ->
                    use["tool_name"] = kotlinx.serialization.json.JsonPrimitive("Read")
                    use["tool_input"] = buildJsonObject { put("file_path", "picture.png") }
                }
                items.removeAll { it.getValue("kind").jsonPrimitive.content == "image" }
                edit(items, "4#0") { result -> result["is_error"] = kotlinx.serialization.json.JsonPrimitive(true) }
            },
            "unsupported Read path" to { items ->
                edit(items, "3#0") { use ->
                    use["tool_name"] = kotlinx.serialization.json.JsonPrimitive("Read")
                    use["tool_input"] = buildJsonObject { put("file_path", "document.txt") }
                }
                items.removeAll { it.getValue("kind").jsonPrimitive.content == "image" }
            },
        )) {
            val items = case.getValue("input").jsonObject.getValue("items").jsonArray.map { it.jsonObject }.toMutableList()
            update(items)
            assertEquals("$label has no viewed line", "",
                NativeBridge.projectViewedImages(buildJsonObject { put("items", JsonArray(items)) }.toString()))
        }
        for ((label, status) in listOf("agent running" to "running", "agent failed" to "failed", "agent error" to "error")) {
            val items = case.getValue("input").jsonObject.getValue("items").jsonArray.map { it.jsonObject }.toMutableList()
            edit(items, "3#0") { use ->
                use["tool_name"] = kotlinx.serialization.json.JsonPrimitive("Read")
                use["tool_input"] = buildJsonObject { put("file_path", "/w/a.png") }
            }
            items.removeAll { it.getValue("kind").jsonPrimitive.content == "image" }
            val agentResult = items.first { it.getValue("kind").jsonPrimitive.content == "tool_result" }.toMutableMap()
            agentResult["task"] = buildJsonObject {
                put("kind", "agent")
                put("status", status)
            }
            val ordinaryResult = agentResult.toMutableMap().apply { remove("task") }
            val resultIndex = items.indexOfFirst { it.getValue("kind").jsonPrimitive.content == "tool_result" }
            items[resultIndex] = JsonObject(agentResult)
            items.add(resultIndex + 1, JsonObject(ordinaryResult))
            assertEquals("$label agent status is not hidden by a later ordinary result", "",
                NativeBridge.projectViewedImages(buildJsonObject { put("items", JsonArray(items)) }.toString()))
        }
        val duplicates = case.getValue("input").jsonObject.getValue("items").jsonArray.toMutableList()
        duplicates.add(duplicates[4])
        val deduplicated = replay(buildJsonObject { put("items", JsonArray(duplicates)) })
        assertEquals("one image result appears once", listOf("d:iVBORw0KGgo=12"),
            deduplicated.jsonObject.getValue("image_ids").jsonArray.map { it.jsonPrimitive.content })
        val erroredItems = case.getValue("input").jsonObject.getValue("items").jsonArray.map { it.jsonObject }.toMutableList()
        edit(erroredItems, "4#0") { result -> result["is_error"] = kotlinx.serialization.json.JsonPrimitive(true) }
        assertEquals("an actual image result remains visible when the tool failed", listOf("d:iVBORw0KGgo=12"),
            replay(buildJsonObject { put("items", JsonArray(erroredItems)) }).jsonObject
                .getValue("image_ids").jsonArray.map { it.jsonPrimitive.content })
    }

    @Test fun identityAndNoLineBoundariesMatchApple() {
        val case = fixture("tool_images_belong_to_call")
        val sourceImage = case.getValue("input").jsonObject.getValue("items").jsonArray
            .map { it.jsonObject }.single { it.getValue("kind").jsonPrimitive.content == "image" && it["tool_use_id"]?.jsonPrimitive?.contentOrNull == "call_a" }
        fun withImage(image: JsonObject): JsonElement {
            val items = case.getValue("input").jsonObject.getValue("items").jsonArray.map { it.jsonObject }.toMutableList()
            val index = items.indexOfFirst { it.getValue("kind").jsonPrimitive.content == "image" && it["tool_use_id"]?.jsonPrimitive?.contentOrNull == "call_a" }
            items[index] = image
            return replay(buildJsonObject { put("items", JsonArray(items)) })
        }
        val ref = "m1/s1/" + "x".repeat(60)
        val refImage = sourceImage.toMutableMap()
        refImage["image"] = buildJsonObject {
            put("mime", "image/png")
            put("data", "")
            put("ref", ref)
        }
        assertEquals("a lazy ref is never truncated or suffixed", listOf("d:$ref"),
            withImage(JsonObject(refImage)).jsonObject.getValue("image_ids").jsonArray.map { it.jsonPrimitive.content })
        val metadataImage = sourceImage.toMutableMap()
        metadataImage["image"] = buildJsonObject {
            put("mime", "image/png")
            put("data", "ignored")
            put("size", 900)
            put("width", 16)
            put("height", 10)
        }
        assertEquals("size metadata wins over inline identity", listOf("s:image/png:900:16x10"),
            withImage(JsonObject(metadataImage)).jsonObject.getValue("image_ids").jsonArray.map { it.jsonPrimitive.content })
        val unicodeImage = sourceImage.toMutableMap()
        unicodeImage["image"] = buildJsonObject {
            put("mime", "image/png")
            put("data", "é".repeat(64))
        }
        assertEquals("inline suffix counts Swift characters, not UTF-8 bytes", listOf("d:${"é".repeat(64)}64"),
            withImage(JsonObject(unicodeImage)).jsonObject.getValue("image_ids").jsonArray.map { it.jsonPrimitive.content })
        val combining = "é".repeat(64)
        unicodeImage["image"] = buildJsonObject {
            put("mime", "image/png")
            put("data", combining)
        }
        assertEquals("inline identity counts extended grapheme clusters", listOf("d:${combining}64"),
            withImage(JsonObject(unicodeImage)).jsonObject.getValue("image_ids").jsonArray.map { it.jsonPrimitive.content })
        unicodeImage["image"] = buildJsonObject {
            put("mime", "image/png")
            put("data", combining)
            put("width", 4)
            put("height", 3)
        }
        assertEquals("derived size counts extended grapheme clusters", listOf("s:image/png:48:4x3"),
            withImage(JsonObject(unicodeImage)).jsonObject.getValue("image_ids").jsonArray.map { it.jsonPrimitive.content })
        assertEquals("tool-call-less input has no viewed line", "", NativeBridge.projectViewedImages(buildJsonObject {
            put("items", JsonArray(emptyList()))
        }.toString()))
    }

    @Test fun foldedImagesUseTheExistingFoldOwnerAndAppleIdentityRules() {
        val case = fixture("fold_counts_tools_and_thoughts")
        val images = JsonArray(listOf(
            foldImage("5#0", "a", "/w/shot.png", 100, 4, 3),
            foldImage("8#0", "b", null, 900, 16, 10, tool = "mcp__persona__computer_state", source = "screenshot"),
            foldImage(null, "d", "/w/big.JPG", null, null, null),
            foldImage("20#0", "g", null, 300, 8, 5, tool = "mcp__persona__browser_look", source = "browser"),
        ))
        val input = withFoldImages(case, images)
        val actual = replay(input)
        // ViewedImagesTests.aFoldGivesTheSameStripAsItsWork states these IDs independently of this Rust projection.
        val expected = viewed("vi-1#0", listOf(
            "p:/w/shot.png", "s:image/png:900:16x10", "p:/w/big.JPG", "s:image/png:300:8x5",
        ))
        assertLineEqual("fold_counts_tools_and_thoughts/folded Apple strip", expected, actual)
        val owner = Json.parseToJsonElement(NativeBridge.projectTurns(input.toString())).jsonArray.single().jsonObject
        assertTrue("the existing turn projection owns the folded step",
            owner.getValue("ordered_ids").jsonArray.map { it.jsonPrimitive.content }.contains("tools-fold:2#0"))
        assertNotEquals("the viewed owner is the turn user, not the fold item",
            viewed("vi-fold:2#0", listOf(
                "p:/w/shot.png", "s:image/png:900:16x10", "p:/w/big.JPG", "s:image/png:300:8x5",
            )), actual)

        val fallback = JsonArray(listOf(
            foldImage("5#0", "a", null, null, 4, 3),
            foldImage(null, "call_b", null, 0, 16, 10),
        ))
        assertLineEqual("item_id precedes tool_use_id when path and valid size are absent",
            viewed("vi-1#0", listOf("i:5#0", "i:call_b")), replay(withFoldImages(case, fallback)))
        val repeated = JsonArray(listOf(images[0], images[0], images[1], images[2]))
        assertLineEqual("folded image identities keep first-seen order and appear once",
            viewed("vi-1#0", listOf("p:/w/shot.png", "s:image/png:900:16x10", "p:/w/big.JPG")),
            replay(withFoldImages(case, repeated)))
        assertEquals("a fold without images has no viewed line", "",
            NativeBridge.projectViewedImages(case.getValue("input").jsonObject.toString()))
        val failure = assertThrows("a wrong folded expected object is rejected", AssertionError::class.java) {
            assertLineEqual("folded Apple strip", viewed("vi-1#0", listOf("i:5#0")), actual)
        }
        assertTrue(failure.message.orEmpty().contains("folded Apple strip"))
    }

    @Test fun foldAndToolImagesShareOrderAndIdentityAcrossTheWorkGroup() {
        val toolCase = fixture("tool_images_belong_to_call")
        val items = toolCase.getValue("input").jsonObject.getValue("items").jsonArray.map { it.jsonObject }.toMutableList()
        edit(items, "3#0") { use ->
            use["tool_name"] = kotlinx.serialization.json.JsonPrimitive("Read")
            use["tool_input"] = buildJsonObject { put("file_path", "/w/shot.png") }
        }
        val toolGroup = items.first { it.getValue("kind").jsonPrimitive.content == "tool_use" }
            .getValue("id").jsonPrimitive.content
        val answer = items.removeLast()
        val sourceImage = items.last().toMutableMap()
        sourceImage["image"] = buildJsonObject {
            put("mime", "image/png")
            put("data", "")
            put("ref", "m1/s1/5#0")
            put("size", 100)
            put("width", 4)
            put("height", 3)
        }
        items[items.lastIndex] = JsonObject(sourceImage)
        // Both the Read result and the folded image legitimately identify the same file with p:/w/shot.png.
        val expectedIds = listOf("p:/w/shot.png")
        fun mixedInput(foldFirst: Boolean, includeSizeImage: Boolean): JsonObject {
            val mixed = items.toMutableList()
            val fold = buildJsonObject {
                put("id", "fold:10#0")
                put("seq", 10)
                put("role", "assistant")
                put("kind", "fold")
                put("ts", "2026-01-01T00:00:10Z")
                put("fold", buildJsonObject {
                    put("items", 1)
                    put("tools", 1)
                    put("thoughts", 0)
                    put("through_seq", 11)
                    put("through_id", "11#0")
                    put("until", "2026-01-01T00:00:11Z")
                    put("images", JsonArray(buildList {
                        if (includeSizeImage && foldFirst) {
                            add(foldImage("8#0", "b", null, 900, 16, 10))
                        }
                        add(foldImage("5#0", "a", "/w/shot.png", 100, 4, 3))
                        if (includeSizeImage && !foldFirst) {
                            add(foldImage("8#0", "b", null, 900, 16, 10))
                        }
                    }))
                })
            }
            val insertion = mixed.indexOfFirst { it.getValue("kind").jsonPrimitive.content == "tool_use" }
            assertTrue("the original tool use remains the insertion anchor", insertion > 0)
            mixed.add(if (foldFirst) insertion else mixed.size, fold)
            mixed += JsonObject(answer.toMutableMap().apply {
                this["seq"] = kotlinx.serialization.json.JsonPrimitive(20)
                this["ts"] = kotlinx.serialization.json.JsonPrimitive("2026-01-01T00:00:20Z")
            })
            val input = buildJsonObject { put("items", JsonArray(mixed)) }
            val turns = Json.parseToJsonElement(NativeBridge.projectTurns(input.toString())).jsonArray
            val workTurn = turns.map { it.jsonObject }.single { turn ->
                turn.getValue("user_ids").jsonArray.map { it.jsonPrimitive.content } == listOf("2#0")
            }
            val rawWork = input.getValue("items").jsonArray.map { it.jsonObject }.map { it.getValue("id").jsonPrimitive.content }
            val groupIndex = rawWork.indexOf(toolGroup)
            val foldIndex = rawWork.indexOf("fold:10#0")
            assertTrue("the source run contains the tool and fold in the requested order: $workTurn",
                groupIndex >= 0 && (foldIndex > groupIndex) == !foldFirst)
            assertTrue("the turn retains the run that owns both source steps: $workTurn",
                workTurn.getValue("ordered_ids").jsonArray.map { it.jsonPrimitive.content }
                    .contains(if (foldFirst) "tools-fold:10#0" else "tools-$toolGroup"))
            assertEquals("the existing text/image fixture retains its image-turn owner", "2#0",
                workTurn.getValue("user_ids").jsonArray.single().jsonPrimitive.content)
            return input
        }
        assertLineEqual("ordinary then folded Read image is one path identity",
            viewed("vi-2#0", expectedIds), replay(mixedInput(foldFirst = false, includeSizeImage = false)))
        assertLineEqual("folded then ordinary Read image is one path identity",
            viewed("vi-2#0", expectedIds), replay(mixedInput(foldFirst = true, includeSizeImage = false)))
        val orderedIds = listOf("p:/w/shot.png", "s:image/png:900:16x10")
        assertLineEqual("ordinary then folded images retain first-seen work order",
            viewed("vi-2#0", orderedIds), replay(mixedInput(foldFirst = false, includeSizeImage = true)))
        assertLineEqual("folded images precede the ordinary image in first-seen work order",
            viewed("vi-2#0", listOf("s:image/png:900:16x10", "p:/w/shot.png")),
            replay(mixedInput(foldFirst = true, includeSizeImage = true)))

        val inline = toolCase.getValue("input").jsonObject.getValue("items").jsonArray.map { it.jsonObject }.toMutableList()
        edit(inline, "3#0") { use ->
            use["tool_name"] = kotlinx.serialization.json.JsonPrimitive("Read")
            use["tool_input"] = buildJsonObject { put("file_path", "picture.png") }
        }
        val inferred = inline.first { it.getValue("kind").jsonPrimitive.content == "image" && it["tool_use_id"] != null }.toMutableMap()
        inferred["image"] = buildJsonObject {
            put("mime", "image/png")
            put("data", "AAAA")
            put("width", 4)
            put("height", 3)
        }
        val index = inline.indexOfFirst { it.getValue("kind").jsonPrimitive.content == "image" && it["tool_use_id"] != null }
        inline[index] = JsonObject(inferred)
        assertLineEqual("one Read image uses its path before derived size",
            viewed("vi-2#0", listOf("p:picture.png")), replay(buildJsonObject { put("items", JsonArray(inline)) }))
        edit(inline, "3#0") { use ->
            use["tool_name"] = kotlinx.serialization.json.JsonPrimitive("mcp__persona__computer_state")
            use["tool_input"] = buildJsonObject { put("machine", "m1") }
        }
        assertLineEqual("non-Read inline image derives size from its character count",
            viewed("vi-2#0", listOf("s:image/png:3:4x3")), replay(buildJsonObject { put("items", JsonArray(inline)) }))
    }
    @Test fun firstViewedLineStopsAtTheNextQueuedUserSegment() {
        val items = fixture("finished_queued_messages_apart").getValue("input").jsonObject
            .getValue("items").jsonArray.map { it.jsonObject }.toMutableList()
        for ((useId, path) in listOf("a" to "/w/first.png", "b" to "/w/second.png")) {
            edit(items, if (useId == "a") "3#0" else "7#0") { use ->
                use["tool_name"] = kotlinx.serialization.json.JsonPrimitive("Read")
                use["tool_input"] = buildJsonObject { put("file_path", path) }
            }
        }
        items += buildJsonObject {
            put("id", "12#0")
            put("seq", 12)
            put("role", "user")
            put("kind", "tool_result")
            put("tool_use_id", "b")
            put("text", "ok")
        }
        val input = buildJsonObject { put("items", JsonArray(items)) }
        val turn = Json.parseToJsonElement(NativeBridge.projectTurns(input.toString())).jsonArray.first().jsonObject
        val ordered = turn.getValue("ordered_ids").jsonArray.map { it.jsonPrimitive.content }
        assertTrue("the synthetic queued input remains in the same turn with two segments",
            ordered.containsAll(listOf("1#0", "5#0", "tools-3#0", "tools-7#0")))
        assertLineEqual("only the first segment's image belongs to the first viewed line",
            viewed("vi-1#0", listOf("p:/w/first.png")), replay(input))
        val wrongSecondOwner = viewed("vi-5#0", listOf("p:/w/second.png"))
        assertNotEquals("the second segment is not folded into the first owner", wrongSecondOwner, replay(input))

        val imageQueued = items.map { item ->
            if (item.getValue("id").jsonPrimitive.content == "5#0") {
                JsonObject(item.toMutableMap().apply {
                    this["kind"] = kotlinx.serialization.json.JsonPrimitive("image")
                    this.remove("text")
                    this["image"] = buildJsonObject {
                        put("mime", "image/png")
                        put("data", "aGVsbG8=")
                    }
                })
            } else {
                item
            }
        }
        assertLineEqual("a queued image-only user also ends the first segment",
            viewed("vi-1#0", listOf("p:/w/first.png")),
            replay(buildJsonObject { put("items", JsonArray(imageQueued)) }))

        val multiBlock = items.toMutableList()
        val queuedText = multiBlock.single { it.getValue("id").jsonPrimitive.content == "5#0" }
        val queuedImage = buildJsonObject {
            put("id", "5#1")
            put("seq", 5)
            put("role", "user")
            put("kind", "image")
            put("queued", true)
            put("image", buildJsonObject {
                put("mime", "image/png")
                put("data", "aGVsbG8=")
            })
        }
        val queuedIndex = multiBlock.indexOf(queuedText)
        multiBlock.add(queuedIndex + 1, queuedImage)
        val multiBlockInput = buildJsonObject { put("items", JsonArray(multiBlock)) }
        val turnOutput = NativeBridge.projectTurns(multiBlockInput.toString())
        assertTrue("an absent optional fold payload must not reject a queued text+image message", turnOutput.isNotEmpty())
        val turns = Json.parseToJsonElement(turnOutput).jsonArray
        val firstTurn = turns.map { it.jsonObject }.single { it.getValue("id").jsonPrimitive.content == "1#0" }
        val users = firstTurn.getValue("user_ids").jsonArray.map { it.jsonPrimitive.content }
        assertEquals("both queued message blocks stay in the first turn", listOf("1#0", "5#0", "5#1"), users)
        assertLineEqual("queued text+image does not suppress the prior viewed segment",
            viewed("vi-1#0", listOf("p:/w/first.png")), replay(multiBlockInput))
    }
    private fun viewed(id: String, ids: List<String>): JsonObject = buildJsonObject {
        put("id", id)
        put("image_ids", JsonArray(ids.map { kotlinx.serialization.json.JsonPrimitive(it) }))
        put("is_input", false)
        put("type", "viewed")
    }

    private fun foldImage(
        itemId: String?,
        toolUseId: String,
        path: String?,
        size: Int?,
        width: Int?,
        height: Int?,
        tool: String = "Read",
        source: String = "read",
    ): JsonObject = buildJsonObject {
        itemId?.let { put("item_id", it) }
        put("tool_use_id", toolUseId)
        put("tool", tool)
        put("source", source)
        put("mime", "image/png")
        path?.let { put("path", it) }
        size?.let { put("size", it) }
        width?.let { put("width", it) }
        height?.let { put("height", it) }
    }

    private fun withFoldImages(case: JsonObject, images: JsonArray): JsonObject {
        val items = case.getValue("input").jsonObject.getValue("items").jsonArray.map { it.jsonObject }.toMutableList()
        val index = items.indexOfFirst { it.getValue("kind").jsonPrimitive.content == "fold" }
        assertTrue("the source fold remains the mutation target", index >= 0)
        val item = items[index].toMutableMap()
        val fold = item.getValue("fold").jsonObject.toMutableMap()
        fold["images"] = images
        item["fold"] = JsonObject(fold)
        items[index] = JsonObject(item)
        return buildJsonObject { put("items", JsonArray(items)) }
    }

    private fun edit(items: MutableList<JsonObject>, id: String, update: (MutableMap<String, JsonElement>) -> Unit) {
        val index = items.indexOfFirst {
            it.getValue("id").jsonPrimitive.content == id &&
                it.getValue("kind").jsonPrimitive.content in setOf("tool_use", "tool_result")
        }
        assertTrue("mutation target $id is a tool call or result", index >= 0)
        val target = items[index].toMutableMap()
        update(target)
        items[index] = JsonObject(target)
    }

    private fun fixture(name: String): JsonObject = requireNotNull(fixtures.listFiles { file -> file.isFile && file.extension == "json" })
        .flatMap { Json.parseToJsonElement(it.readText()).jsonObject.getValue("cases").jsonArray }
        .map { it.jsonObject }.first { it.getValue("name").jsonPrimitive.content == name }
}
