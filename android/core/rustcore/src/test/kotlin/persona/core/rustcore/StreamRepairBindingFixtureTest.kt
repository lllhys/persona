package persona.core.rustcore

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

/** Real Kotlin/JNI replay for bounded StreamRepair, with independently stated Apple expectations. */
class StreamRepairBindingFixtureTest {
    private fun replay(text: String): JsonObject {
        val input = buildJsonObject { put("text", text) }.toString()
        val output = NativeBridge.projectStreamRepair(input)
        assertTrue("JNI returned no StreamRepair projection", output.isNotEmpty())
        return Json.parseToJsonElement(output) as JsonObject
    }

    private fun expected(repaired: String, end: String): JsonObject = buildJsonObject {
        put("repaired", repaired)
        put("end", end)
    }

    @Test fun fenceRepairAndEndMatchTheAppleSourceCases() {
        // StreamRepairTests.closesAnOpenCodeFenceWithItsOwnMarker / holdsAFencesOpeningLineBack... / tellsWhereTheTextEnds.
        val repairCases = listOf(
            "text\n```ts\nconst a" to "text\n```ts\nconst a\n```",
            "~~~~\nx\n" to "~~~~\nx\n~~~~",
            "```\na\n```\ndone" to "```\na\n```\ndone",
            "看这段：\n```swi" to "看这段：\n",
            "```mermaid" to "",
            "看这段：\n```swift\n" to "看这段：\n```swift\n```",
            "```\nlet a = 1\n``" to "```\nlet a = 1\n```",
            "````\nx\n```" to "````\nx\n````",
            "````\nx\n```\n" to "````\nx\n```\n````",
            "prefix\n````\nx\n```\n" to "prefix\n````\nx\n```\n````",
            "```\n**x [a](b" to "```\n**x [a](b\n```",
            "```\n-" to "```\n-\n```",
            "1. 安装依赖：\n\n   ```bash\n   npm install" to "1. 安装依赖：\n\n   ```bash\n   npm install\n   ```",
            "1. 安装依赖：\n   ```bash\n   npm install\n" to "1. 安装依赖：\n   ```bash\n   npm install\n   ```",
            "- 运行：\n  ~~~\n  make" to "- 运行：\n  ~~~\n  make\n  ~~~",
            "- 运行：\n  ```\n  make\n  ``" to "- 运行：\n  ```\n  make\n  ```",
            "```\na\n  " to "```\na\n```",
            "这是 **重要" to "这是 **重要**",
            "```swift\nlet a\n```" to "```swift\nlet a\n```",
            "```swift\nlet a\n```\n" to "```swift\nlet a\n```\n",
            "" to "",
        )
        for ((input, repaired) in repairCases) {
            assertEquals("StreamRepair fence case: ${input.replace("\n", "\\n")}", repaired, replay(input).getValue("repaired").jsonPrimitive.content)
        }
        val endCases = listOf(
            "" to "text",
            "这是 **重要" to "text",
            "看这段：\n```swi" to "code",
            "```swift\nlet a" to "code",
            "```swift\nlet a\n```" to "code",
            "```swift\nlet a\n```\n" to "text",
        )
        for ((input, end) in endCases) {
            assertEquals("StreamRepair end case: ${input.replace("\n", "\\n")}", end, replay(input).getValue("end").jsonPrimitive.content)
        }
        println("JNI StreamRepair fence replay: ${repairCases.size} repaired + ${endCases.size} end expectations")
    }

    @Test fun holdsOnlyTheUnfinishedBlockMarkerLineBack() {
        // StreamRepairTests.holdsALastLineOfOnlyMarkersBack: exact Apple inputs and expected strings.
        val cases = listOf(
            "- 第一项\n-" to "- 第一项\n",
            "- 第一项\n- " to "- 第一项\n",
            "1. 第一步\n2." to "1. 第一步\n",
            "1. 第一步\n12" to "1. 第一步\n",
            "正文\n##" to "正文\n",
            "正文\n>" to "正文\n",
            "标题\n---" to "标题\n",
            "- [ ]" to "",
            "**" to "",
            "- 第" to "- 第",
            "2026年" to "2026年",
            "1.5 倍" to "1.5 倍",
            "-\n" to "-\n",
            "正文\n---\n" to "正文\n---\n",
        )
        for ((input, repaired) in cases) {
            assertEquals("StreamRepair markersOnly case: ${input.replace("\n", "\\n")}", repaired,
                replay(input).getValue("repaired").jsonPrimitive.content)
        }
        val wrong = expected("", "text")
        val actual = replay("正文\n##")
        assertNotEquals("a marker-only expected from another input must differ", wrong, actual)
        assertThrows("wrong complete marker expectation is rejected", AssertionError::class.java) {
            assertEquals("StreamRepair markersOnly negative", wrong, actual)
        }
        assertEquals("a digit beyond Apple's nine-digit ordered-item boundary is text", "1234567890",
            replay("1234567890").getValue("repaired").jsonPrimitive.content)
        assertEquals("digits after a marker are not an ordered-item opener", "- 12",
            replay("- 12").getValue("repaired").jsonPrimitive.content)
        assertEquals("a non-ASCII digit is real text", "１２", replay("１２").getValue("repaired").jsonPrimitive.content)
        println("JNI StreamRepair markersOnly replay: ${cases.size} repaired expectations plus boundaries")
    }
    @Test fun preservesAppleWhitespaceAndMarkerPrefixBoundaries() {
        // Scan.string(0..<lastLine.lowerBound) retains the newline after an initial blank line.
        assertEquals("leading blank line before a marker", "\n", replay("\n-").getValue("repaired").jsonPrimitive.content)
        assertEquals("leading blank line before a heading marker", "\n", replay("\n##").getValue("repaired").jsonPrimitive.content)
        // Foundation .whitespaces includes Zs/tab and five ASCII newlines, not NEL or Unicode line/paragraph separators.
        val input = "| a |\n| -\r |\n| x"
        assertEquals("CR inside a delimiter is not a table", expected(input, "text"), replay(input))
        val verticalTab = "| a |\n| -\u000b |\n| x"
        assertEquals("vertical tab inside a delimiter is not a table", expected(verticalTab, "text"), replay(verticalTab))
        assertEquals("tab inside a delimiter is trimmed",
            "| a |\n| -\t |\n| x\n",
            replay("| a |\n| -\t |\n| x\n").getValue("repaired").jsonPrimitive.content)
        val nonBreakingSpace = "| a |\n| - |\n| x"
        assertEquals("non-breaking space is a Foundation whitespace",
            "| a |\n| - |\n| x\n",
            replay(nonBreakingSpace + "\n").getValue("repaired").jsonPrimitive.content)
        val nextLine = "| a |\n| -\u0085 |\n| x"
        assertEquals("NEL is not a Foundation whitespace", expected(nextLine, "text"), replay(nextLine))
        val lineSeparator = "| a |\n| -  |\n| x"
        assertEquals("Unicode line separator is not a Foundation whitespace", expected(lineSeparator, "text"), replay(lineSeparator))
    }

    @Test fun tableRepairAndEndMatchEveryAppleTableAssertion() {
        // Exact repair assertions from holdsATablesHalfWrittenLastRowBack, givesATableHeaderItsDelimiterRowEarly,
        // findsATableRightUnderAParagraphLine, keepsATableInTheListItemItIsIn and findsNoTableInALazyLine.
        val head = "| 文件 | 改动 |\n| --- | --- |\n| a.swift | 刷新 |\n"
        val repairCases = listOf(
            head + "| b.swi" to head,
            head + "| b.swift | 超时 |\n" to head + "| b.swift | 超时 |\n",
            "| 文件 | 改" to "| 文件 | 改",
            "Use a | b\n| not a table" to "Use a | b\n| not a table",
            "| 文件 | 改动 |\n" to "| 文件 | 改动 |\n| --- | --- |",
            "| a | b |\n| --" to "| a | b |\n| --- | --- |",
            "| a | b |\n|" to "| a | b |\n| --- | --- |",
            "## 结果\n| a | b | c |\n" to "## 结果\n| a | b | c |\n| --- | --- | --- |",
            "| a | b |\n| :-- | --: |" to "| a | b |\n| :-- | --: |",
            "| 不是表格\n" to "| 不是表格\n",
            "| x |\n| a | b |\n" to "| x |\n| a | b |\n",
            "**结果：**\n| a | b |\n" to "**结果：**\n| a | b |\n| --- | --- |",
            "**结果：**\n| a | b |\n| --" to "**结果：**\n| a | b |\n| --- | --- |",
            "**结果：**\n| a | b |\n| --- | --- |\n| 1" to "**结果：**\n| a | b |\n| --- | --- |\n",
            "- 项\n\n  | a | b |\n" to "- 项\n\n  | a | b |\n  | --- | --- |",
            "1. 项\n   | a | b |\n   | -" to "1. 项\n   | a | b |\n   | --- | --- |",
            "- 项\n  | a | b |\n  | --- | --- |\n  | 1" to "- 项\n  | a | b |\n  | --- | --- |\n",
            "- 项\n\n  | a | b |\n  | --- | --- |\n  " to "- 项\n\n  | a | b |\n  | --- | --- |\n",
            "- 项\n| a | b |\n" to "- 项\n| a | b |\n",
            "- a\n  - b\n  | x |\n" to "- a\n  - b\n  | x |\n",
            "> 引用\n| a | b |\n" to "> 引用\n| a | b |\n",
            "- 项\n| a | b |\n| --- | --- |\n| 1" to "- 项\n| a | b |\n| --- | --- |\n| 1",
        )
        for ((input, expectedRepair) in repairCases) {
            assertEquals("Apple table repair: ${input.replace("\n", "\\n")}", expectedRepair,
                replay(input).getValue("repaired").jsonPrimitive.content)
        }
        // Exact end assertions in the same five Apple table tests plus the table cases in tellsWhereTheTextEnds.
        val endCases = listOf(
            "**结果：**\n| a | b |\n| --- | --- |\n| 1" to "table",
            "**结果：**\n| a | b |\n| --- | --- |\n| 1 | 2 |\n" to "table",
            "| a | b |\n| --- | --- |\nfoo" to "table",
            "| a | b |\n| --- | --- |\n- 项" to "text",
            "- 项\n  | a | b |\n  | --- | --- |\n  | 1" to "table",
            "- 项\n\n  | a | b |\n  | --- | --- |\n  " to "table",
            "- 项\n\n  | a | b |\n" to "table",
            "- 项\n\n  | a | b |\n  " to "table",
            "- 项\n| a | b |\n| --- | --- |\n| 1" to "text",
            "| a | b |\n| --- | --- |\n| 1" to "table",
            "| a | b |\n| --- | --- |\n| 1 | 2 |\n" to "table",
            "| a | b |\n" to "table",
            "| a | b |\n| -" to "table",
            "| a | b" to "text",
            "| a | b |\n| --- | --- |\n\n后文" to "text",
        )
        for ((input, expectedEnd) in endCases) {
            assertEquals("Apple table end: ${input.replace("\n", "\\n")}", expectedEnd,
                replay(input).getValue("end").jsonPrimitive.content)
        }
        val actual = replay("| a | b |\n| -")
        val wrong = expected("| a | b |\n| --- | --- |", "text")
        assertNotEquals("wrong complete table expectation must differ", wrong, actual)
        assertThrows("wrong complete table expectation is rejected", AssertionError::class.java) {
            assertEquals("StreamRepair table negative", wrong, actual)
        }
        println("JNI StreamRepair table replay: ${repairCases.size} repaired + ${endCases.size} end expectations")
    }

    @Test fun inlineRepairMatchesEveryExplicitAppleInlineAssertion() {
        // Exact Apple repair assertions: closesWhatTheGrowingLineLeavesOpen, closesHalfACodeSpanAndLeavesWhatIsInIt,
        // showsAHalfWrittenImageOrLinkAsItsText and every test under MARK: Inlines except display-math end scanning.
        val cases = listOf(
            "这是 **重要" to "这是 **重要**",
            "an *emph" to "an *emph*",
            "**done** and *also*" to "**done** and *also*",
            "trailing **" to "trailing **",
            "** spaced" to "** spaced",
            "**a\nb" to "**a\nb",
            "* item\n* other" to "* item\n* other",
            "***both" to "***both",
            "a \\*literal" to "a \\*literal",
            "`code **x" to "`code **x`",
            "see ![chart](https://exa" to "see chart",
            "see ![cha" to "see cha",
            "read [the docs](https://exa" to "read the docs",
            "read [the docs](https://example.com) ok" to "read [the docs](https://example.com) ok",
            "这 ~~删除" to "这 ~~删除~~",
            "这 ~~删除~" to "这 ~~删除~~",
            "请运行 `swift te" to "请运行 `swift te`",
            "``a ` b" to "``a ` b``",
            "**粗体里的 `代码" to "**粗体里的 `代码`**",
            "**bold *ital" to "**bold *ital***",
            "**粗 ~~删" to "**粗 ~~删~~**",
            "这是 **重要 " to "这是 **重要**",
            "修改 src/_app.tsx 后重启" to "修改 src/_app.tsx 后重启",
            "改了 _utils.py 里的函数" to "改了 _utils.py 里的函数",
            "an _emph" to "an _emph",
            "**粗 _x" to "**粗 _x**",
            "_a_ 和 *b" to "_a_ 和 *b*",
            "set snake_case_name" to "set snake_case_name",
            "大约 ~5 分钟" to "大约 ~5 分钟",
            "see https://example.com/_next/static" to "see https://example.com/_next/static",
            "<https://example.com/_a" to "<https://example.com/_a",
            "read [x](https://example.com/_a) and *more" to "read [x](https://example.com/_a) and *more*",
            "2 * 3" to "2 * 3",
            "**注意：**后" to "**注意：**后",
            "**注意：**后文 *斜" to "**注意：**后文 *斜*",
            "${'$'}${'$'}\nx^2 + **y" to "${'$'}${'$'}\nx^2 + **y",
            "其中 ${'$'}a_1 *b" to "其中 ${'$'}a_1 *b",
            "其中 ${'$'}a_1${'$'} 和 *b" to "其中 ${'$'}a_1${'$'} 和 *b*",
            "费用 ${'$'}5 和 *b" to "费用 ${'$'}5 和 *b*",
            "参见[文档](https://exa" to "参见文档",
            "**参见[文档](https://exa" to "**参见文档**",
            "图：![架构图]" to "图：架构图",
            "[WIP]" to "[WIP]",
            "正文\n\n    let a = **b" to "正文\n\n    let a = **b",
            "1. 第一项\n\n    接着 **重要" to "1. 第一项\n\n    接着 **重要**",
            "这是 **重要\n" to "这是 **重要\n",
            "-\n" to "-\n",
            "```\na\n  " to "```\na\n```",
            "正文\n\t" to "正文\n",
            "   " to "",
            "a  b" to "a  b",
        )
        for ((input, repaired) in cases) {
            val end = when {
                input.startsWith("```\na") -> "code"
                input == "${'$'}${'$'}\nx^2 + **y" -> "math"
                else -> "text"
            }
            assertEquals("Apple inline repair: ${input.replace("\n", "\\n")}", expected(repaired, end), replay(input))
        }
        println("JNI StreamRepair inline replay: ${cases.size} complete {repaired,end} expectations")
    }

    @Test fun inlineSourceBoundariesRejectMarkersAndUnfinishedFormulas() {
        // Direct consequences of Apple's InlineRepair guards, stated independently here rather than derived from Rust.
        val cases = listOf(
            "*a *b" to "*a *b", // two unmatched same-size openers: cmark does not pair them either
            "a **b ~~c" to "a **b ~~c~~**", // innermost first
            "a **b `c" to "a **b `c`**", // an open code span closes before emphasis around it
            "a **b_" to "a **b_**", // the trailing underscore does not open or close; the bold opener still closes
            "see ![a **b](https://exa" to "see a **b**", // LIFO brackets cut the outer image; its outer bold opener closes
            "read [a *b](https://exa" to "read a *b*", // a nearer emphasis opener closes around the shown link text
            "![alt" to "alt", // alt text still being written
            "其中 \\(a *b" to "其中 \\(a *b", // unfinished inline math is not Markdown
            "其中 ${'$'}${'$'}a *b" to "其中 ${'$'}${'$'}a *b", // inline ${'$'}${'$'} is not a display block
            "其中 ${'$'}a *b" to "其中 ${'$'}a *b", // unmatched formula holds its later markers
            "**后\n文 *斜" to "**后\n文 *斜*", // an opener on a previous line is not synthesized
            "<https://example.com/a\n*b" to "<https://example.com/a\n*b*", // autolink skipping cannot cross a newline
            "见图 ![架构 *说明" to "见图 架构 *说明*", // scalar offsets stay intact when cutting a non-ASCII image
        )
        for ((input, repaired) in cases) {
            assertEquals("Apple inline boundary: ${input.replace("\n", "\\n")}", expected(repaired, "text"), replay(input))
        }
        val actual = replay("an *emph")
        val wrong = expected("an *emph", "text")
        assertNotEquals("wrong inline complete expectation must differ", wrong, actual)
        assertThrows("wrong inline complete expectation is rejected", AssertionError::class.java) {
            assertEquals("StreamRepair inline negative", wrong, actual)
        }
        println("JNI StreamRepair inline boundaries: ${cases.size} complete expectations plus wrong-object rejection")
    }

    @Test fun mathEndMatchesEveryExplicitAppleAssertion() {
        // Exact StreamRepairTests.tellsWhereTheTextEnds math assertions, with repair fixed by StreamRepair.repair.
        val cases = listOf(
            "${'$'}${'$'}\nx^2" to expected("${'$'}${'$'}\nx^2", "math"),
            "${'$'}${'$'}\nx^2\n${'$'}${'$'}" to expected("${'$'}${'$'}\nx^2\n${'$'}${'$'}", "math"),
            "${'$'}${'$'}\nx^2\n${'$'}${'$'}\n" to expected("${'$'}${'$'}\nx^2\n${'$'}${'$'}\n", "text"),
            "\\[\nx" to expected("\\[\nx", "math"),
            "${'$'}${'$'}E=mc^2${'$'}${'$'}" to expected("${'$'}${'$'}E=mc^2${'$'}${'$'}", "math"),
            "质能 ${'$'}E=mc^2${'$'}" to expected("质能 ${'$'}E=mc^2${'$'}", "text"),
        )
        for ((input, complete) in cases) {
            assertEquals("Apple math end: ${input.replace("\n", "\\n")}", complete, replay(input))
        }
        val actual = replay("${'$'}${'$'}\nx^2")
        assertThrows("math must not be downgraded to text", AssertionError::class.java) {
            assertEquals("wrong math end", expected("${'$'}${'$'}\nx^2", "text"), actual)
        }
        assertThrows("inline math in prose must not become display math", AssertionError::class.java) {
            assertEquals("wrong prose math end", expected("质能 ${'$'}E=mc^2${'$'}", "math"), replay("质能 ${'$'}E=mc^2${'$'}"))
        }
        println("JNI StreamRepair math end: ${cases.size} explicit complete {repaired,end} expectations")
    }

    @Test fun mathDisplayBoundariesFollowTheApplePredicate() {
        // Direct consequences of StreamRepair.end, MathMarkdown.display/inlineSpans and withoutIndentSoFar.
        // These complete expected objects are independently transcribed, not generated from Rust output.
        val cases = listOf(
            "\\[\nx\n\\]" to expected("\\[\nx\n\\]", "math"),
            "\\[\nx\n\\]\n" to expected("\\[\nx\n\\]\n", "text"),
            "   ${'$'}${'$'}\n x" to expected("   ${'$'}${'$'}\n x", "math"),
            "    ${'$'}${'$'}\nx" to expected("    ${'$'}${'$'}\nx", "text"),
            "${'$'}${'$'}\nx\n  " to expected("${'$'}${'$'}\nx\n", "math"),
            "    ${'$'}${'$'}\nx\n  " to expected("    ${'$'}${'$'}\nx\n", "text"),
            "${'$'}${'$'}\nx\n${'$'}${'$'}\n  " to expected("${'$'}${'$'}\nx\n${'$'}${'$'}\n", "text"),
            "\\[\nx\n  " to expected("\\[\nx\n", "math"),
            "  ${'$'}${'$'} x^2 ${'$'}${'$'}  " to expected("  ${'$'}${'$'} x^2 ${'$'}${'$'}  ", "math"),
            "${'$'}${'$'}x${'$'}${'$'}" to expected("${'$'}${'$'}x${'$'}${'$'}", "math"),
            "${'$'}${'$'}${'$'}${'$'}" to expected("${'$'}${'$'}${'$'}${'$'}", "text"),
            "${'$'}${'$'} ${'$'}${'$'}" to expected("${'$'}${'$'} ${'$'}${'$'}", "text"),
            "${'$'}${'$'}x${'$'}${'$'}${'$'}${'$'}" to expected("${'$'}${'$'}x${'$'}${'$'}${'$'}${'$'}", "text"),
            "\\[x\\]" to expected("\\[x\\]", "math"),
            "\\[x\n\\]" to expected("\\[x\n\\]", "text"),
            "${'$'}${'$'}x${'$'}${'$'} and *b" to expected("${'$'}${'$'}x${'$'}${'$'} and *b*", "text"),
            "${'$'}x${'$'}" to expected("${'$'}x${'$'}", "math"),
            "  ${'$'} x^2 ${'$'}  " to expected("  ${'$'} x^2 ${'$'}  ", "text"),
            "${'$'}x^2${'$'} " to expected("${'$'}x^2${'$'} ", "math"),
            "${'$'}E=mc^2${'$'} and *b" to expected("${'$'}E=mc^2${'$'} and *b*", "text"),
            "${'$'}5${'$'}" to expected("${'$'}5${'$'}", "math"),
            "${'$'}5-${'$'}10" to expected("${'$'}5-${'$'}10", "text"),
            "价格 ${'$'} 5 ${'$'}" to expected("价格 ${'$'} 5 ${'$'}", "text"),
            "\\${'$'}x\\${'$'}" to expected("\\${'$'}x\\${'$'}", "text"),
            "${'$'}x" to expected("${'$'}x", "text"),
            "${'$'}x${'$'}${'$'}" to expected("${'$'}x${'$'}${'$'}", "text"),
            "${'$'}${'$'}x${'$'}" to expected("${'$'}${'$'}x${'$'}", "text"),
            "${'$'}x${'$'} ${'$'}y${'$'}" to expected("${'$'}x${'$'} ${'$'}y${'$'}", "text"),
            "${'$'}一个月${'$'}" to expected("${'$'}一个月${'$'}", "text"),
            "${'$'}\\text{米}${'$'}" to expected("${'$'}\\text{米}${'$'}", "math"),
            "${'$'}\\text{字} + \\text{符}${'$'}" to expected("${'$'}\\text{字} + \\text{符}${'$'}", "math"),
            "${'$'}x + y${'$'}" to expected("${'$'}x + y${'$'}", "math"),
            "`${'$'}x${'$'}`" to expected("`${'$'}x${'$'}`", "text"),
            "`${'$'}x${'$'}" to expected("`${'$'}x${'$'}`", "text"),
            "`${'$'}x${'$'}` ${'$'}y${'$'}" to expected("`${'$'}x${'$'}` ${'$'}y${'$'}", "text"),
            "\\(x\\)" to expected("\\(x\\)", "math"),
            "\\(x\\) and *b" to expected("\\(x\\) and *b*", "text"),
            "\\(x" to expected("\\(x", "text"),
            "\\(一个月\\)" to expected("\\(一个月\\)", "math"),
            "`\\(x\\)`" to expected("`\\(x\\)`", "text"),
        )
        for ((input, complete) in cases) {
            assertEquals("MathMarkdown display boundary: ${input.replace("\n", "\\n")}", complete, replay(input))
        }
        val atLimit = "${'$'}${"x".repeat(400)}${'$'}"
        val aboveLimit = "${'$'}${"x".repeat(401)}${'$'}"
        assertEquals("400-Character formula is plausible", expected(atLimit, "math"), replay(atLimit))
        assertEquals("401-Character formula is not plausible", expected(aboveLimit, "text"), replay(aboveLimit))
        val combiningAtLimit = "${'$'}${"é".repeat(400)}${'$'}"
        val combiningAboveLimit = "${'$'}${"é".repeat(401)}${'$'}"
        assertEquals("400 extended graphemes are plausible even with 800 scalars",
            expected(combiningAtLimit, "math"), replay(combiningAtLimit))
        assertEquals("401 extended graphemes exceed the limit",
            expected(combiningAboveLimit, "text"), replay(combiningAboveLimit))
        val characterWhitespace = listOf("", " ", " ")
        for (space in characterWhitespace) {
            assertEquals("Character whitespace cannot follow a dollar opener",
                expected("${'$'}${space}x${'$'}", "text"), replay("${'$'}${space}x${'$'}"))
            assertEquals("Character whitespace cannot precede a dollar closer",
                expected("${'$'}x${space}${'$'}", "text"), replay("${'$'}x${space}${'$'}"))
        }
        for (tex in listOf("", "", "\r", "", " ", " ")) {
            val input = "${'$'}${'$'}${tex}${'$'}${'$'}"
            assertEquals("Foundation whitespaces do not trim this display TeX scalar",
                expected(input, "math"), replay(input))
        }
        assertEquals("fence wins over math-looking contents", expected("```\n${'$'}${'$'}\nx\n```", "code"), replay("```\n${'$'}${'$'}\nx\n```"))
        assertThrows("wrong display repaired string is rejected", AssertionError::class.java) {
            assertEquals("wrong display repair", expected("${'$'}${'$'}E=mc^2${'$'}${'$'}!", "math"), replay("${'$'}${'$'}E=mc^2${'$'}${'$'}"))
        }
        println("JNI StreamRepair math display: ${cases.size + 17} source-derived boundaries plus wrong-object rejection")
    }

    @Test fun everythingBeforeTheLastLineStaysAsItWasForEveryApplePrefix() {
        // Exact Apple `reply` source and its `0...unicodeScalars.count` invariant. `lastIndexOf('\n') + 1` implements
        // Swift's lastIndex(of: "\n").map(index(after:)) without changing the expected prefix into an output-derived value.
        for (end in 0..reply.codePointCount(0, reply.length)) {
            val text = reply.substring(0, reply.offsetByCodePoints(0, end))
            val chunkInput = buildJsonObject { put("text", text) }.toString()
            val chunks = Json.parseToJsonElement(NativeBridge.projectStreamChunks(chunkInput)) as JsonObject
            val tail = chunks.getValue("tail").jsonPrimitive.content
            val kept = tail.substring(0, tail.lastIndexOf('\n') + 1)
            val repaired = replay(tail).getValue("repaired").jsonPrimitive.content
            assertTrue("completed prefix changed at scalar $end: ${tail.replace("\n", "\\n")}",
                repaired.startsWith(kept))
        }
        assertEquals("finished reply", reply, replay(reply).getValue("repaired").jsonPrimitive.content)
        assertEquals("finished reply plus newline", reply + "\n",
            replay(reply + "\n").getValue("repaired").jsonPrimitive.content)
        val wrongTail = "上一行\n**重要"
        val wrongPrefix = "上一行\n**重要!"
        val actual = replay(wrongTail).getValue("repaired").jsonPrimitive.content
        assertEquals("wrong-prefix control input repairs as Apple states", "上一行\n**重要**", actual)
        assertFalse("a deliberately wrong retained prefix is rejected", actual.startsWith(wrongPrefix))
        println("JNI StreamRepair Apple prefix replay: ${reply.codePointCount(0, reply.length) + 1} scalar prefixes plus finished controls")
    }

    @Test fun plainOpenFenceMatchesEveryExplicitAppleAssertion() {
        val drawn = listOf("mermaid", "math")
        // StreamRepairTests.drawsAnOpenPictureFenceAsPlainCode: exact input/output pairs, not Rust-derived expected.
        val cases = listOf(
            "看图：\n```mermaid\ngraph TD" to "看图：\n```\ngraph TD",
            "  ~~~ math \nx^2\n" to "  ~~~\nx^2\n",
            "```mermaid\ngraph TD\n```\n" to "```mermaid\ngraph TD\n```\n",
            "```mermaid\na\n```\n```swift\nlet" to "```mermaid\na\n```\n```swift\nlet",
            "```swift\nlet" to "```swift\nlet",
        )
        for ((input, expected) in cases) {
            assertEquals("Apple plainOpenFence assertion: ${input.replace("\n", "\\n")}",
                expected, plainFence(input, drawn))
        }
        assertThrows("wrong complete plain-fence expectation is rejected", AssertionError::class.java) {
            assertEquals("```mermaid\ngraph TD", plainFence("看图：\n```mermaid\ngraph TD", drawn))
        }
        println("JNI StreamRepair plainOpenFence: ${cases.size} explicit Apple assertions")
    }

    @Test fun plainOpenFenceBoundariesFollowTheAppleScannerAndPredicate() {
        val drawn = listOf("mermaid", "math")
        val cases = listOf(
            "" to "",
            "mermaid" to "mermaid",
            "`mermaid`" to "`mermaid`",
            "``mermaid\nx" to "``mermaid\nx",
            "```code```\nx" to "```code```\nx",
            "    ```mermaid\nx" to "    ```\nx",
            "\t~~~math \nx" to "\t~~~\nx",
            "````math \nx" to "````\nx",
            "```mermaid" to "```",
            "```mermaid\n" to "```\n",
            "```mermaid\r" to "```",
            "```mermaid\r\nx" to "```\nx",
            "\r```mermaid\nx" to "\r```\nx",
            "```mermaid \nx" to "```\nx",
            " ```mermaid\nx" to " ```mermaid\nx",
            "```\u000bmermaid\nx" to "```\nx",
            "```mermaid\u0085\nx" to "```\nx",
            "```mermaid \nx" to "```\nx",
            "```mermaid \nx" to "```\nx",
            "```mermaid extra\nx" to "```mermaid extra\nx",
            "```MERMAID\nx" to "```MERMAID\nx",
            "```mermaid\nx\n~~~\n" to "```\nx\n~~~\n",
            "````mermaid\nx\n```\n" to "````\nx\n```\n",
            "```mermaid\nx\n```\n~~~math\ny" to "```mermaid\nx\n```\n~~~\ny",
            "${'$'}${'$'}\n```mermaid\nx" to "${'$'}${'$'}\n```mermaid\nx",
            "```mermaid\n${'$'}${'$'}\nx" to "```\n${'$'}${'$'}\nx",
            "```mermaid\nx" to "```mermaid\nx",
        )
        val predicates = cases.map { (input, _) ->
            if (input == "```mermaid\nx") listOf("math") else drawn
        }
        for ((index, pair) in cases.withIndex()) {
            assertEquals("plain-fence source boundary $index: ${pair.first.replace("\n", "\\n").replace("\r", "\\r")}",
                pair.second, plainFence(pair.first, predicates[index]))
        }
        assertEquals("empty drawn input preserves the info", "```mermaid\nx", plainFence("```mermaid\nx", emptyList()))
        assertEquals("drawn receives the complete trimmed info, not its first word", "```\nx",
            plainFence("```mermaid extra\nx", listOf("mermaid extra")))
        assertEquals("Unicode and extended-grapheme info reaches the predicate intact", "```\nx",
            plainFence("```图 é \nx", listOf("图 é")))
        assertThrows("wrong non-drawn predicate expectation is rejected", AssertionError::class.java) {
            assertEquals("```\nx", plainFence("```swift\nx", drawn))
        }
        println("JNI StreamRepair plainOpenFence: ${cases.size + 4} source-derived boundaries")
    }

    private fun plainFence(text: String, drawn: List<String>): String {
        val input = buildJsonObject {
            put("text", text)
            put("drawn", kotlinx.serialization.json.JsonArray(drawn.map { kotlinx.serialization.json.JsonPrimitive(it) }))
        }.toString()
        val output = NativeBridge.projectPlainOpenFence(input)
        assertTrue("JNI returned no plain-fence projection", output.isNotEmpty())
        return (Json.parseToJsonElement(output) as JsonObject).getValue("text").jsonPrimitive.content
    }

    @Test fun rejectsMalformedInputAndWrongExpectedObjects() {
        assertEquals("", NativeBridge.projectPlainOpenFence("{\"text\":\"```mermaid\"}"))
        assertEquals("", NativeBridge.projectPlainOpenFence("{\"drawn\":[]}"))
        assertEquals("", NativeBridge.projectPlainOpenFence("{\"text\":1,\"drawn\":[]}"))
        assertEquals("", NativeBridge.projectPlainOpenFence("not JSON"))
        assertEquals("", NativeBridge.projectStreamRepair("{\"invalid\":true}"))
        assertEquals("", NativeBridge.projectStreamRepair("not JSON"))
        val input = "text\n```ts\nconst a"
        val actual = replay(input)
        val wrong = expected("text\n```ts\nconst a\n~~~", "text")
        assertNotEquals("wrong fence marker and end must differ", wrong, actual)
        val failure = assertThrows("a wrong complete expected object is rejected", AssertionError::class.java) {
            assertEquals("StreamRepair fence negative", wrong, actual)
        }
        assertTrue(failure.message.orEmpty().contains("StreamRepair fence negative"))
        assertEquals("a trailing unfinished emphasis marker is unchanged", expected("unfinished **", "text"), replay("unfinished **"))
    }

    private companion object {
        // StreamRepairTests.reply, independently transcribed including CJK, links, formulas and the final newline.
        val reply = """
            ## 看完了

            改动没问题，**两处**要注意：`StreamChunks` 的 *缝隙* 和 [文档](https://example.com/docs_v2)。

            - 第一项 ~~删掉~~
            - 第二项
              1. 嵌套 `code`
            > 引用 **粗体**

            | 文件 | 改动 |
            | --- | :-: |
            | a.swift | 刷新 |

            1. 安装依赖：

               ```bash
               npm install
               ```
            **结果：**
            | 项 | 值 |
            | --- | --- |
            | b | 2 |

            ```swift
            let a = "**"
            ```

            ${'$'}${'$'}
            x^2
            ${'$'}${'$'}

            ![图](https://example.com/a.png) 结束 ${'$'}a_1${'$'}。
        """.trimIndent() + "\n"
    }
}
