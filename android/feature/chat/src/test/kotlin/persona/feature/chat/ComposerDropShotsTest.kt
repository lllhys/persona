package persona.feature.chat

import android.content.Context
import android.content.res.Configuration
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import java.util.Locale
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.After
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatComposerService
import persona.core.contracts.api.ComposerDelivery
import persona.core.contracts.api.DraftUploadState
import persona.core.designsystem.AppStyle
import persona.core.designsystem.ComposerDropTargetFrame
import persona.core.designsystem.PersonaTheme

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w640dp-h900dp-xhdpi")
class ComposerDropShotsTest {
    @get:Rule val rule = createComposeRule()

    private enum class Scene { Drop, Importing, Uploading, Failed, Uploaded }

    private lateinit var source: File
    private lateinit var sourceUri: String

    @Before fun attachmentSource() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        useChatFileProvider(context)
        source = File(File(context.cacheDir, "chat-captures").apply { mkdirs() }, "drop-shot.txt")
        source.writeText("shot")
        sourceUri = FileProvider.getUriForFile(context, "${context.packageName}.chat.files", source).toString()
    }

    @After fun removeSource() {
        source.delete()
    }

    @Composable
    private fun DropScene(scene: Scene, style: AppStyle, dark: Boolean, locale: Locale, largeText: Boolean) {
        val configuration = Configuration(LocalConfiguration.current).apply {
            uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
            if (largeText) {
                fontScale = 1.6f
                setLocale(locale)
            }
        }
        CompositionLocalProvider(LocalConfiguration provides configuration) {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val failure = stringResource(R.string.chat_composer_upload_failed)
                val state = remember(scene, locale) {
                    ChatComposerState().apply {
                        if (scene != Scene.Drop && scene != Scene.Importing) attachments = listOf(ComposerAttachment(
                            "drop-shot", "A readable report with a long filename.txt", sourceUri, false,
                            path = if (scene == Scene.Uploaded) "/remote/report.txt" else null,
                            error = if (scene == Scene.Failed) failure else null,
                            uploadState = when (scene) {
                                Scene.Uploading -> DraftUploadState.Uploading
                                Scene.Failed -> DraftUploadState.Failed
                                Scene.Uploaded -> DraftUploadState.Uploaded
                                else -> DraftUploadState.Pending
                            },
                        ))
                    }
                }
                val service = remember(scene, locale) { ShotComposerService() }
                Box(Modifier.testTag("shot").width(600.dp).height(640.dp).background(PersonaTheme.palette.background)) {
                    Column(Modifier.fillMaxSize()) {
                        Spacer(Modifier.weight(1f))
                        ComposerDropTargetFrame(
                            active = scene == Scene.Drop || scene == Scene.Importing,
                            importing = scene == Scene.Importing,
                            label = stringResource(R.string.chat_composer_drop_target),
                            importingLabel = stringResource(R.string.chat_composer_drop_importing),
                        ) {
                            ChatComposer(SessionKey("box", "s1"), state, working = false, sending = false,
                                canClear = false, service = service, onSend = {}, onCommand = {}, onNotice = {})
                        }
                    }
                }
            }
        }
    }

    @Test fun dropAndAttachmentStatesInEveryStyleAndAppearance() {
        var scene by mutableStateOf(Scene.Drop)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent { DropScene(scene, style, dark, Locale.SIMPLIFIED_CHINESE, largeText = false) }
        val out = File(System.getProperty("persona.shots") ?: "build/composer-drop-shots").apply { mkdirs() }
        for (sc in Scene.entries) for (s in AppStyle.entries) for (d in listOf(false, true)) {
            scene = sc; style = s; dark = d
            rule.waitForIdle()
            if (sc == Scene.Drop || sc == Scene.Importing) {
                rule.onNodeWithTag("composer-drop-target").assert(SemanticsMatcher.keyIsDefined(SemanticsProperties.StateDescription))
            }
            if (sc == Scene.Failed) rule.onNodeWithText("重试").assertIsDisplayed()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out, "${sc.name.lowercase()}-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)
            }
        }
    }

    @Config(sdk = [35], qualifiers = "en-w640dp-h900dp-xhdpi")
    @Test fun failureAndRetryInEnglishAtMaximumText() = failureAndRetryAtMaximumText(Locale.ENGLISH)

    @Config(sdk = [35], qualifiers = "zh-w640dp-h900dp-xhdpi")
    @Test fun failureAndRetryInChineseAtMaximumText() = failureAndRetryAtMaximumText(Locale.SIMPLIFIED_CHINESE)

    @Config(sdk = [35], qualifiers = "ja-w640dp-h900dp-xhdpi")
    @Test fun failureAndRetryInJapaneseAtMaximumText() = failureAndRetryAtMaximumText(Locale.JAPANESE)

    private fun failureAndRetryAtMaximumText(language: Locale) {
        RuntimeEnvironment.setFontScale(1.6f)
        var style by mutableStateOf(AppStyle.Glass)
        rule.setContent { DropScene(Scene.Failed, style, dark = false, locale = language, largeText = true) }
        val out = File(System.getProperty("persona.shots") ?: "build/composer-drop-localized-shots").apply { mkdirs() }
        for (s in AppStyle.entries) {
            style = s
            rule.waitForIdle()
            val retry = when (language) {
                Locale.SIMPLIFIED_CHINESE -> "重试"
                Locale.JAPANESE -> "再試行"
                else -> "Retry"
            }
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out, "failed-${s.name.lowercase()}-${language.language}-large-text.png").outputStream().use {
                bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)
            }
            rule.onNodeWithText(retry).assertIsDisplayed()
        }
    }
}

private class ShotComposerService : ChatComposerService {
    override val deliveries = MutableStateFlow<Map<String, ComposerDelivery>>(emptyMap())
    private val gate = CompletableDeferred<Unit>()

    override suspend fun upload(session: SessionKey, name: String, file: File): String {
        gate.await()
        return "/remote/$name"
    }

    override suspend fun interrupt(session: SessionKey) = Unit

    override suspend fun retryRefused(clientMessageId: String, clearDraft: Boolean): ComposerDelivery =
        error("not used by composer drop shots")
}
