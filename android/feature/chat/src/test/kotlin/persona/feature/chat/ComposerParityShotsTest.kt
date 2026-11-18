package persona.feature.chat

import android.content.Context
import android.content.Intent
import android.content.pm.ActivityInfo
import android.content.pm.ResolveInfo
import android.content.res.Configuration
import android.graphics.Bitmap
import android.speech.RecognizerIntent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
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
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.contracts.Session
import persona.core.designsystem.*
import java.io.File

/**
 * The composer's parity pieces (#2976) as they draw, in every style and appearance: the @ menu with a quote and the
 * host's failure note over it; the Agent and main-chat "/" lists, and the /loop line; the mic beside send with a draft.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
class ComposerParityShotsTest {
    @get:Rule val rule = createComposeRule()

    private enum class Scene { Mentions, Slash, MainSlash, Loop, Dictation }

    private val session = Session(machineId = "box", sessionId = "s1", title = "修复登录", cwd = "/w/app", status = "idle",
        state = "unread", failureReason = "⚠️ 模型接口连续报错，已停下这一轮", externalUrl = "https://applink.example.com/thread", updatedAt = "2026-11-05T10:00:00Z")
    private val others = listOf(
        Session(machineId = "box", sessionId = "s2", title = "照片同步支持 HEIC", cwd = "/w/photos", agent = "codex", updatedAt = "2026-11-05T09:00:00Z"),
        Session(machineId = "box", sessionId = "s3", title = "首页改版", cwd = "/w/web", updatedAt = "2026-11-04T09:00:00Z"),
    )
    private val files = listOf(
        Item("1", kind = "tool_use", toolInput = buildJsonObject { put("file_path", "/w/app/src/login/LoginView.kt") }),
        Item("2", kind = "tool_use", toolInput = buildJsonObject { put("file_path", "/w/app/README.md") }),
    )

    @Before fun recognizer() {
        // The phone's voice input screen, so the composer offers its mic (Dictation.kt asks the package manager).
        val info = ResolveInfo().apply { activityInfo = ActivityInfo().apply { packageName = "com.example.voice"; name = "Voice" } }
        shadowOf(ApplicationProvider.getApplicationContext<Context>().packageManager)
            .addResolveInfoForIntent(Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH), info)
    }

    @Composable
    private fun SceneContent(scene: Scene) {
        val state = remember(scene) {
            ChatComposerState().apply {
                val text = when (scene) {
                    Scene.Mentions -> "看下 @"
                    Scene.Slash, Scene.MainSlash -> "/"
                    Scene.Loop -> "/loop 10m 看下 CI 有没有过"
                    Scene.Dictation -> "先跑一遍测试，然后"
                }
                value = TextFieldValue(text, TextRange(text.length))
            }
        }
        val quote = ChatLinkContext.quote("登录页在弱网下会一直转圈，重试按钮没有反应。", "Claude", "m1")
        CompositionLocalProvider(LocalMainChatClear provides (if (scene == Scene.MainSlash) MainChatClear { } else null)) {
            Column(Modifier.fillMaxSize()) {
                Spacer(Modifier.weight(1f))
                if (scene == Scene.Mentions) ChatHostNote(session, retried = null, canRetry = true, retryEnabled = true, onRetry = {})
                ChatComposer(session.key, state, working = false, sending = false, canClear = true, service = null,
                    onSend = {}, onCommand = {}, onNotice = {}, assistantChat = scene == Scene.MainSlash,
                    accessory = if (scene == Scene.Mentions) ({ ChatContextCard(quote) {} }) else null,
                    mentions = ComposerMentions(chatFiles(files), session.cwd, listOf(session) + others, session.key),
                    onModel = {}, loop = true)
            }
        }
    }

    @Test fun parityPiecesInEveryStyleAndAppearance() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var scene by mutableStateOf(Scene.Mentions)
        rule.setContent {
            val configuration = Configuration(LocalConfiguration.current).apply {
                uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or
                    if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
            }
            CompositionLocalProvider(LocalConfiguration provides configuration) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Box(Modifier.testTag("shot").width(400.dp).height(640.dp).background(PersonaTheme.palette.background)) {
                        SceneContent(scene)
                    }
                }
            }
        }
        val out = File(System.getProperty("persona.shots") ?: "build/composer-parity-shots").apply { mkdirs() }
        for (sc in Scene.entries) for (s in AppStyle.entries) for (d in listOf(false, true)) {
            scene = sc; style = s; dark = d
            rule.waitForIdle()
            when (sc) {
                Scene.Mentions -> {
                    rule.onNodeWithText("LoginView.kt").assertExists()
                    rule.onNodeWithText("照片同步支持 HEIC").assertExists()
                    rule.onNodeWithText("模型接口连续报错，已停下这一轮").assertExists()
                    rule.onNodeWithText("重试").assertExists()
                    rule.onNodeWithText("在飞书打开").assertExists()
                    rule.onNodeWithText("引用 Claude").assertExists()
                    rule.onNodeWithContentDescription("引用文件或会话").assertExists()
                }
                Scene.Slash -> { rule.onNodeWithText("/model").assertExists(); rule.onNodeWithText("/loop").assertExists() }
                Scene.MainSlash -> {
                    rule.onNodeWithText("/clear").assertExists()
                    rule.onNodeWithText("清空对话，从头开始").assertExists()
                    rule.onNodeWithText("/compact").assertDoesNotExist()
                    rule.onNodeWithText("/model").assertDoesNotExist()
                    rule.onNodeWithText("/loop").assertDoesNotExist()
                    rule.onNodeWithText("/继续").assertDoesNotExist()
                }
                Scene.Loop -> rule.onNodeWithText("每 10 分钟重复：看下 CI 有没有过").assertExists()
                Scene.Dictation -> rule.onNodeWithContentDescription("听写").assertExists()
            }
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out, "${sc.name.lowercase()}-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream().use {
                bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)
            }
        }
    }
}
