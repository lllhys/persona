package persona.core.designsystem

import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/**
 * The loadable container: loading, content, empty, failure, retry, and a reload that keeps the old content, in every style,
 * in the three languages and at a large font size.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp")
class LoadableContainerTest {
    @get:Rule val rule = createComposeRule()

    private val rows = listOf("alpha", "beta")
    private val none = emptyList<String>()

    private fun show(
        state: JLoadable<List<String>>, onRetry: (() -> Unit)? = {}, style: AppStyle = AppStyle.Glass,
        loadingText: String? = null,
    ) = rule.setContent {
        PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
            LazyColumn(Modifier.testTag("list")) {
                jLoadableContent(state, onRetry = onRetry, loadingText = loadingText, isEmpty = { it.isEmpty() },
                    empty = { item("empty") { JEmptyState("Nothing here", "Add something first") } }) { list ->
                    items(list, key = { it }) { Text(it) }
                }
            }
        }
    }

    // The choice, apart from drawing.

    @Test fun loadingAndFailureDoNotDependOnTheValue() {
        assertEquals(LoadableSlot.Loading, loadableSlot<List<String>>(JLoadable.Loading, { it.isEmpty() }, hasEmpty = true))
        assertEquals(LoadableSlot.Failure, loadableSlot<List<String>>(JLoadable.Failed("down"), { it.isEmpty() }, hasEmpty = true))
    }

    @Test fun loadedValueIsContentOrEmpty() {
        assertEquals(LoadableSlot.Content, loadableSlot(JLoadable.Loaded(rows), { it.isEmpty() }, hasEmpty = true))
        assertEquals(LoadableSlot.Empty, loadableSlot(JLoadable.Loaded(none), { it.isEmpty() }, hasEmpty = true))
        // A page with no empty state of its own draws its content as it is.
        assertEquals(LoadableSlot.Content, loadableSlot(JLoadable.Loaded(none), { it.isEmpty() }, hasEmpty = false))
    }

    @Test fun aFailedReloadIsNeverTheEmptyState() {
        assertEquals(LoadableSlot.Failure, loadableSlot(JLoadable.Loaded(none, failure = "offline"), { it.isEmpty() }, hasEmpty = true))
        assertEquals(LoadableSlot.Content, loadableSlot(JLoadable.Loaded(rows, failure = "offline"), { it.isEmpty() }, hasEmpty = true))
    }

    @Test fun onlyLoadedContentRefreshes() {
        assertTrue(JLoadable.Loaded(rows, refreshing = true).isRefreshing)
        assertFalse(JLoadable.Loaded(rows).isRefreshing)
        assertFalse(JLoadable.Loading.isRefreshing)
        assertFalse(JLoadable.Failed("down").isRefreshing)
    }

    // The drawing.

    @Test fun firstLoadIsASpinnerWithNothingElse() {
        show(JLoadable.Loading, loadingText = "Reading the hub")
        rule.onNode(hasContentDescription("Loading")).assertIsDisplayed()
        rule.onNodeWithText("Reading the hub").assertIsDisplayed()
        rule.onAllNodesWithTextCount("alpha", 0)
        rule.onAllNodesWithTextCount("Retry", 0)
    }

    @Test fun contentIsDrawn() {
        show(JLoadable.Loaded(rows))
        rule.onNodeWithText("alpha").assertIsDisplayed()
        rule.onNodeWithText("beta").assertIsDisplayed()
        rule.onAllNodesWithTextCount("Retry", 0)
    }

    @Test fun refreshingKeepsTheContentAndShowsNoSpinnerOfItsOwn() {
        show(JLoadable.Loaded(rows, refreshing = true))
        rule.onNodeWithText("alpha").assertIsDisplayed()
        rule.onAllNodesWithContentDescriptionCount("Loading", 0)
    }

    @Test fun emptyContentIsTheEmptyStateNotAFailure() {
        show(JLoadable.Loaded(none))
        rule.onNodeWithText("Nothing here").assertIsDisplayed()
        rule.onAllNodesWithTextCount("Retry", 0)
    }

    @Test fun failureSaysWhyAndRetryAsks() {
        var retried = 0
        show(JLoadable.Failed("The hub did not answer", title = "Couldn't load"), onRetry = { retried++ })
        rule.onNodeWithText("Couldn't load").assertIsDisplayed()
        rule.onNodeWithText("The hub did not answer").assertIsDisplayed()
        rule.onNodeWithText("Retry").performClick()
        assertEquals(1, retried)
    }

    @Test fun failureWithoutARetryHasNoButton() {
        show(JLoadable.Failed("Not available here"), onRetry = null)
        rule.onNodeWithText("Not available here").assertIsDisplayed()
        rule.onAllNodesWithTextCount("Retry", 0)
    }

    @Test fun aFailedReloadSitsAboveTheContentItKept() {
        var retried = 0
        show(JLoadable.Loaded(rows, failure = "Offline: showing what was saved"), onRetry = { retried++ })
        rule.onNodeWithText("Offline: showing what was saved").assertIsDisplayed()
        rule.onNodeWithText("alpha").assertIsDisplayed()
        rule.onNodeWithText("Retry").performClick()
        assertEquals(1, retried)
    }

    @Test fun aFailedReadOverNothingIsAFailureNotAnEmptyList() {
        show(JLoadable.Loaded(none, failure = "Offline"))
        rule.onNodeWithText("Offline").assertIsDisplayed()
        rule.onAllNodesWithTextCount("Nothing here", 0)
    }

    @Test fun retryTurnsFailureIntoSpinnerThenContent() {
        var state by mutableStateOf<JLoadable<List<String>>>(JLoadable.Failed("down"))
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                LazyColumn { jLoadableContent(state, onRetry = { state = JLoadable.Loading }) { list -> items(list) { Text(it) } } }
            }
        }
        rule.onNodeWithText("Retry").performClick()
        rule.onNode(hasContentDescription("Loading")).assertIsDisplayed()
        state = JLoadable.Loaded(rows)
        rule.onNodeWithText("alpha").assertIsDisplayed()
    }

    @Test fun everyStyleDrawsEveryState() {
        val states = listOf(
            JLoadable.Loading, JLoadable.Loaded(rows), JLoadable.Loaded(rows, refreshing = true), JLoadable.Loaded(none),
            JLoadable.Loaded(rows, failure = "Offline"), JLoadable.Failed("down"),
        )
        var state by mutableStateOf<JLoadable<List<String>>>(states.first())
        var style by mutableStateOf(AppStyle.Glass)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                LazyColumn(Modifier.testTag("list")) {
                    jLoadableContent(state, onRetry = {}, isEmpty = { it.isEmpty() },
                        empty = { item("empty") { JEmptyState("Nothing here", "Add something first") } }) { list -> items(list, key = { it }) { Text(it) } }
                }
            }
        }
        for (next in AppStyle.entries) {
            style = next
            for (each in states) {
                state = each
                rule.waitForIdle()
                rule.onNodeWithTag("list").assertIsDisplayed()
            }
        }
    }

    // Languages and size.

    @Config(qualifiers = "zh-w400dp-h900dp")
    @Test fun retryAndSpinnerAreChineseInSimplifiedChinese() {
        show(JLoadable.Failed("读不到 Hub 的版本"))
        rule.onNodeWithText("重试").assertIsDisplayed()
        rule.onNodeWithText("读不到 Hub 的版本").assertIsDisplayed()
    }

    @Config(qualifiers = "zh-w400dp-h900dp")
    @Test fun spinnerIsChineseInSimplifiedChinese() {
        show(JLoadable.Loading)
        rule.onNode(hasContentDescription("加载中")).assertIsDisplayed()
    }

    @Config(qualifiers = "ja-w400dp-h900dp")
    @Test fun retryAndSpinnerAreJapaneseInJapanese() {
        show(JLoadable.Loaded(rows, failure = "ハブに届きません"))
        rule.onNodeWithText("再試行").assertIsDisplayed()
        rule.onNodeWithText("ハブに届きません").assertIsDisplayed()
    }

    @Config(qualifiers = "ja-w400dp-h900dp")
    @Test fun spinnerIsJapaneseInJapanese() {
        show(JLoadable.Loading)
        rule.onNode(hasContentDescription("読み込み中")).assertIsDisplayed()
    }

    @Test fun retryStaysOnOneLineAtTheLargestFontSize() {
        RuntimeEnvironment.setFontScale(2.0f)
        show(JLoadable.Failed("The hub did not answer. Check that it is running and reachable from this phone."))
        rule.onNodeWithText("Retry").assertIsDisplayed().performClick()
    }

    @Test fun aFailedReloadStillReachesRetryAtTheLargestFontSize() {
        RuntimeEnvironment.setFontScale(2.0f)
        show(JLoadable.Loaded(rows, failure = "Offline: showing what was saved"))
        rule.onNodeWithText("Retry").assertIsDisplayed().performClick()
    }

    private fun ComposeContentTestRule.onAllNodesWithTextCount(text: String, count: Int) =
        assertEquals("nodes with text \"$text\"", count, onAllNodes(hasText(text)).fetchSemanticsNodes().size)

    private fun ComposeContentTestRule.onAllNodesWithContentDescriptionCount(description: String, count: Int) =
        assertEquals("nodes described \"$description\"", count, onAllNodes(hasContentDescription(description)).fetchSemanticsNodes().size)
}
