package persona.core.designsystem

import android.os.SystemClock
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.assertIsFocused
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.height
import androidx.compose.ui.unit.width
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import dev.chrisbanes.haze.HazeState
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.icons.PhIcons

/** One density pixel per dp keeps the semantic bounds and WindowLayout's window pixels directly comparable. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w1000dp-h900dp-mdpi")
class TabletopPageTest {
    @get:Rule val rule = createComposeRule()

    @After fun disableProbe() = AdaptiveProbe.disable()

    private val tabletop = WindowLayout(tabletop = true, hingeTop = 450f, hingeBottom = 470f)
    private val flat = WindowLayout()
    private val topBar: @Composable () -> Unit = { Box(Modifier.fillMaxWidth().height(60.dp).testTag("top-bar")) }
    private val dock: @Composable () -> Unit = { Box(Modifier.fillMaxWidth().height(80.dp).testTag("dock")) }

    private data class PageCase(val window: WindowLayout, val hasDock: Boolean)

    @Test fun glassPagesAvoidTheHingeAndFallBack() = exerciseLayouts(AppStyle.Glass)
    @Test fun briefPagesAvoidTheHingeAndFallBack() = exerciseLayouts(AppStyle.Brief)
    @Test fun boardPagesAvoidTheHingeAndFallBack() = exerciseLayouts(AppStyle.Board)

    private fun exerciseLayouts(style: AppStyle) {
        var current by mutableStateOf(PageCase(tabletop, hasDock = false))
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalWindowLayout provides current.window) {
                    Box(Modifier.fillMaxSize().testTag("stage")) {
                        JPage("Page", topBar = topBar, bottomBar = if (current.hasDock) dock else null) {
                            Box(Modifier.fillMaxSize().testTag("content")) {
                                JButton("Content", onClick = {}, modifier = Modifier.testTag("content-action"))
                            }
                        }
                    }
                }
            }
        }

        assertSplit(hasDock = false)
        current = PageCase(tabletop, hasDock = true)
        rule.waitForIdle()
        assertSplit(hasDock = true)

        current = PageCase(tabletop.copy(hingeTop = 107f, hingeBottom = 120f), hasDock = false)
        rule.waitForIdle()
        assertSingle(hasDock = false)
        current = current.copy(hasDock = true)
        rule.waitForIdle()
        assertSingle(hasDock = true)

        current = PageCase(tabletop.copy(hingeBottom = 850f), hasDock = true)
        rule.waitForIdle()
        assertSingle(hasDock = true)
        current = current.copy(hasDock = false)
        rule.waitForIdle()
        assertSplit(hasDock = false)

        current = PageCase(flat, hasDock = true)
        rule.waitForIdle()
        assertSingle(hasDock = true)
    }

    private fun assertSplit(hasDock: Boolean) {
        assertAtMost(450.dp, rule.onNodeWithTag("top-bar").getBoundsInRoot().bottom, "top bar")
        assertAtMost(450.dp, rule.onNodeWithTag("content").getBoundsInRoot().bottom, "content")
        if (hasDock) {
            val dockBounds = rule.onNodeWithTag("dock").getBoundsInRoot()
            assertTrue("dock starts inside the hinge: ${dockBounds.top}", dockBounds.top >= 470.dp)
            val stage = rule.onNodeWithTag("stage").getBoundsInRoot()
            val expectedTop = 470.dp + (stage.height - 470.dp - 80.dp) / 2
            assertEquals(expectedTop.value, dockBounds.top.value, 0.5f)
            assertEquals((ReadingWidth + 40.dp).value, dockBounds.width.value, 0.5f)
        } else {
            rule.onNodeWithTag("dock").assertDoesNotExist()
            rule.onAllNodes(hasClickAction()).fetchSemanticsNodes().forEach { node ->
                assertTrue("interactive node below the upper safe side: ${node.boundsInRoot}", node.boundsInRoot.bottom <= 450f)
            }
        }
    }

    private fun assertSingle(hasDock: Boolean) {
        assertTrue("single-page content did not recover the lower side", rule.onNodeWithTag("content").getBoundsInRoot().bottom > 470.dp)
        if (hasDock) {
            val dockBounds = rule.onNodeWithTag("dock").getBoundsInRoot()
            assertEquals((ReadingWidth + 40.dp).value, dockBounds.width.value, 0.5f)
        } else {
            rule.onNodeWithTag("dock").assertDoesNotExist()
        }
    }

    private fun assertAtMost(limit: Dp, actual: Dp, label: String) {
        assertTrue("$label crosses the hinge: $actual > $limit", actual <= limit)
    }

    @Test fun glassKeepsDraftFocusAndRememberedStateWhenPostureChanges() = exerciseState(AppStyle.Glass)
    @Test fun briefKeepsDraftFocusAndRememberedStateWhenPostureChanges() = exerciseState(AppStyle.Brief)
    @Test fun boardKeepsDraftFocusAndRememberedStateWhenPostureChanges() = exerciseState(AppStyle.Board)

    private fun exerciseState(style: AppStyle) {
        var window by mutableStateOf(tabletop)
        var draft by mutableStateOf(TextFieldValue("keep", selection = TextRange(4)))
        val focus = FocusRequester()
        var contentCreations = 0
        var backs = 0
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalWindowLayout provides window) {
                    JPage("Page", onBack = { backs++ }, topBar = topBar) {
                        remember { contentCreations++ }
                        LaunchedEffect(Unit) { focus.requestFocus() }
                        BasicTextField(draft, { draft = it }, Modifier.testTag("draft").focusRequester(focus))
                    }
                }
            }
        }
        rule.onNodeWithTag("draft").assertIsFocused()
        window = flat
        rule.waitForIdle()
        assertDraftAndState(contentCreations, backs)
        window = tabletop
        rule.waitForIdle()
        assertDraftAndState(contentCreations, backs)
    }

    private fun assertDraftAndState(contentCreations: Int, backs: Int) {
        rule.onNodeWithTag("draft").assertIsFocused()
        assertEquals(1, contentCreations)
        assertEquals(0, backs)
        assertEquals(TextRange(4), rule.onNodeWithTag("draft").fetchSemanticsNode().config.getOrNull(
            androidx.compose.ui.semantics.SemanticsProperties.TextSelectionRange))
    }

    @Test fun listPageRetainsTheCallersStateWithTheProbeDisabled() = exerciseListProbe(enabled = false)
    @Test fun listPageRetainsTheCallersStateWithTheProbeEnabled() = exerciseListProbe(enabled = true)

    private fun exerciseListProbe(enabled: Boolean) {
        if (enabled) AdaptiveProbe.enable(ApplicationProvider.getApplicationContext()) else AdaptiveProbe.disable()
        assertEquals(enabled, AdaptiveProbe.enabled)
        val listState = LazyListState(firstVisibleItemIndex = 20, firstVisibleItemScrollOffset = 7)
        var window by mutableStateOf(tabletop)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalWindowLayout provides window) {
                    JListPage("Settings", state = listState) {
                        repeat(100) { index ->
                            item(key = "row-$index") { Text("Row $index", Modifier.height(40.dp)) }
                        }
                    }
                }
            }
        }
        rule.waitForIdle()
        assertEquals(20, listState.firstVisibleItemIndex)
        assertEquals(7, listState.firstVisibleItemScrollOffset)
        assertAtMost(450.dp, rule.onNodeWithText("Row 20").getBoundsInRoot().bottom, "list row")
        window = flat
        rule.waitForIdle()
        assertEquals(20, listState.firstVisibleItemIndex)
        assertEquals(7, listState.firstVisibleItemScrollOffset)
    }

    @Test fun anEmbeddedTabletopPageRemainsTheOnlyGlassSource() {
        val state = HazeState()
        rule.setContent {
            PersonaTheme(darkTheme = true, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalGlassSource provides state, LocalWindowLayout provides tabletop) {
                    JPage("host", topBar = {}, edges = false) {
                        JPage("conversation", Modifier.fillMaxSize(), topBar = {}, bottomBar = dock) { Text("Body") }
                    }
                }
            }
        }
        rule.waitForIdle()
        val sources = HazeState::class.java.getMethod("getAreas\$haze_root_haze").invoke(state) as List<*>
        assertEquals(1, sources.size)
    }

    @Test fun theHomeBarStillPublishesTheConnectionPillAnchor() {
        val barHeight = mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(
                    LocalWindowLayout provides tabletop,
                    LocalHomeChrome provides HomeChrome(
                        items = listOf(
                            NavItem("assistant", "Assistant", PhIcons.ChatCircle),
                            NavItem("agents", "Agents", PhIcons.Robot),
                        ),
                        selected = "assistant", onSelect = {}, leading = {}, barHeight = barHeight,
                    ),
                ) {
                    JPage("Home") { Text("Body") }
                }
            }
        }
        rule.waitForIdle()
        assertTrue("home bar did not publish its height", barHeight.intValue > 0)
        assertAtMost(450.dp, rule.onNodeWithText("Agents").getBoundsInRoot().bottom, "home navigation")
    }

    @Test fun crampedFlatBriefAndBoardKeepTheOriginalColumnOrder() {
        var style by mutableStateOf(AppStyle.Brief)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalWindowLayout provides flat) {
                    Box(Modifier.fillMaxWidth().height(100.dp)) {
                        JPage("Page", topBar = topBar, bottomBar = dock) {
                            Box(Modifier.fillMaxSize().testTag("content"))
                        }
                    }
                }
            }
        }
        for (current in listOf(AppStyle.Brief, AppStyle.Board)) {
            rule.runOnIdle { style = current }
            rule.waitForIdle()
            val top = rule.onNodeWithTag("top-bar").getBoundsInRoot()
            val bottom = rule.onNodeWithTag("dock").getBoundsInRoot()
            assertEquals("the dock follows the top bar in $current", top.bottom.value, bottom.top.value, 0.5f)
        }
    }

    @Test fun nonListPagesPublishSafeContentAndHingeBounds() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        context.applicationInfo.flags = context.applicationInfo.flags or android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE
        org.robolectric.shadows.ShadowProcess.setPid(1234)
        var style by mutableStateOf(AppStyle.Glass)
        AdaptiveProbe.enable(context)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalWindowLayout provides tabletop) {
                    JPage("Files-$style", topBar = topBar, bottomBar = dock) {
                        Box(Modifier.fillMaxSize().testTag("content"))
                    }
                }
            }
        }
        for (current in AppStyle.entries) {
            if (current != AppStyle.Glass) {
                AdaptiveProbe.enable(context)
                rule.runOnIdle { style = current }
            }
            rule.waitForIdle()
            val file = context.noBackupFilesDir.resolve("hub-data/adaptive-probe.json")
            assertTrue("probe file missing for $current", file.isFile)
            val snapshot = decodeAdaptiveProbeSnapshot(file.readText(), SystemClock.elapsedRealtimeNanos(), Long.MAX_VALUE)
            val evidence = snapshot?.pages?.get("Files-$current")?.bounds
            assertTrue("bounds missing for $current", evidence != null)
            assertTrue("hinge missing for $current", evidence?.hinge != null)
            assertTrue(
                "content crosses the hinge in $current: $evidence",
                evidence != null && evidence.hinge != null && evidence.content.bottom <= evidence.hinge.top,
            )
            AdaptiveProbe.disable()
        }
    }
}
