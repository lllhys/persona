package persona.core.designsystem

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.ExperimentalTestApi
import androidx.compose.ui.test.click
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performMouseInput
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.rightClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class PointerClickTest {
    @get:Rule val rule = createComposeRule()

    @OptIn(ExperimentalTestApi::class)
    @Test fun onlyASecondaryPressReportsItsWindowOffset() {
        var clicked by mutableStateOf<Offset?>(null)
        var bounds by mutableStateOf(Rect.Zero)
        rule.setContent {
            Box(Modifier.size(120.dp).onGloballyPositioned { bounds = it.boundsInWindow() }
                .onSecondaryClickAt { clicked = it }.testTag("target"))
        }
        val target = rule.onNodeWithTag("target")
        target.performMouseInput { moveTo(center) }
        assertNull(clicked)
        target.performMouseInput { click() }
        assertNull(clicked)
        target.performTouchInput { down(center); up() }
        assertNull(clicked)
        target.performMouseInput { rightClick() }
        rule.waitForIdle()
        val point = requireNotNull(clicked)
        assertEquals(bounds.center.x, point.x, 0.5f)
        assertEquals(bounds.center.y, point.y, 0.5f)
    }

    @OptIn(ExperimentalTestApi::class)
    @Test fun theParameterlessSecondaryClickKeepsWorking() {
        var openings = 0
        rule.setContent {
            Box(Modifier.size(120.dp).onSecondaryClick { openings++ }.testTag("target"))
        }
        rule.onNodeWithTag("target").performMouseInput { rightClick() }
        rule.waitForIdle()
        assertEquals(1, openings)
    }
}
