package persona.feature.settings

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.ExtensionUiEvent
import persona.core.contracts.api.*

// UI version 3 (docs/extensions/extensions.md "Declarative UI", #1850 / #1851): push / pop, search / filter / "Load more", ui.invalidate.

private class ListUi : ExtensionApiStub() {
    var view = ExtensionView(title = "笔记", state = "page=1",
        search = ExtensionViewSearch("搜笔记", action = ExtensionViewAction("search")),
        filter = ExtensionViewFilter("folder", "文件夹", listOf(ExtensionViewChoice("all", "全部"), ExtensionViewChoice("work", "工作")), "all",
            ExtensionViewAction("folder")),
        sections = listOf(ExtensionViewSection(more = ExtensionViewMore(action = ExtensionViewAction("more")), items = listOf(
            ExtensionViewItem("row", title = "周报", key = "n1", action = ExtensionViewAction("open", push = ExtensionViewPush("note=n1", "周报"))),
            ExtensionViewItem("field", name = "draft", kind = "text", value = ""),
        ))))
    var answer: suspend (String, Map<String, String>?) -> ExtensionUiResult = { _, _ -> ExtensionUiResult(view = view) }
    val renders = mutableListOf<String?>()
    val actions = mutableListOf<Triple<String, String?, Map<String, String>?>>()

    override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?): ExtensionView {
        renders += state
        return view
    }
    override suspend fun uiAction(extension: String, ui: String, action: String, context: ExtensionUiContext?, state: String?,
        values: Map<String, String>?): ExtensionUiResult {
        actions += Triple(action, state, values)
        return answer(action, values)
    }
}

class ExtensionUiV3Test {
    @Test fun aPushIsNotSentAndOpensThePageOnTop() = runBlocking {
        val api = ListUi()
        val model = ExtensionViewModel(api, "notes", "list")
        model.load()
        val row = model.state.value.view!!.sections[0].items[0]
        val outcome = model.press(row.action!!)
        assertTrue("A push never reaches the extension", api.actions.isEmpty())
        assertEquals(ExtensionViewPush("note=n1", "周报"), outcome.push)
        // The page on top: the same extension's UI (this one), its state, its title, a level up; none past 8 levels.
        assertEquals(persona.core.navigation.ExtensionUiRoute("notes", "list", "周报", "note=n1", 1),
            ExtensionViewText.pushed("notes", "list", "笔记", 0, outcome.push!!))
        assertEquals("detail", ExtensionViewText.pushed("notes", "list", "笔记", 0, ExtensionViewPush(ui = "detail"))!!.uiId)
        assertEquals("笔记", ExtensionViewText.pushed("notes", "list", "笔记", 0, ExtensionViewPush())!!.title)
        assertNull(ExtensionViewText.pushed("notes", "list", "笔记", ExtensionViewText.MaxDepth - 1, ExtensionViewPush()))
        // another manager's extension (several managers): the page it opens is the same manager's, and a place's too
        assertEquals("m-3", ExtensionViewText.pushed("notes", "list", "笔记", 0, ExtensionViewPush(ui = "detail"), "m-3")!!.manager)
        assertNull(ExtensionViewText.pushed("notes", "list", "笔记", 0, ExtensionViewPush(ui = "detail"))!!.manager)
        assertEquals("m-3", ExtensionViewText.opened(ExtensionUiPlace("notes", "card", manager = "m-3"), ExtensionViewPush()).manager)
        assertNull(ExtensionViewText.opened(ExtensionUiPlace("notes", "card", manager = "assistant"), ExtensionViewPush()).manager)
        // From a card, a machine's section or a launcher's: a whole page of its own.
        val place = ExtensionUiPlace("notes", "today", "今天", "overview_card")
        assertEquals(persona.core.navigation.ExtensionUiRoute("notes", "today", "今天", "x", 0), ExtensionViewText.opened(place, ExtensionViewPush("x")))
    }

    @Test fun aPushedPageStartsFromThePushsState() = runBlocking {
        val api = ListUi()
        val model = ExtensionViewModel(api, "notes", "list", initialState = "note=n1")
        api.view = api.view.copy(state = null)
        model.load()
        assertEquals("note=n1", api.renders.single())
        // Saving there goes back: the answer's pop, and nothing drawn again here (the page is leaving).
        api.answer = { _, _ -> ExtensionUiResult(pop = true, toast = "已保存") }
        val outcome = model.press(ExtensionViewAction("save", submit = true))
        assertTrue(outcome.pop)
        assertEquals("已保存", outcome.toast)
        assertEquals("note=n1", api.actions.single().second)
        assertEquals(1, api.renders.size)
        // An action may push too ("Create" -> the new note's page).
        api.answer = { _, _ -> ExtensionUiResult(push = ExtensionViewPush("note=new")) }
        assertEquals("note=new", model.press(ExtensionViewAction("new")).push!!.state)
        assertEquals(1, api.renders.size)
    }

    @Test fun searchFilterAndMoreLeaveThePageUsable() = runBlocking {
        val api = ListUi()
        // The extension answers with the filter set as asked (its view says what is chosen).
        api.answer = { action, values ->
            ExtensionUiResult(view = if (action == "folder") api.view.copy(filter = api.view.filter!!.copy(value = values!!["folder"])) else api.view)
        }
        val model = ExtensionViewModel(api, "notes", "list")
        model.load()
        model.edit("draft", "正在写")
        model.search("周")
        assertEquals(Triple("search", "page=1", mapOf("q" to "周", "folder" to "all")), api.actions.last())
        assertEquals("周", model.state.value.query)
        assertEquals("What is typed stays", "正在写", model.state.value.values["draft"])
        model.filter("work")
        assertEquals(Triple("folder", "page=1", mapOf("folder" to "work", "q" to "周")), api.actions.last())
        assertEquals("work", model.state.value.view!!.filter!!.value)
        model.search("周报")
        assertEquals(mapOf("q" to "周报", "folder" to "work"), api.actions.last().third)
        val section = model.state.value.view!!.sections[0]
        model.more(0, section)
        assertEquals("more", api.actions.last().first)
        assertNull(model.state.value.loadingMore)
        assertFalse(model.state.value.busy)
    }

    @Test fun loadMoreGoesOnceUntilTheAnswerIsBack() = runBlocking {
        val api = ListUi()
        val model = ExtensionViewModel(api, "notes", "list")
        model.load()
        val gate = CompletableDeferred<Unit>()
        api.answer = { _, _ -> gate.await(); ExtensionUiResult(view = api.view) }
        val section = model.state.value.view!!.sections[0]
        val first = async { model.more(0, section) }
        yield()
        assertEquals(0, model.state.value.loadingMore)
        model.more(0, section)
        assertEquals("The end coming on screen again asks nothing more", 1, api.actions.size)
        gate.complete(Unit)
        first.await()
        assertNull(model.state.value.loadingMore)
    }

    @Test fun aSearchAnswerOvertakenByALaterOneIsDropped() = runBlocking {
        val api = ListUi()
        val model = ExtensionViewModel(api, "notes", "list")
        model.load()
        val slow = CompletableDeferred<Unit>()
        api.answer = { _, values ->
            if (values?.get("q") == "周") slow.await()
            ExtensionUiResult(view = api.view.copy(title = "结果：" + values?.get("q")))
        }
        val early = async { model.search("周") }
        yield()
        model.search("周报")
        assertEquals("结果：周报", model.state.value.view!!.title)
        slow.complete(Unit)
        early.await()
        assertEquals("结果：周报", model.state.value.view!!.title)
    }

    @Test fun anInvalidatedViewIsDrawnAtOnceOnScreenElseWhenItShows() = runBlocking {
        val api = ListUi()
        val model = ExtensionViewModel(api, "notes", "list")
        model.load()
        model.shown(true)
        model.invalidate()
        assertEquals(2, api.renders.size)
        model.shown(false)
        model.invalidate()
        assertEquals("Not on screen: nothing asked yet", 2, api.renders.size)
        model.shown(true)
        assertEquals(3, api.renders.size)
        model.shown(true)
        assertEquals("Once", 3, api.renders.size)
        // A page pushed on it: drawn again when the user comes back.
        model.covered()
        model.shown(false)
        model.shown(true)
        assertEquals(4, api.renders.size)
    }

    @Test fun whichEventsInvalidateWhichViews() {
        assertTrue(ExtensionViewText.invalidates(ExtensionUiEvent("notes", "list", invalidate = true), "notes", "list"))
        assertTrue("No id: all of the extension's", ExtensionViewText.invalidates(ExtensionUiEvent("notes", null, invalidate = true), "notes", "list"))
        assertFalse(ExtensionViewText.invalidates(ExtensionUiEvent("notes", "other", invalidate = true), "notes", "list"))
        assertFalse(ExtensionViewText.invalidates(ExtensionUiEvent("other", null, invalidate = true), "notes", "list"))
        assertFalse("A badge is not an invalidation", ExtensionViewText.invalidates(ExtensionUiEvent("notes", "list"), "notes", "list"))
    }

    @Test fun theNewKindsAndTheirParts() {
        assertTrue(ExtensionViewText.drawable(ExtensionViewItem("markdown", text = "# 标题")))
        assertFalse(ExtensionViewText.drawable(ExtensionViewItem("markdown", text = " ")))
        assertTrue(ExtensionViewText.drawable(ExtensionViewItem("table", columns = listOf(ExtensionTableColumn("名称")))))
        assertFalse(ExtensionViewText.drawable(ExtensionViewItem("table")))
        assertTrue(ExtensionViewText.drawable(ExtensionViewItem("empty", title = "还没有笔记")))
        assertTrue(ExtensionViewText.drawable(ExtensionViewItem("loading")))
        assertFalse("No tables on a card", ExtensionViewText.drawableOnCard(ExtensionViewItem("table", columns = listOf(ExtensionTableColumn("名称")))))
        // A table's rows are as wide as its columns.
        val table = ExtensionViewItem("table", columns = listOf(ExtensionTableColumn("名称"), ExtensionTableColumn("大小", "end")),
            rows = listOf(listOf("a"), listOf("b", "2 KB", "extra")))
        assertEquals(listOf(listOf("a", ""), listOf("b", "2 KB")), ExtensionViewText.tableRows(table))
        // Pictures: https only; letters stand in.
        assertNull(ExtensionViewText.picture("http://example.com/a.png"))
        assertEquals("https://example.com/a.png", ExtensionViewText.picture("https://example.com/a.png"))
        assertEquals("周", ExtensionViewText.initials(ExtensionViewItem("row", title = "周报", avatar = ExtensionViewAvatar())))
        assertEquals("AB", ExtensionViewText.initials(ExtensionViewItem("row", title = "周报", avatar = ExtensionViewAvatar(text = "ABC"))))
        // Markdown: pictures become their words, links other than https too.
        assertEquals("看 图 和 [官网](https://example.com) 和 本地",
            ExtensionViewText.safeMarkdown("看 ![图](https://example.com/a.png) 和 [官网](https://example.com) 和 [本地](file:///etc/passwd)"))
        // Keys: the extension's when unique, else by position.
        assertEquals(listOf("a", null, null), ExtensionViewText.itemKeys(listOf(
            ExtensionViewItem("row", key = "a"), ExtensionViewItem("row", key = "a"), ExtensionViewItem("row"))))
        // Icons this app does not know are the extension icon.
        assertEquals(persona.core.designsystem.icons.PhIcons.User, ExtensionIcons.of("person"))
        assertEquals(persona.core.designsystem.icons.PhIcons.List, ExtensionIcons.of("list"))
        assertEquals(persona.core.designsystem.icons.PhIcons.PuzzlePiece, ExtensionIcons.of("rocket"))
    }
}
