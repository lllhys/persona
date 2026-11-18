package persona.feature.chat

import android.content.Context
import android.content.pm.ApplicationInfo
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.SessionKey
import persona.core.contracts.api.DraftUploadState
import persona.core.designsystem.AdaptiveProbe
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-mdpi")
class ChatComposerAdaptiveProbeTest {
    @get:Rule val rule = createComposeRule()
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val file = File(context.noBackupFilesDir, "hub-data/adaptive-probe.json")

    @After fun tearDown() = AdaptiveProbe.disable()

    @Test fun composerPublishesOnlyStateEvidenceAndKeepsMessageBehavior() {
        context.applicationInfo.flags = context.applicationInfo.flags or ApplicationInfo.FLAG_DEBUGGABLE
        AdaptiveProbe.enable(context)
        val state = ChatComposerState()
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, reduceMotion = true) {
                ChatComposer(SessionKey("machine", "session"), state, working = false, sending = false, canClear = false,
                    service = null, onSend = {}, onCommand = {}, onNotice = {})
            }
        }
        rule.onNodeWithText("Message").performClick()
        state.value = TextFieldValue("hello adaptive", TextRange(2, 8))
        state.attachments = listOf(ComposerAttachment("attachment-1", "draft.txt", "", image = false,
            path = "/tmp/draft.txt", uploadState = DraftUploadState.Uploaded))
        val message = state.message()
        rule.waitUntil(timeoutMillis = 3_000) {
            runCatching {
                val page = Json.parseToJsonElement(file.readText()).jsonObject.getValue("pages").jsonObject
                    .getValue("chat:machine/session").jsonObject.getValue("composer").jsonObject
                page.getValue("selection").jsonObject.getValue("end").jsonPrimitive.int == 8 &&
                    page.getValue("attachments").jsonArray.size == 1
            }.getOrDefault(false)
        }
        val page = Json.parseToJsonElement(file.readText()).jsonObject.getValue("pages").jsonObject
            .getValue("chat:machine/session").jsonObject.getValue("composer").jsonObject
        assertEquals(2, page.getValue("selection").jsonObject.getValue("start").jsonPrimitive.int)
        assertEquals(8, page.getValue("selection").jsonObject.getValue("end").jsonPrimitive.int)
        assertEquals(true, page.getValue("focused").jsonPrimitive.boolean)
        assertEquals("attachment-1", page.getValue("attachments").jsonArray.single().jsonObject
            .getValue("id").jsonPrimitive.content)
        assertEquals("Uploaded", page.getValue("attachments").jsonArray.single().jsonObject
            .getValue("uploadState").jsonPrimitive.content)
        assertFalse(file.readText().contains("hello adaptive"))
        assertFalse(file.readText().contains("/tmp/draft.txt"))
        assertEquals(message, state.message())
        assertEquals(TextRange(2, 8), state.value.selection)
    }
}
