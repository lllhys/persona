package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
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
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Interaction
import persona.core.contracts.Question
import persona.core.contracts.QuestionOption
import persona.core.contracts.Resolution
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1000dp-xhdpi", application = QuestionTestApplication::class)
class RecommendedWhitespaceShotsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun displayMatchesIosWhileTheAnswerKeepsItsOriginalLabel() {
        val label = "\u200b \tKeep current settings\n \u200b (Recommended)"
        val question = "Which option?"
        val card = Interaction("example_request", machineId = "example_machine", sessionId = "example_session",
            kind = "question", questions = listOf(Question(question, options = listOf(QuestionOption(label)))))
        val sent = mutableListOf<Resolution>()
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var fontScale by mutableStateOf(1f)
        rule.setContent {
            val density = LocalDensity.current.density
            CompositionLocalProvider(LocalDensity provides Density(density, fontScale)) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background)) {
                        InteractionPanel(listOf(card), null, 1000.dp, null, emptySet(), { _, answer -> sent += answer }, null, {})
                    }
                }
            }
        }
        val output = File(System.getProperty("persona.shots") ?: "build/recommended-whitespace-shots").also { it.mkdirs() }
        for (scale in listOf(1f, 2f)) for (nextStyle in AppStyle.entries) for (nextDark in listOf(false, true)) {
            fontScale = scale; style = nextStyle; dark = nextDark
            rule.waitForIdle()
            rule.onNodeWithText("1. Keep current settings\n").assertExists()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            val name = "recommended-${style.key}-${if (dark) "dark" else "light"}-${scale.toInt()}x.png"
            File(output, name).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        rule.onNodeWithText("1. Keep current settings\n").performClick()
        rule.waitForIdle()
        assertEquals(1, sent.size)
        assertEquals(mapOf(question to label), sent.single().answers)
    }
}
