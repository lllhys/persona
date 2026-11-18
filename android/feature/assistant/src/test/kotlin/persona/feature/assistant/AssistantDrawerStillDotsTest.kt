package persona.feature.assistant

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.designsystem.AppStyle
import persona.core.designsystem.NavItem
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.icons.PhIcons

/**
 * The drawer's Needs You dots are still with motion on, as iOS draws them: Settings' badge (`ExtensionSettingsBadge`, a
 * still dot) and a page's (its badge's words, nothing that breathes). A conversation that needs you is listed with a still
 * dot too (`RowStateMark`). Caught twice, 400 ms apart, in Glass: a breath would change the pixels.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
class AssistantDrawerStillDotsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun theDrawersDotsAreStill() {
        val waiting = Session(machineId = "hub", sessionId = "w1", title = "Weekend plans", status = "waiting", state = "needs_you")
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = false, style = AppStyle.Glass) {
                Box(Modifier.testTag("shot").width(400.dp).height(1100.dp).background(PersonaTheme.palette.background)) {
                    AssistantDrawer("", {}, ConversationGroups(null, emptyList(), listOf(waiting)), active = null, hasMain = false,
                        onEnd = null, onMain = {}, onOpen = {}, onInfo = {}, onNew = {}, onSettings = {}, onTodos = {}, todoCount = 0,
                        onInbox = {}, inboxCount = 0, settingsDot = true,
                        pages = listOf(NavItem("extension", "Extension", PhIcons.PuzzlePiece, dot = true)), onPage = {})
                }
            }
        }
        repeat(3) { rule.mainClock.advanceTimeByFrame() }
        val first = shot()
        rule.mainClock.advanceTimeBy(400)
        assertTrue("a dot in the drawer moved", first.sameAs(shot()))
    }

    private fun shot(): Bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
}
