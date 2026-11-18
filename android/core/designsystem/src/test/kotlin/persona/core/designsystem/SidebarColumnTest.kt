package persona.core.designsystem

import androidx.compose.foundation.focusable
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.focus.FocusManager
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertWidthIsEqualTo
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.click
import androidx.compose.ui.test.swipeUp
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w673dp-h951dp-mdpi")
class SidebarColumnTest {
    @get:Rule val rule = createComposeRule()

    @Test fun presentedPagesKeepTheirOwnBackControl() {
        var sheet by mutableStateOf(false)
        var opened by mutableStateOf(true)
        var backs = 0
        var toggles = 0
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalSplitDetail provides SplitDetailContext(isFirstPage = true, listHidden = true, onShowList = { toggles++ })) {
                    val close = { backs++; opened = false }
                    if (opened) {
                        if (sheet) JSheet(onDismiss = close) { JTopBar("Sheet", onBack = close) }
                        else JFullScreen(onDismiss = close) { JTopBar("Reader", onBack = close) }
                    }
                }
            }
        }
        rule.onNodeWithContentDescription("Back").performClick()
        rule.runOnIdle { assertEquals(1, backs); assertEquals(0, toggles); sheet = true; opened = true }
        rule.onNodeWithContentDescription("Back").performClick()
        rule.runOnIdle { assertEquals(2, backs); assertEquals(0, toggles) }
    }

    @Test fun theOverlayColumnKeepsItsRowsScrollableAndTappable() {
        lateinit var list: LazyListState
        var picked = -1
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                list = rememberLazyListState()
                JSidebarColumn(sidebar = {
                    LazyColumn(state = list, modifier = Modifier.fillMaxSize().testTag("list")) {
                        items(60) { index ->
                            Box(Modifier.height(Tokens.MinTouch.android).clickable { picked = index }) { Text("Row $index") }
                        }
                    }
                }, width = 320.dp, pageStart = 320.dp, shown = true, overlay = true) { Box(Modifier.fillMaxSize()) }
            }
        }
        rule.onNodeWithTag("list").performTouchInput { swipeUp() }
        rule.waitForIdle()
        var visible = 0
        rule.runOnIdle { visible = list.firstVisibleItemIndex + 1; assertTrue(visible > 1) }
        rule.onNodeWithText("Row $visible").performClick()
        rule.runOnIdle { assertEquals(visible, picked) }
    }

    @Test fun overlayKeepsThePageMountedAndFullWidthAndBlocksItsInputUntilDismissed() {
        var shown by mutableStateOf(false)
        var overlay by mutableStateOf(true)
        var built = 0
        var pageTaps = 0
        var pageFocused = false
        val focus = FocusRequester()
        val columnFocus = FocusRequester()
        lateinit var focusManager: FocusManager
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
              focusManager = LocalFocusManager.current
              // Production nests the column inside the shared drawer, even while that drawer is closed.
              JShellDrawer(opened = false, onDismiss = {}, drawer = {}) {
                JSidebarColumn(sidebar = { Box(Modifier.fillMaxSize().testTag("column").focusRequester(columnFocus).focusable()) },
                    width = 320.dp, pageStart = 320.dp, shown = shown, overlay = overlay, onDismiss = { shown = false }) {
                    remember { built++ }
                    Box(Modifier.fillMaxSize().testTag("page").focusRequester(focus).onFocusChanged { pageFocused = it.isFocused }
                        .focusable().clickable { pageTaps++ })
                }
              }
            }
        }
        rule.onNodeWithTag("page").assertWidthIsEqualTo(673.dp)
        rule.runOnIdle { assertTrue(focus.requestFocus()); shown = true }
        rule.waitForIdle()
        rule.runOnIdle {
            assertFalse(focus.requestFocus())
            assertFalse(pageFocused)
            assertTrue(columnFocus.requestFocus())
            // Tab / D-pad traversal must never enter the page under the scrim either.
            repeat(4) {
                focusManager.moveFocus(FocusDirection.Next)
                assertFalse(focus.requestFocus())
                assertFalse(pageFocused)
            }
        }
        rule.onNodeWithTag("column").assertWidthIsEqualTo(320.dp)
        rule.onNodeWithTag("column").performTouchInput { click(center) }
        rule.runOnIdle { assertTrue(shown); assertEquals(0, pageTaps) }
        rule.onNodeWithContentDescription("Close").performTouchInput { click(centerRight) }
        rule.waitForIdle()
        rule.runOnIdle { assertFalse(shown); assertEquals(0, pageTaps); assertTrue(focus.requestFocus()) }
        rule.onNodeWithTag("page").assertWidthIsEqualTo(673.dp)
        rule.runOnIdle { overlay = false; shown = true }
        rule.waitForIdle()
        assertEquals(320f, rule.onNodeWithTag("page").getBoundsInRoot().left.value, 0.01f)
        rule.onNodeWithContentDescription("Close").assertDoesNotExist()
        rule.runOnIdle { overlay = true; shown = false }
        rule.waitForIdle()
        rule.onNodeWithTag("page").assertWidthIsEqualTo(673.dp)
        rule.runOnIdle { assertEquals("page survives overlay/beside and show/hide changes", 1, built) }
    }
}
