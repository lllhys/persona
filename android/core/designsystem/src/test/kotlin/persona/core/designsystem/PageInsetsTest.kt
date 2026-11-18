package persona.core.designsystem

import android.graphics.Bitmap
import android.view.View
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.core.graphics.Insets
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.icons.PhIcons
import java.io.File
import kotlin.math.roundToInt

/**
 * Verifies navigation-bar and IME inset placement, dock measurement, click hit-testing, and visual bounds for [JPage]
 * across [AppStyle.Glass], [AppStyle.Brief], and [AppStyle.Board] in both standalone and embedded
 * (`AssistantScreen` -> `ChatScreen`) configurations (#8392).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-mdpi")
class PageInsetsTest {
    @get:Rule val rule = createComposeRule()

    private lateinit var composeView: View
    private val topBar: @Composable () -> Unit = { Box(Modifier.fillMaxWidth().height(56.dp).testTag("top-bar")) }
    private val composer: @Composable () -> Unit = {
        Box(Modifier.fillMaxWidth().navigationBarsPadding().height(72.dp).testTag("composer"))
    }

    private fun dpToPx(dp: Int): Int = (dp * composeView.resources.displayMetrics.density).roundToInt()

    private fun applyInsets(
        navigation: Insets = Insets.NONE,
        imeBottom: Int = 0,
        statusTop: Int = 24,
    ) {
        val insets = WindowInsetsCompat.Builder()
            .setInsets(WindowInsetsCompat.Type.statusBars(), Insets.of(0, statusTop, 0, 0))
            .setInsets(WindowInsetsCompat.Type.navigationBars(), navigation)
            .setInsets(WindowInsetsCompat.Type.ime(), Insets.of(0, 0, 0, imeBottom))
            .setVisible(WindowInsetsCompat.Type.ime(), imeBottom > 0)
            .build()
        rule.runOnIdle { ViewCompat.dispatchApplyWindowInsets(composeView, insets) }
        rule.waitForIdle()
    }

    private fun assertComposerBottomFromWindowBottom(expectedInset: Dp, label: String) {
        val rootBottom = rule.onRoot().getUnclippedBoundsInRoot().bottom
        val bounds = rule.onNodeWithTag("composer").getUnclippedBoundsInRoot()
        assertEquals("$label: composer height must not double-pad navigationBars", 72f, (bounds.bottom - bounds.top).value, 0.5f)
        assertEquals("$label: composer bottom inset", (rootBottom - expectedInset).value, bounds.bottom.value, 0.5f)
    }

    @Test fun embeddedConversationKeepsComposerClearOfNavigationBarAndFlushWithKeyboard() {
        var style by mutableStateOf(AppStyle.Brief)
        var dark by mutableStateOf(false)
        var hostContentCompositions = 0
        rule.setContent {
            composeView = LocalView.current
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                // Matches AssistantScreen hosting ChatScreen(embedded = true): outer JPage carries imePadding() and
                // forwards its bottom/top padding through LocalChromeInsets with handlesNavigationBar = true.
                JPage(
                    title = "Persona",
                    modifier = Modifier.fillMaxSize().imePadding(),
                    topBar = topBar,
                    edges = false,
                    scrollEdgeStyle = ScrollEdgeStyle.Gradient,
                ) { padding ->
                    SideEffect { hostContentCompositions++ }
                    val host = ChromeInsets(
                        bottom = padding.calculateBottomPadding(),
                        handlesNavigationBar = true,
                        top = padding.calculateTopPadding(),
                    )
                    CompositionLocalProvider(LocalChromeInsets provides host) {
                        JPage(
                            title = "Conversation",
                            modifier = Modifier.fillMaxSize(),
                            topBar = {},
                            bottomBar = composer,
                            scrollEdgeStyle = ScrollEdgeStyle.Gradient,
                        ) {
                            Box(Modifier.fillMaxSize().testTag("messages"))
                        }
                    }
                }
            }
        }

        for (currentStyle in AppStyle.entries) {
            for (currentDark in listOf(false, true)) {
                rule.runOnIdle {
                    style = currentStyle
                    dark = currentDark
                }
                rule.waitForIdle()
                val label = "embedded style=$currentStyle dark=$currentDark"

                // Three-button navigation (48dp) with keyboard down: composer sits 48dp above window bottom.
                applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 0)
                assertComposerBottomFromWindowBottom(48.dp, "$label 3-button ime=0")

                // Gesture navigation (20dp) with keyboard down: composer sits 20dp above window bottom.
                applyInsets(navigation = Insets.of(0, 0, 0, 20), imeBottom = 0)
                assertComposerBottomFromWindowBottom(20.dp, "$label gesture ime=0")

                // Keyboard sliding past the bottom edge but still below the 48dp navigation bar (20dp IME):
                // 20dp imePadding + 28dp unconsumed navigation bar = 48dp total, with no jump.
                applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 20)
                assertComposerBottomFromWindowBottom(48.dp, "$label 3-button ime=20")

                // Keyboard sliding above the navigation bar (80 -> 160 -> 240 -> 300dp):
                // composer sits flush on top of the keyboard without an extra 48dp gap, and host ChromeInsets
                // stops recomposing once unconsumed navigation reaches 0dp.
                applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 80)
                assertComposerBottomFromWindowBottom(80.dp, "$label 3-button ime=80")
                val beforeSteadySlide = hostContentCompositions
                for (imeStep in listOf(160, 240, 300)) {
                    applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = imeStep)
                    assertComposerBottomFromWindowBottom(imeStep.dp, "$label 3-button ime=$imeStep")
                }
                assertEquals(
                    "$label: host content must not recompose on keyboard frames above navigation bar",
                    beforeSteadySlide,
                    hostContentCompositions,
                )

                // Keyboard dismissed again: composer returns above the 48dp three-button navigation bar.
                applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 0)
                assertComposerBottomFromWindowBottom(48.dp, "$label 3-button ime=0 after close")
            }
        }
    }

    @Test fun standalonePageKeepsComposerClearOfNavigationBarAndFlushWithKeyboard() {
        var style by mutableStateOf(AppStyle.Brief)
        rule.setContent {
            composeView = LocalView.current
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                // Matches standalone ChatScreen inside ChatLinkHost(Modifier.fillMaxSize().imePadding()).
                Box(Modifier.fillMaxSize().imePadding()) {
                    JPage(
                        title = "Agent",
                        topBar = topBar,
                        bottomBar = composer,
                        scrollEdgeStyle = ScrollEdgeStyle.Gradient,
                    ) {
                        Box(Modifier.fillMaxSize().testTag("messages"))
                    }
                }
            }
        }

        for (currentStyle in AppStyle.entries) {
            rule.runOnIdle { style = currentStyle }
            rule.waitForIdle()
            val label = "standalone style=$currentStyle"

            applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 0)
            assertComposerBottomFromWindowBottom(48.dp, "$label 3-button ime=0")

            applyInsets(navigation = Insets.of(0, 0, 0, 20), imeBottom = 0)
            assertComposerBottomFromWindowBottom(20.dp, "$label gesture ime=0")

            applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 20)
            assertComposerBottomFromWindowBottom(48.dp, "$label 3-button ime=20")

            applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 300)
            assertComposerBottomFromWindowBottom(300.dp, "$label 3-button ime=300")
        }
    }

    @Test fun docklessEmbeddedPagePropagatesHostBottomPaddingAcrossAllStyles() {
        var style by mutableStateOf(AppStyle.Brief)
        var innerPadding = PaddingValues()
        rule.setContent {
            composeView = LocalView.current
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                JPage(
                    title = "Host",
                    modifier = Modifier.fillMaxSize().imePadding(),
                    topBar = topBar,
                    edges = false,
                ) { hostPadding ->
                    val host = ChromeInsets(
                        bottom = hostPadding.calculateBottomPadding(),
                        handlesNavigationBar = true,
                        top = hostPadding.calculateTopPadding(),
                    )
                    CompositionLocalProvider(LocalChromeInsets provides host) {
                        JPage(title = "Embedded", modifier = Modifier.fillMaxSize(), topBar = {}) { padding ->
                            SideEffect { innerPadding = padding }
                            Box(Modifier.fillMaxSize())
                        }
                    }
                }
            }
        }

        for (currentStyle in AppStyle.entries) {
            rule.runOnIdle { style = currentStyle }
            rule.waitForIdle()
            val label = "dockless embedded style=$currentStyle"

            applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 0)
            assertEquals("$label 3-button ime=0", 48f, innerPadding.calculateBottomPadding().value, 0.5f)

            applyInsets(navigation = Insets.of(0, 0, 0, 20), imeBottom = 0)
            assertEquals("$label gesture ime=0", 20f, innerPadding.calculateBottomPadding().value, 0.5f)

            applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 300)
            assertEquals("$label 3-button ime=300", 0f, innerPadding.calculateBottomPadding().value, 0.5f)
        }
    }

    @Test
    @Config(qualifiers = "en-w900dp-h400dp-mdpi")
    fun landscapeSideNavigationBarDoesNotDoubleInsetUnderShellHorizontalSafeDrawing() {
        var underShellSides by mutableStateOf(false)
        var style by mutableStateOf(AppStyle.Brief)
        rule.setContent {
            composeView = LocalView.current
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                val sides = WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal)
                Box(Modifier.fillMaxSize().then(if (underShellSides) Modifier.windowInsetsPadding(sides) else Modifier)) {
                    JPage(title = "Landscape", topBar = topBar, bottomBar = composer) {
                        Box(Modifier.fillMaxSize())
                    }
                }
            }
        }

        for (currentStyle in AppStyle.entries) {
            for (shellHandled in listOf(false, true)) {
                rule.runOnIdle {
                    style = currentStyle
                    underShellSides = shellHandled
                }
                rule.waitForIdle()
                applyInsets(navigation = Insets.of(0, 0, 48, 0), imeBottom = 0)
                val rootWidth = rule.onRoot().getUnclippedBoundsInRoot().let { it.right - it.left }
                val dockBounds = rule.onNodeWithTag("composer").getUnclippedBoundsInRoot()
                val availablePaneRight = rootWidth - 48.dp
                // Centered inside the 900 - 48 = 852dp safe pane width (composer max width is ReadingWidth + 40.dp = 760.dp),
                // never shifted left by a second 48dp horizontal inset when Root.kt already applied safeDrawing horizontal padding.
                val expectedWidth = minOf(ReadingWidth + 40.dp, availablePaneRight)
                val expectedLeft = (availablePaneRight - expectedWidth) / 2
                assertEquals(
                    "landscape style=$currentStyle shellHandled=$shellHandled left",
                    expectedLeft.value,
                    dockBounds.left.value,
                    0.5f,
                )
            }
        }
    }

    /**
     * Verifies that `consumeWindowInsets(WindowInsets.navigationBars)` and `onConsumedWindowInsetsChanged` in [JPage]
     * neither alter [AppStyle.Glass] / [AppStyle.Brief] / [AppStyle.Board] dock height measurement nor obstruct pointer
     * clicks on interactive dock controls ([JDockPill] and [JBottomBar] actions), in both standalone and embedded modes.
     */
    @Test fun bottomDockControlsKeepUnpaddedHeightAndReceiveClicksAcrossAllStyles() {
        var style by mutableStateOf(AppStyle.Glass)
        var embedded by mutableStateOf(false)
        var pillClicks = 0
        var sendClicks = 0
        val interactiveDock: @Composable () -> Unit = {
            Column(
                Modifier.fillMaxWidth().testTag("dock-stack"),
                verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs),
            ) {
                JDockPill(
                    text = "Controlling Workstation",
                    running = false,
                    icon = PhIcons.Laptop,
                    spoken = "Controlling Workstation",
                    modifier = Modifier.padding(horizontal = Tokens.Space.m).testTag("dock-pill"),
                    onClick = { pillClicks++ },
                    onClickLabel = "Show the steps",
                )
                JBottomBar(Modifier.testTag("dock-composer")) {
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            "Ask Persona…",
                            Modifier.weight(1f),
                            style = PersonaTheme.type.body,
                            color = PersonaTheme.palette.ink3,
                        )
                        JIconButton(PhIcons.ArrowUp, "Send", { sendClicks++ }, Modifier.testTag("dock-send"))
                    }
                }
            }
        }

        rule.setContent {
            composeView = LocalView.current
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                if (!embedded) {
                    Box(Modifier.fillMaxSize().imePadding()) {
                        JPage(
                            title = "Agent",
                            topBar = topBar,
                            bottomBar = interactiveDock,
                            scrollEdgeStyle = ScrollEdgeStyle.Gradient,
                        ) {
                            Box(Modifier.fillMaxSize())
                        }
                    }
                } else {
                    JPage(
                        title = "Persona",
                        modifier = Modifier.fillMaxSize().imePadding(),
                        topBar = topBar,
                        edges = false,
                        scrollEdgeStyle = ScrollEdgeStyle.Gradient,
                    ) { padding ->
                        val host = ChromeInsets(
                            bottom = padding.calculateBottomPadding(),
                            handlesNavigationBar = true,
                            top = padding.calculateTopPadding(),
                        )
                        CompositionLocalProvider(LocalChromeInsets provides host) {
                            JPage(
                                title = "Conversation",
                                modifier = Modifier.fillMaxSize(),
                                topBar = {},
                                bottomBar = interactiveDock,
                                scrollEdgeStyle = ScrollEdgeStyle.Gradient,
                            ) {
                                Box(Modifier.fillMaxSize())
                            }
                        }
                    }
                }
            }
        }

        var expectedPillClicks = 0
        var expectedSendClicks = 0
        for (currentStyle in AppStyle.entries) {
            for (isEmbedded in listOf(false, true)) {
                rule.runOnIdle {
                    style = currentStyle
                    embedded = isEmbedded
                }
                rule.waitForIdle()
                val label = "dock interactive style=$currentStyle embedded=$isEmbedded"

                applyInsets(navigation = Insets.NONE, imeBottom = 0)
                val rootBottom = rule.onRoot().getUnclippedBoundsInRoot().bottom
                val unpaddedBounds = rule.onNodeWithTag("dock-stack").getUnclippedBoundsInRoot()
                val unpaddedHeight = (unpaddedBounds.bottom - unpaddedBounds.top).value
                assertTrue("$label: dock has positive measured height", unpaddedHeight > 0f)

                applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 0)
                val navBounds = rule.onNodeWithTag("dock-stack").getUnclippedBoundsInRoot()
                assertEquals("$label: dock height unchanged under 48dp nav bar", unpaddedHeight, (navBounds.bottom - navBounds.top).value, 0.5f)
                assertEquals("$label: dock bottom sits 48dp above window bottom", (rootBottom - 48.dp).value, navBounds.bottom.value, 0.5f)
                rule.onNodeWithTag("dock-pill").performClick()
                rule.waitForIdle()
                assertEquals("$label: pill click under 48dp nav bar", ++expectedPillClicks, pillClicks)
                rule.onNodeWithTag("dock-send").performClick()
                rule.waitForIdle()
                assertEquals("$label: send click under 48dp nav bar", ++expectedSendClicks, sendClicks)

                applyInsets(navigation = Insets.of(0, 0, 0, 48), imeBottom = 300)
                val imeBounds = rule.onNodeWithTag("dock-stack").getUnclippedBoundsInRoot()
                assertEquals("$label: dock height unchanged under 300dp IME", unpaddedHeight, (imeBounds.bottom - imeBounds.top).value, 0.5f)
                assertEquals("$label: dock bottom sits 300dp above window bottom", (rootBottom - 300.dp).value, imeBounds.bottom.value, 0.5f)
                rule.onNodeWithTag("dock-pill").performClick()
                rule.waitForIdle()
                assertEquals("$label: pill click under 300dp IME", ++expectedPillClicks, pillClicks)
                rule.onNodeWithTag("dock-send").performClick()
                rule.waitForIdle()
                assertEquals("$label: send click under 300dp IME", ++expectedSendClicks, sendClicks)
            }
        }
    }

    /**
     * Renders synthetic Robolectric native-graphics screenshots of the embedded Assistant conversation layout in
     * [AppStyle.Brief], [AppStyle.Board], and [AppStyle.Glass] (light and dark) with a visible 48dp three-button
     * navigation bar indicator and with a 260dp keyboard indicator, written under `build/reports/tests/page-insets-shots/`.
     */
    @Test
    @Config(qualifiers = "en-w400dp-h760dp-xhdpi")
    fun embeddedConversationShotsShowComposerClearOfSystemBarsAndKeyboard() {
        var style by mutableStateOf(AppStyle.Brief)
        var dark by mutableStateOf(false)
        var shownImeDp by mutableIntStateOf(0)
        val out = File(System.getProperty("persona.shots") ?: "build/reports/tests/page-insets-shots").also { it.mkdirs() }

        rule.setContent {
            composeView = LocalView.current
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.size(400.dp, 760.dp).background(PersonaTheme.palette.background).testTag("shot")) {
                    JPage(
                        title = "Persona",
                        modifier = Modifier.fillMaxSize().imePadding(),
                        edges = false,
                        scrollEdgeStyle = ScrollEdgeStyle.Gradient,
                    ) { padding ->
                        val host = ChromeInsets(
                            bottom = padding.calculateBottomPadding(),
                            handlesNavigationBar = true,
                            top = padding.calculateTopPadding(),
                        )
                        CompositionLocalProvider(LocalChromeInsets provides host) {
                            JPage(
                                title = "Conversation",
                                modifier = Modifier.fillMaxSize(),
                                topBar = {},
                                bottomBar = {
                                    JBottomBar(Modifier.testTag("shot-composer")) {
                                        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                            JIconButton(PhIcons.Plus, "Add", {})
                                            Text(
                                                "Ask Persona…",
                                                Modifier.weight(1f).padding(horizontal = Tokens.Space.s),
                                                style = PersonaTheme.type.body,
                                                color = PersonaTheme.palette.ink3,
                                            )
                                            JIconButton(PhIcons.ArrowUp, "Send", {})
                                        }
                                    }
                                },
                                scrollEdgeStyle = ScrollEdgeStyle.Gradient,
                            ) { innerPadding ->
                                Column(
                                    Modifier.fillMaxSize().padding(
                                        start = Tokens.Space.l,
                                        end = Tokens.Space.l,
                                        top = innerPadding.calculateTopPadding() + Tokens.Space.m,
                                        bottom = innerPadding.calculateBottomPadding() + Tokens.Space.m,
                                    ),
                                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.m, Alignment.Bottom),
                                ) {
                                    Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.CenterEnd) {
                                        UserBubble("Check embedded composer bottom insets in Brief and Board.", time = "18:20")
                                    }
                                    Text(
                                        "The composer stays above the 48 dp navigation bar and sits flush on top of the keyboard.",
                                        style = PersonaTheme.type.body,
                                        color = PersonaTheme.palette.ink,
                                    )
                                }
                            }
                        }
                    }

                    // Visual overlay representing the Android system window regions (not drawn by app View hierarchy).
                    val barBg = if (dark) Color(0xE614161A) else Color(0xEBEDEFF3)
                    val barInk = if (dark) Color(0xFFE2E5EC) else Color(0xFF3A3F47)
                    val barStroke = if (dark) Color(0xFF343942) else Color(0xFFC8CDD6)
                    if (shownImeDp > 0) {
                        val imeBg = if (dark) Color(0xFF22252B) else Color(0xFFD7DBE2)
                        Column(
                            Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(shownImeDp.dp)
                                .background(imeBg).border(1.dp, barStroke),
                            horizontalAlignment = Alignment.CenterHorizontally,
                        ) {
                            Box(Modifier.fillMaxWidth().weight(1f), contentAlignment = Alignment.Center) {
                                Text("Synthetic IME (${shownImeDp} dp)", style = PersonaTheme.type.caption, color = barInk)
                            }
                            ThreeButtonBarOverlay(barBg, barInk, barStroke)
                        }
                    } else {
                        Box(Modifier.align(Alignment.BottomCenter)) {
                            ThreeButtonBarOverlay(barBg, barInk, barStroke)
                        }
                    }
                }
            }
        }

        for (currentStyle in AppStyle.entries) {
            for (currentDark in listOf(false, true)) {
                rule.runOnIdle {
                    style = currentStyle
                    dark = currentDark
                    shownImeDp = 0
                }
                applyInsets(navigation = Insets.of(0, 0, 0, dpToPx(48)), imeBottom = 0, statusTop = dpToPx(24))
                val composerBounds = rule.onNodeWithTag("shot-composer").getUnclippedBoundsInRoot()
                assertEquals(
                    "shot style=$currentStyle dark=$currentDark 3-button composer bottom",
                    712f,
                    composerBounds.bottom.value,
                    0.5f,
                )
                val file = File(out, "embedded-3button-${currentStyle.name.lowercase()}-${if (currentDark) "dark" else "light"}.png")
                val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                file.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
                assertTrue("wrote ${file.name}", file.isFile && file.length() > 0L)
            }
        }

        for (currentStyle in listOf(AppStyle.Brief, AppStyle.Board, AppStyle.Glass)) {
            rule.runOnIdle {
                style = currentStyle
                dark = false
                shownImeDp = 260
            }
            applyInsets(navigation = Insets.of(0, 0, 0, dpToPx(48)), imeBottom = dpToPx(260), statusTop = dpToPx(24))
            val composerBounds = rule.onNodeWithTag("shot-composer").getUnclippedBoundsInRoot()
            assertEquals(
                "shot style=$currentStyle ime=260 composer bottom",
                500f,
                composerBounds.bottom.value,
                0.5f,
            )
            val file = File(out, "embedded-ime-${currentStyle.name.lowercase()}-light.png")
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            file.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            assertTrue("wrote ${file.name}", file.isFile && file.length() > 0L)
        }
    }
}

@Composable
private fun ThreeButtonBarOverlay(background: Color, ink: Color, stroke: Color) {
    Column(Modifier.fillMaxWidth().height(48.dp).background(background)) {
        Spacer(Modifier.fillMaxWidth().height(1.dp).background(stroke))
        Row(
            Modifier.fillMaxSize(),
            horizontalArrangement = Arrangement.SpaceEvenly,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            JIcon(PhIcons.CaretLeft, null, size = 18.dp, tint = ink)
            Box(Modifier.size(16.dp).border(2.dp, ink, CircleShape))
            Box(Modifier.size(15.dp).clip(RoundedCornerShape(3.dp)).border(2.dp, ink, RoundedCornerShape(3.dp)))
        }
    }
}
