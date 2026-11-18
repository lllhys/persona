package persona.feature.agents

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.designsystem.*
import persona.core.domain.sort.AgentGrouping
import java.io.File

/**
 * By project's space headings and idle agents (#4338 A7, A8), in every style, light and dark: a space's heading counts the
 * agents that only wait in the background apart from Working, with a clock, as the computer's does; "N more idle" opens a
 * busy space's idle agents and, on Glass, "Hide N Idle" follows them and folds them again. Brief and Board get the clock and
 * keep their one-way "N more idle". A heading ends with its idle agents' count, a gray dot and a number (#4338 A5); on Glass
 * the folded "N more idle" names the first six's tabs as chips beside it (#4338 A8, iOS `IdleFoldRow`). PNGs in
 * `build/by-project-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp-xhdpi")
class ByProjectShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun session(machine: String, id: String, title: String, state: String, words: String, space: String = "app", waiting: Boolean = false,
                        tab: Int = 1) =
        Session(machine, id, title = title, cwd = "/code/$space", state = state, agent = "claude", spaceKind = "herdr", spaceId = space, spaceName = space,
            slotId = "t$tab", slotName = id, slotIndex = tab, paneId = "$space:p$tab", lastText = words, backgroundWaiting = waiting,
            backgroundWorking = waiting, updatedAt = "2026-01-01T00:00:00Z")

    private val sessions = listOf(
        session("mac", "needs", "Approve the migration", "needs_you", "Run the migration on the staging database?", tab = 1),
        session("mac", "working", "Fix the login bug", "working", "Updating the retry logic.", tab = 2),
        session("mac", "watch", "Watch the CI run", "working", "Waiting for the CI run to finish.", waiting = true, tab = 3),
        session("mac", "review", "Review the refactor", "idle", "Two places swallow errors.", tab = 4),
        session("mac", "research", "Compare cache designs", "idle", "Design A has its benchmark.", tab = 5),
        session("mac", "docs", "Update the docs", "idle", "The guide is updated.", tab = 6),
        session("server", "nightly", "Nightly build", "working", "Waiting for the build to finish.", space = "api", waiting = true),
        session("server", "deploy", "Roll out the fix", "working", "Rolling out to the second region.", space = "api", tab = 2),
        session("server", "poll", "Poll the queue", "working", "Waiting for the queue to drain.", space = "api", waiting = true, tab = 3),
    )
    private val machines = listOf(Machine("mac", "MacBook Pro", online = true, icon = "laptop"),
        Machine("server", "Workstation", online = true, icon = "server"))

    @Test fun everyStyle() {
        val model = agentsModel(sessions, machines)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    AgentsScreen(model, NoNavigation, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded }
        rule.runOnIdle { model.grouping(AgentGrouping.MachineSpace) }
        val out = File(System.getProperty("persona.shots") ?: "build/by-project-shots").also { it.mkdirs() }
        fun save(name: String, s: AppStyle, d: Boolean) {
            rule.waitForIdle()
            val bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
            File(out, "$name-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        val opened = mutableMapOf<AppStyle, Int>()
        val hidden = mutableMapOf<AppStyle, Int>()
        var foldedAgain = 0
        var clocks = 0
        var idles = 0
        val named = mutableMapOf<AppStyle, Int>()
        fun app() = model.page.value.list.groups.first { it.title.shown == "app" }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            rule.runOnIdle { style = s; dark = d }
            rule.waitForIdle()
            save("by-project", s, d)
            if (!d) clocks += listOf("1", "2").sumOf { rule.onAllNodesWithContentDescription("$it waiting in the background").fetchSemanticsNodes().size }
            if (!d) idles += rule.onAllNodesWithContentDescription("3 idle").fetchSemanticsNodes().size
            // The folded row's chips: the three idle agents' tabs, beside "3 more idle" on Glass.
            if (!d) named[s] = listOf("review", "research", "docs").count { rule.onAllNodesWithText(it).fetchSemanticsNodes().isNotEmpty() }
            // "3 more idle" opens the three idle agents; on Glass "Hide 3 Idle" follows them.
            rule.onNodeWithText("3 more idle").performClick()
            rule.waitForIdle()
            save("by-project-open", s, d)
            opened[s] = rule.onAllNodesWithText("Update the docs").fetchSemanticsNodes().size
            hidden[s] = rule.onAllNodesWithText("Hide 3 Idle").fetchSemanticsNodes().size
            if (s == AppStyle.Glass && !d && hidden[s] == 1) {
                rule.onNodeWithText("Hide 3 Idle").performClick()
                rule.waitForIdle()
                foldedAgain = rule.onAllNodesWithText("3 more idle").fetchSemanticsNodes().size
            } else rule.runOnIdle { model.hideIdle(app()) }
            rule.waitForIdle()
        }
        // The clocks: each computer's heading and each space's, in every style (the computers' were there before).
        assertEquals(4 * AppStyle.entries.size, clocks)
        // The idle count after the clock, on the computer's heading and the space's, in every style.
        assertEquals(2 * AppStyle.entries.size, idles)
        // Only Glass names the folded agents' tabs beside "3 more idle".
        assertEquals(mapOf(AppStyle.Glass to 3, AppStyle.Brief to 0, AppStyle.Board to 0), named)
        // The idle agents show once opened in every style; only Glass has the row that hides them again.
        assertEquals(mapOf(AppStyle.Glass to 1, AppStyle.Brief to 1, AppStyle.Board to 1), opened)
        assertEquals(mapOf(AppStyle.Glass to 1, AppStyle.Brief to 0, AppStyle.Board to 0), hidden)
        // A tap on "Hide 3 Idle" folds them behind "3 more idle" again.
        assertEquals(1, foldedAgain)
    }

    /** The folded row names at most six of the hidden agents' tabs (iOS `IdleFoldRow`'s `prefix(6)`), as chips; a compact list says them in one line. */
    @Test fun theFoldedRowNamesAtMostSixTabs() {
        val tabs = listOf("review", "research", "docs", "deps", "lint", "types", "perf", "ci")
        val idle = tabs.mapIndexed { i, tab ->
            Session("mac", "idle$i", title = "Idle $tab", cwd = "/code/app", state = "idle", agent = "claude", spaceKind = "herdr", spaceId = "app", spaceName = "app",
                slotId = "idle-$i", slotName = tab, slotIndex = i + 2, paneId = "app:idle$i", lastText = "Done with $tab.", updatedAt = "2026-01-01T00:0${9 - i}:00Z")
        }
        val model = agentsModel(listOf(session("mac", "build", "Fix the login bug", "working", "Updating the retry logic.", tab = 1)) + idle, machines.take(1))
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalHomeChrome provides HomeChrome(emptyList(), "Agents", onSelect = {}, leading = {})) {
                    AgentsScreen(model, NoNavigation, AgentsMetadata())
                }
            }
        }
        rule.waitUntil(5_000) { model.page.value.loaded }
        rule.runOnIdle { model.grouping(AgentGrouping.MachineSpace) }
        val out = File(System.getProperty("persona.shots") ?: "build/by-project-shots").also { it.mkdirs() }
        fun save(name: String, s: AppStyle, d: Boolean) {
            rule.waitForIdle()
            val bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
            File(out, "$name-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        fun named() = tabs.filter { rule.onAllNodesWithText(it).fetchSemanticsNodes().isNotEmpty() }
        for (d in listOf(false, true)) {
            rule.runOnIdle { dark = d }
            rule.waitForIdle()
            save("by-project-chips", AppStyle.Glass, d)
            rule.onNodeWithText("8 more idle").assertExists()
            // The first six, in the order the agents are listed; the last two stay unnamed. The heading counts all eight.
            assertEquals(tabs.take(6), named())
            assertEquals(1, rule.onAllNodesWithContentDescription("8 idle").fetchSemanticsNodes().size)
        }
        // A compact list says them in one line after the count (iOS `CompactFoldRow`).
        rule.runOnIdle { dark = false; model.compact() }
        rule.waitForIdle()
        rule.waitUntil(5_000) { model.page.value.preferences.compact }
        rule.waitForIdle()
        save("by-project-chips-compact", AppStyle.Glass, false)
        assertEquals(1, rule.onAllNodesWithText("review · research · docs · deps · lint · types").fetchSemanticsNodes().size)
        // Brief and Board keep the count alone.
        rule.runOnIdle { model.compact() }
        rule.waitUntil(5_000) { !model.page.value.preferences.compact }
        for (s in listOf(AppStyle.Brief, AppStyle.Board)) {
            rule.runOnIdle { style = s }
            rule.waitForIdle()
            save("by-project-chips", s, false)
            assertEquals(emptyList<String>(), named())
        }
    }
}
