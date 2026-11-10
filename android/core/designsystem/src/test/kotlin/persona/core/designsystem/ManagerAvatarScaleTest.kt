package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Row
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.Density
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.HubManager

/**
 * An assistant's monogram is a mark: its letter is sized by the circle, so the system's text size leaves it as it is
 * (at 200 % the letter outgrew the circle and covered the presence dot). The head's and the roster's sizes alike.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class ManagerAvatarScaleTest {
    @get:Rule val rule = createComposeRule()

    @Test fun theLetterIsSizedByTheCircleNotByTheTextSize() {
        var scale by mutableStateOf(1f)
        rule.setContent {
            CompositionLocalProvider(LocalDensity provides Density(LocalDensity.current.density, scale)) {
                PersonaTheme(darkTheme = false, reduceMotion = true) {
                    Row(Modifier.testTag("monograms").background(PersonaTheme.palette.background)) {
                        JManagerAvatar(HubManager("assistant", "助手", online = true, isDefault = true), isDefault = true, presence = true)
                        JManagerAvatar(HubManager("m-3fa21c", "工作", online = true), isDefault = false,
                            size = HomeTokens.assistantAvatarSmall, presence = true)
                    }
                }
            }
        }
        fun capture() = rule.onNodeWithTag("monograms").captureToImage().asAndroidBitmap()
        val normal = capture()
        scale = 2f
        rule.waitForIdle()
        val large = capture()
        assertTrue("the monograms changed with the text size", normal.sameAs(large))
    }
}
