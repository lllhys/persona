package persona.feature.assistant.proactive

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.click
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.time.Instant
import java.time.ZoneId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.ScheduledTask
import persona.core.designsystem.AppStyle
import persona.core.designsystem.GroupPosition
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens

/** A tap in the switch's extra touch area toggles only the switch; it must not fall through to the task row. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp")
class TaskRowTouchTargetTest {
    @get:Rule val rule = createComposeRule()

    @Test fun switchTouchAreaDoesNotOpenTheTask() {
        var style by mutableStateOf(AppStyle.Glass)
        var enabled by mutableStateOf(false)
        val toggles = mutableListOf<Boolean>()
        var opens = 0
        val task = ScheduledTask(id = "morning", title = "Morning briefing", prompt = "Brief me",
            repeats = ScheduledTask.DAILY, time = "09:00")
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                TaskRow(task.copy(enabled = enabled), null, rememberProactiveText(), Instant.parse("2026-11-10T09:00:00Z"),
                    ZoneId.of("UTC"), GroupPosition.Single, busy = false,
                    onToggle = { enabled = it; toggles += it }, onOpen = { opens++ }, onMore = {})
            }
        }
        for (nextStyle in AppStyle.entries) {
            rule.runOnIdle {
                style = nextStyle
                enabled = false
                toggles.clear()
                opens = 0
            }
            val switch = rule.onNode(SemanticsMatcher.expectValue(SemanticsProperties.Role, Role.Switch))
            val visual = switch.fetchSemanticsNode().boundsInRoot
            val touch = switch.fetchSemanticsNode().touchBoundsInRoot
            val minimum = with(rule.density) { Tokens.MinTouch.android.toPx() }
            assertTrue("$nextStyle touch width ${touch.width}", touch.width >= minimum)
            assertTrue("$nextStyle touch height ${touch.height}", touch.height >= minimum)
            val outsideTrack = Offset(visual.center.x, visual.center.y + with(rule.density) { 20.dp.toPx() })
            assertTrue("The test tap must be outside the drawn track", outsideTrack.y > visual.bottom)
            assertTrue("The test tap must be inside the switch target", touch.contains(outsideTrack))
            rule.onRoot().performTouchInput { click(outsideTrack) }
            rule.waitForIdle()
            assertEquals("$nextStyle switch callbacks", listOf(true), toggles)
            assertEquals("$nextStyle row opens", 0, opens)
        }
    }
}
