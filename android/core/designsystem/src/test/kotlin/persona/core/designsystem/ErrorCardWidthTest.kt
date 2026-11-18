package persona.core.designsystem

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** The error card's Retry keeps its own width. A tight min width used to stretch it to the card. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
class ErrorCardWidthTest {
    @get:Rule val rule = createComposeRule()

    @Test fun theActionStaysNarrowerThanTheCardOpenAndClosed() {
        var open by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, reduceMotion = true) {
                Box(Modifier.width(360.dp)) {
                    ErrorCard(
                        "Request failed",
                        emptyList(),
                        open,
                        { open = !open },
                        Modifier.testTag("card"),
                        action = {
                            JButton(
                                "Retry",
                                {},
                                Modifier.testTag("action"),
                                kind = JButtonKind.Secondary,
                                size = JButtonSize.Medium,
                            )
                        },
                    ) {
                        Text("Request id and the raw text")
                    }
                }
            }
        }
        val closed = width("action")
        assertTrue("closed action $closed card ${width("card")}", closed < width("card"))
        rule.runOnIdle { open = true }
        rule.waitForIdle()
        val opened = width("action")
        assertTrue("open action $opened card ${width("card")}", opened < width("card"))
        assertEquals(closed, opened)
    }

    private fun width(tag: String): Int =
        rule.onNodeWithTag(tag, useUnmergedTree = true).fetchSemanticsNode().size.width
}
