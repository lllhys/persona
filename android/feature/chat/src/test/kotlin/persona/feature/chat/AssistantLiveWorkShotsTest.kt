package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.performScrollTo
import persona.core.chatlist.*
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import coil3.ImageLoader
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.contracts.SessionKey
import persona.core.designsystem.*
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.WorkItem
import java.io.File
import java.time.Instant

/** Real chat line rendering for both conversation types, every style and appearance. Native JVM shots, not device shots. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
class AssistantLiveWorkShotsTest {
    @get:Rule val rule = createComposeRule()
    private fun out() = File("build/assistant-live-work-shots").also { it.mkdirs() }
    private val start = Instant.now().minusSeconds(20)
    private fun at(seq: Int) = start.plusSeconds(seq.toLong()).toString()
    private fun use(seq: Int, name: String, vararg input: Pair<String, String>) = Item("t$seq", seq, role = "assistant",
        kind = "tool_use", toolUseId = "t$seq", toolName = name,
        toolInput = buildJsonObject { input.forEach { (k, v) -> put(k, v) } }, ts = at(seq))
    private fun result(seq: Int, of: Int, failed: Boolean = false) = Item("r$seq", seq, role = "user", kind = "tool_result",
        toolUseId = "t$of", text = if (failed) "not found" else "ok", isError = failed.takeIf { it }, ts = at(seq))
    private fun reply(seq: Int, text: String) = Item("a$seq", seq, role = "assistant", kind = "text", text = text, ts = at(seq))
    private val running = listOf(
        Item("u1", 1, role = "user", kind = "text", text = "Check tomorrow's calendar and sort the tasks.", ts = at(1)),
        reply(2, "I will first check the calendar."),
        use(3, "Bash", "command" to "calendar --day tomorrow"), result(4, 3),
        use(5, "Read", "file_path" to "/notes/todo.md"), result(6, 5),
        use(7, "Grep", "pattern" to "deadline"), result(8, 7, failed = true),
        use(9, "Edit", "file_path" to "/notes/todo.md", "old_string" to "a", "new_string" to "b"),
    )
    private val latest = running + reply(10, "The calendar is checked; I am sorting the tasks.")
    private val ended = latest + result(11, 9) + reply(12, "Two meetings tomorrow. The tasks are sorted by date.")

    @Test fun collapsedToolsAndLatestTextSurviveUpdatesCompletionAndManualTogglesInEveryStyle() =
        rendersTransitionsInEveryStyle(running, latest, ended)

    @Test fun anAutomaticWakeKeepsItsTailToolsExpandableBeforeNewTextInEveryStyle() {
        val resumed = ended + Item("wake13", 13, role = "system", kind = "notice", text = "Background work resumed", wake = true, ts = at(13)) +
            use(14, "Read", "file_path" to "/notes/follow-up.md")
        val newest = resumed + reply(15, "I am checking the follow-up after the background wake.")
        rendersTransitionsInEveryStyle(resumed, newest, newest + result(16, 14) + reply(17, "The background follow-up is complete."),
            initialText = "Two meetings tomorrow. The tasks are sorted by date.",
            latestText = "I am checking the follow-up after the background wake.", finalText = "The background follow-up is complete.",
            lastResult = result(16, 14), snapshotPrefix = "wake-")
    }

    private fun rendersTransitionsInEveryStyle(running: List<Item>, latest: List<Item>, ended: List<Item>,
        initialText: String = "I will first check the calendar.", latestText: String = "The calendar is checked; I am sorting the tasks.",
        finalText: String = "Two meetings tomorrow. The tasks are sorted by date.", lastResult: Item = result(11, 9), snapshotPrefix: String = "") {
        var items by mutableStateOf(running)
        var live by mutableStateOf(true)
        var state by mutableStateOf(WorkOpenState())
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var renderStyle by mutableStateOf(ChatRenderStyle.Assistant)
        var updates by mutableStateOf(emptyMap<String, ChatTextUpdate>())
        val latestId = latest.last { it.kind == "text" && it.role == "assistant" }.id
        fun lines() = ChatTimeline().lines(items, live, state.open).lines // Fresh history projection on every update.
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val context = LocalContext.current
                val loader = remember { ImageLoader.Builder(context).build() }
                DisposableEffect(loader) { onDispose { loader.shutdown() } }
                val lines = remember(items, live, state) { lines() }
                val runningIds = liveWorkIds(lines)
                var previousLive by remember { mutableStateOf(emptySet<String>()) }
                LaunchedEffect(runningIds) {
                    state = state.copy(open = settleWorkOpen(state.open, runningIds, previousLive))
                    previousLive = runningIds
                }
                val groups = lines.filterIsInstance<ChatLine.Work>().associateBy { it.id }
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l)) {
                    for (line in lines) key(line.contentKey) {
                        ChatLineContent(line, SessionKey("computer", "conversation"), loader, recordedDuration = 9,
                            onToggle = { id -> state = toggleWork(state, id).first }, onFold = {}, folding = emptySet(),
                            foldErrors = emptyMap(), onFullText = {}, style = renderStyle, textUpdates = updates,
                            workRunning = (line as? ChatLine.Step)?.let { groups[it.groupId]?.live } == true,
                            lastWorkStep = (line as? ChatLine.Step)?.let { groups[it.groupId]?.steps?.lastOrNull()?.id == it.step.id } == true)
                    }
                }
            }
        }
        fun settle() { Snapshot.sendApplyNotifications(); repeat(3) { rule.mainClock.advanceTimeByFrame() } }
        fun summary() = rule.onNode(hasClickAction() and (hasContentDescription("，已展开", substring = true) or
            hasContentDescription("，已收起", substring = true)))
        fun expanded(expected: Boolean) = summary().assert(hasContentDescription(if (expected) "，已展开" else "，已收起", substring = true))
        fun toolsHidden() = rule.onNodeWithText("deadline", substring = true).assertDoesNotExist()
        fun shoot(name: String, host: String): Int {
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "$snapshotPrefix$host-$name-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            return bitmap.getPixel(2, 2)
        }
        for ((host, hostStyle) in listOf("persona" to ChatRenderStyle.Assistant, "agent" to ChatRenderStyle.Agent)) {
            renderStyle = hostStyle
            for (s in AppStyle.entries) {
                val corners = mutableListOf<Int>()
                for (d in listOf(false, true)) {
                    style = s; dark = d; items = running; live = true; state = WorkOpenState(); updates = emptyMap(); settle()
                    expanded(false); toolsHidden()
                    rule.onNodeWithText(initialText).assertIsDisplayed()
                    val slot = lines().last().contentKey
                    corners += shoot("running", host)

                    items = latest; settle()
                    assertEquals(slot, lines().last().contentKey)
                    rule.onNodeWithText(initialText).assertDoesNotExist()
                    rule.onNodeWithText(latestText).assertIsDisplayed()
                    toolsHidden(); expanded(false)
                    shoot("latest", host)
                    updates = mapOf(latestId to ChatTextUpdate("The newest streamed text replaces the stored snapshot.", streaming = true))
                    settle()
                    rule.onNodeWithText(latestText).assertDoesNotExist()
                    rule.onNodeWithText("The newest streamed text replaces the stored snapshot.").assertIsDisplayed()
                    toolsHidden()

                    // Both directions work during the run; collapse is not overridden by a new tool result.
                    summary().performClick(); settle(); expanded(true)
                    rule.onNodeWithText("deadline", substring = true).assertIsDisplayed()
                    rule.onNodeWithText(initialText).assertIsDisplayed()
                    summary().performClick(); settle(); expanded(false); toolsHidden()
                    items = latest + lastResult; settle(); toolsHidden()
                    summary().performClick(); settle(); expanded(true)

                    // Completion folds even a manually opened running summary, but keeps the same text slot.
                    items = ended; live = false; updates = emptyMap(); settle()
                    rule.onNodeWithText("The newest streamed text replaces the stored snapshot.").assertDoesNotExist()
                    expanded(false); toolsHidden()
                    assertEquals(slot, lines().last().contentKey)
                    rule.onNodeWithText(latestText).assertDoesNotExist()
                    rule.onNodeWithText(finalText).assertIsDisplayed()
                    shoot("completed", host)
                    summary().performClick(); settle(); expanded(true)
                    rule.onNodeWithText("deadline", substring = true).assertIsDisplayed()
                    rule.onNodeWithText(initialText).assertIsDisplayed()
                    rule.onNodeWithText(latestText).assertIsDisplayed()
                    shoot("reopened", host)
                    summary().performClick(); settle(); toolsHidden()
                }
                assertNotEquals("dark capture must not still be light", corners[0], corners[1])
            }
        }
    }

    @Test fun completionFoldsLiveExpansionAndHistoricalReopeningSurvivesReload() {
        val id = ChatTimeline().lines(running, true, emptySet()).lines.filterIsInstance<ChatLine.Work>().single().id
        val during = toggleWork(WorkOpenState(), id).first
        check(ChatTimeline().lines(latest, true, during.open).lines.filterIsInstance<ChatLine.Work>().single().open)
        val completed = during.copy(open = settleWorkOpen(during.open, emptySet(), setOf(id)))
        check(ChatTimeline().lines(ended, false, completed.open).lines.none { it is ChatLine.Step })
        val restored = toggleWork(completed, id).first.open.toList().toSet()
        val lines = ChatTimeline().lines(ended, false, restored).lines
        check(lines.filterIsInstance<ChatLine.Work>().single().open)
        check(lines.filterIsInstance<ChatLine.Step>().count { it.step is WorkItem.Tool } == 4)
    }

    @Test fun restoredReadingKeysAndManualExpansionUseTheRealChatListState() {
        val group = "w-u1"
        val readingKey = ChatReadingKey("test-scope", "conversation")
        val store = object : ChatAnchorStore {
            var saved = ChatReadingPosition("a12", followsEnd = false, expanded = setOf(group))
            override fun read(key: ChatReadingKey) = saved
            override fun write(key: ChatReadingKey, position: ChatReadingPosition) { saved = position }
        }
        val restoration = StateRestorationTester(rule)
        var shown: ChatListState? = null
        restoration.setContent {
            PersonaTheme(reduceMotion = true) {
                val list = rememberChatListState(readingKey, store)
                shown = list
                val lines = ChatTimeline().lines(ended, false, list.position.expanded).lines.asReversed()
                val context = LocalContext.current
                val loader = remember { ImageLoader.Builder(context).build() }
                DisposableEffect(loader) { onDispose { loader.shutdown() } }
                ChatListEffects(list, lines.map { it.contentKey }, ready = true, onFreeze = {}, onFollow = {}, loadOlder = {},
                    rowAliases = lines.associate { it.id to it.contentKey })
                LazyColumn(Modifier.width(400.dp).height(900.dp), state = list.list) {
                    items(lines, key = { it.contentKey }) { line ->
                        ChatLineContent(line, SessionKey("computer", "conversation"), loader, 9,
                            onToggle = { id -> list.setExpanded(toggleWork(WorkOpenState(list.position.expanded), id).first.open) },
                            onFold = {}, folding = emptySet(), foldErrors = emptyMap(), onFullText = {})
                    }
                }
            }
        }
        rule.waitForIdle()
        rule.runOnIdle {
            check(shown!!.restored)
            check(!shown!!.followsEnd) // The pre-change message key landed via its new stable slot alias.
            assertEquals(setOf(group), shown!!.position.expanded)
        }
        restoration.emulateSavedInstanceStateRestore()
        rule.waitForIdle()
        rule.runOnIdle { assertEquals(setOf(group), shown!!.position.expanded) }
        val summary = rule.onNode(hasClickAction() and hasContentDescription("，已展开", substring = true))
        summary.performScrollTo().performClick()
        rule.waitForIdle()
        rule.runOnIdle { check(shown!!.position.expanded.isEmpty()) }
        restoration.emulateSavedInstanceStateRestore()
        rule.waitForIdle()
        rule.runOnIdle { check(shown!!.position.expanded.isEmpty()) }
    }

    @Test fun unfoldingTheFirstHubFoldKeepsAnAssistantInitiatedExchangeOpen() {
        val fold = Item("fold:t3#0", 3, role = "assistant", kind = "fold",
            fold = persona.core.contracts.Fold(items = 6, tools = 3, throughSeq = 8, throughId = "r8"))
        val items = listOf(fold) + latest.filter { it.seq >= 9 }
        val timeline = ChatTimeline()
        val before = timeline.lines(items, true, emptySet()).lines
        val work = before.filterIsInstance<ChatLine.Work>().single()
        val opened = toggleWork(WorkOpenState(), work.id).first.open
        timeline.freeze(items) // Opening a fold must not turn the frozen running exchange into a completed one.
        timeline.unfold(fold, running.filter { it.seq in 3..8 })
        val after = timeline.lines(items, true, opened).lines
        val expanded = after.filterIsInstance<ChatLine.Work>().single()
        assertEquals(work.id, expanded.id)
        check(expanded.open)
        check(expanded.live)
        assertEquals(before.last().contentKey, after.last().contentKey)
        assertEquals(4, after.filterIsInstance<ChatLine.Step>().count { it.step is WorkItem.Tool })
    }

    @Test fun hubFoldsStayCollapsedUntilOpenedAndLoadAfterRestoration() {
        val fold = Item("fold:t3", 3, role = "assistant", kind = "fold",
            fold = persona.core.contracts.Fold(items = 6, tools = 3, throughSeq = 8, throughId = "r8"))
        val items = running.filter { it.seq < 3 } + fold + running.filter { it.seq >= 9 }
        val timeline = ChatTimeline()
        val before = timeline.lines(items, true, emptySet()).lines
        check(before.none { it is ChatLine.Step })
        check(openFoldsToFetch(before, emptySet(), emptySet()).isEmpty())
        val id = before.filterIsInstance<ChatLine.Work>().single().id
        for (live in listOf(true, false)) {
            val opened = timeline.lines(items, live, setOf(id)).lines
            assertEquals(listOf(fold.id), openFoldsToFetch(opened, emptySet(), emptySet()).map { it.id })
        }
        timeline.unfold(fold, running.filter { it.seq in 3..8 })
        val after = timeline.lines(items, true, setOf(id)).lines.filterIsInstance<ChatLine.Step>()
        check(after.none { it.step is WorkItem.Folded })
        check(after.count { it.step is WorkItem.Tool } == 4)
    }
}
