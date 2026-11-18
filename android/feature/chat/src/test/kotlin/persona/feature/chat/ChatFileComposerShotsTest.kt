package persona.feature.chat

import android.content.Context
import android.content.res.Configuration
import android.graphics.Bitmap
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatComposerService
import persona.core.contracts.api.ComposerDelivery
import persona.core.contracts.api.DraftUploadState
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/** The real composer's file states in every style, appearance, language and text scale. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ChatFileComposerShotsTest {
    @get:Rule val rule = createComposeRule()
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private lateinit var uri: Uri
    private val key = SessionKey("machine", "session")
    private val service = object : ChatComposerService {
        override val deliveries = MutableStateFlow(emptyMap<String, ComposerDelivery>())
        override suspend fun upload(session: SessionKey, name: String, file: File): String = awaitCancellation()
        override suspend fun interrupt(session: SessionKey) = Unit
        override suspend fun retryRefused(clientMessageId: String, clearDraft: Boolean): ComposerDelivery = error("unused")
    }

    @Before fun provider() {
        useChatFileProvider(context)
        val folder = File(context.cacheDir, "chat-captures").apply { mkdirs() }
        val file = File(folder, "composer-shot.txt").apply { writeText("attachment") }
        uri = FileProvider.getUriForFile(context, "${context.packageName}.chat.files", file)
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
    @Test fun english() = matrix("en", 1f)
    @Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi", fontScale = 2f)
    @Test fun englishLargeText() = matrix("en", 2f)
    @Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
    @Test fun chinese() = matrix("zh-Hans", 1f)
    @Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi", fontScale = 2f)
    @Test fun chineseLargeText() = matrix("zh-Hans", 2f)

    private enum class Scene { Uploading, Failed, Ready }

    private fun matrix(language: String, fontScale: Float) {
        val resources = context.resources
        val uploading = resources.getString(R.string.chat_attach_uploading)
        val retry = resources.getString(R.string.chat_attach_retry)
        val uploaded = resources.getString(R.string.chat_attach_uploaded)
        val send = resources.getString(persona.core.designsystem.R.string.designsystem_send)
        var scene by mutableStateOf(Scene.Uploading)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            val state = remember(scene) {
                ChatComposerState().apply {
                    value = androidx.compose.ui.text.input.TextFieldValue("Review these files")
                    attachments = when (scene) {
                        Scene.Uploading -> listOf(attachment("uploading-report.txt"))
                        Scene.Failed -> listOf(attachment("failed-report.txt").copy(
                            error = resources.getString(R.string.chat_composer_upload_failed), uploadState = DraftUploadState.Failed))
                        Scene.Ready -> List(2) { index -> attachment("ready-$index.txt").copy(
                            // An uploaded path is enough: a revoked local picker grant must not invalidate the remote file.
                            uri = "content://example.com/revoked-$index", path = "/up/ready-$index.txt", uploadState = DraftUploadState.Uploaded) }
                    }
                }
            }
            val configuration = Configuration(LocalConfiguration.current).apply {
                uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                    if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
            }
            CompositionLocalProvider(LocalConfiguration provides configuration,
                LocalDensity provides Density(LocalDensity.current.density, fontScale)) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Column(Modifier.testTag("shot").width(400.dp).height(480.dp).background(PersonaTheme.palette.background)) {
                        Spacer(Modifier.weight(1f))
                        ChatComposer(key, state, working = false, sending = false, canClear = false, service = service,
                            onSend = {}, onCommand = {}, onNotice = {})
                    }
                }
            }
        }
        val out = File("build/file-composer-shots-8527").apply { mkdirs() }
        for (sc in Scene.entries) {
            rule.runOnIdle { scene = sc }
            rule.waitForIdle()
            when (sc) {
                Scene.Uploading -> {
                    rule.waitUntil(timeoutMillis = 5_000) { rule.onAllNodesWithText(uploading).fetchSemanticsNodes().isNotEmpty() }
                    rule.onNodeWithContentDescription(send).assertIsNotEnabled()
                }
                Scene.Failed -> {
                    rule.waitUntil(timeoutMillis = 5_000) { rule.onAllNodesWithText(retry).fetchSemanticsNodes().isNotEmpty() }
                    rule.onNodeWithContentDescription(send).assertIsNotEnabled()
                }
                Scene.Ready -> {
                    rule.waitUntil(timeoutMillis = 5_000) { rule.onAllNodesWithText(uploaded).fetchSemanticsNodes().size == 2 }
                    rule.onNodeWithContentDescription(send).assertIsEnabled()
                }
            }
            for (s in AppStyle.entries) for (night in listOf(false, true)) {
                rule.runOnIdle { style = s; dark = night }
                rule.waitForIdle()
                val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                assertTrue(bitmap.width > 0 && bitmap.height > 0)
                val name = "composer-${sc.name.lowercase()}-$language-${s.name.lowercase()}-${if (night) "dark" else "light"}-${fontScale.text()}.png"
                File(out, name).outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
            }
        }
    }

    @Config(sdk = [35])
    @Test fun healthDestinationRestartsAPendingUploadAndUsesTheStagedSize() {
        val destinations = java.util.concurrent.CopyOnWriteArrayList<String>()
        val uploads = object : ChatComposerService {
            override val deliveries = MutableStateFlow(emptyMap<String, ComposerDelivery>())
            override suspend fun upload(session: SessionKey, name: String, file: File): String = error("media not used")
            override suspend fun upload(session: SessionKey, name: String, file: File, destination: String): String {
                destinations += destination
                if (destinations.size == 1) awaitCancellation()
                return "/up/health.txt"
            }
            override suspend fun interrupt(session: SessionKey) = Unit
            override suspend fun retryRefused(clientMessageId: String, clearDraft: Boolean): ComposerDelivery = error("unused")
        }
        val state = ChatComposerState().apply {
            value = androidx.compose.ui.text.input.TextFieldValue("Read")
            attachments = listOf(attachment("health.txt").copy(size = null))
        }
        var destination by mutableStateOf<String?>(null)
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                ChatComposer(key, state, working = false, sending = false, canClear = false, service = uploads,
                    uploadDestination = destination, onSend = {}, onCommand = {}, onNotice = {})
            }
        }
        rule.waitUntil(timeoutMillis = 5_000) { destinations == listOf("attachment") }
        rule.runOnIdle { destination = "health" }
        rule.waitForIdle()
        rule.waitUntil(timeoutMillis = 5_000) { destinations == listOf("attachment", "health") }
        rule.waitUntil(timeoutMillis = 5_000) { state.attachments.single().path == "/up/health.txt" }
        assertEquals(listOf("attachment", "health"), destinations)
        assertEquals(10L, state.attachments.single().size)
    }

    private fun attachment(name: String) = ComposerAttachment(name, name, uri.toString(), false, size = 10)
    private fun Float.text() = if (this == 1f) "1x" else "2x"
}
