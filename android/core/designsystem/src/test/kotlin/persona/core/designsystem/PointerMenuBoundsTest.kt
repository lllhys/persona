package persona.core.designsystem

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.focusTarget
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performKeyInput
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class PointerMenuBoundsTest {
    @get:Rule val rule = createComposeRule()
    private val window = IntRect(24, 48, 1024, 768)
    private val menu = IntSize(240, 320)

    @Test fun everyCornerStaysInsideTheSafeWindow() {
        val cases = mapOf(
            Offset(24f, 48f) to IntOffset(24, 48),
            Offset(1024f, 48f) to IntOffset(784, 48),
            Offset(24f, 768f) to IntOffset(24, 448),
            Offset(1024f, 768f) to IntOffset(784, 448),
        )
        for ((anchor, expected) in cases) assertEquals(expected, pointerMenuOrigin(anchor, menu, window))
    }

    @Test fun anEdgeFlipsTheGrowthDirectionWhenTheOtherSideFits() {
        assertEquals(IntOffset(660, 100), pointerMenuOrigin(Offset(900f, 100f), menu, window))
        assertEquals(IntOffset(100, 380), pointerMenuOrigin(Offset(100f, 700f), menu, window))
    }

    @Test fun horizontalSafeInsetsAndClampingStayPhysicalAcrossLayoutDirections() {
        val insets = WindowInsets(10, 20, 30, 40)
        val size = IntSize(200, 300)
        val ltr = pointerMenuSafeBounds(size, insets, Density(1f), LayoutDirection.Ltr)
        val rtl = pointerMenuSafeBounds(size, insets, Density(1f), LayoutDirection.Rtl)
        assertEquals(IntRect(10, 20, 170, 260), ltr)
        assertEquals(ltr, rtl)
        val smallMenu = IntSize(20, 20)
        assertEquals(IntOffset(145, 20), pointerMenuOrigin(Offset(165f, 20f), smallMenu, ltr))
        assertEquals(IntOffset(145, 20), pointerMenuOrigin(Offset(165f, 20f), smallMenu, rtl))
    }

    @Test fun anOversizedMenuStartsAtTheSafeEdgeAndCanScroll() {
        assertEquals(IntOffset(24, 48), pointerMenuOrigin(Offset(700f, 500f), IntSize(1200, 900), window))
        var open by mutableStateOf(true)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                Box {
                    if (open) JPointerMenu(Offset(200f, 200f), { open = false },
                        (1..20).map { JAction("Action $it") {} }, Modifier.testTag("pointer-menu"))
                }
            }
        }
        rule.onAllNodesWithText("Action 20").assertCountEquals(1)
    }

    @OptIn(ExperimentalTestApi::class)
    @Test fun escapeClosesTheShownMenuOnce() {
        var open by mutableStateOf(true)
        var dismissals = 0
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                Box {
                    if (open) JPointerMenu(Offset(80f, 80f), { dismissals++; open = false },
                        listOf(JAction("Copy") {}), Modifier.testTag("pointer-menu"))
                }
            }
        }
        rule.onNodeWithTag("pointer-menu").performKeyInput { keyDown(Key.Escape) }
        rule.waitForIdle()
        assertFalse(open)
        assertEquals(1, dismissals)
        rule.onNodeWithTag("pointer-menu").assertDoesNotExist()
    }

    @OptIn(ExperimentalTestApi::class)
    @Test fun noVisibleMenuLeavesEscapeToTheHost() {
        var open by mutableStateOf(false)
        var escapes = 0
        val focus = FocusRequester()
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                Box(Modifier.fillMaxSize().onPreviewKeyEvent {
                    if (it.key == Key.Escape) { escapes++; true } else false
                }.focusRequester(focus).focusTarget().testTag("host")) {
                    if (open) JPointerMenu(Offset.Zero, { open = false }, listOf(JAction("Copy") {}))
                }
            }
        }
        rule.runOnUiThread { focus.requestFocus() }
        rule.onNodeWithTag("host").performKeyInput { keyDown(Key.Escape) }
        assertEquals(1, escapes)
    }
}
