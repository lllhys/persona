package persona.feature.settings

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.HubConnector
import persona.core.contracts.api.KnowledgeDetail
import persona.core.contracts.api.KnowledgeList
import persona.core.contracts.api.KnowledgeSelection
import persona.core.contracts.api.KnowledgeSelectionCandidate
import persona.core.contracts.api.KnowledgeSelectionCandidates
import persona.core.contracts.api.KnowledgeSelectionChange
import persona.core.designsystem.AppStyle
import persona.core.designsystem.JListPage
import persona.core.designsystem.PersonaTheme
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import java.io.File

/**
 * Every fixture is drawn at 360dp in Glass/Brief/Board, dark and light, with real 1x/2x LocalDensity text. One PNG per
 * fixture/locale/scale contains the six labelled style/theme frames, so every captured frame can be reviewed together.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35])
class KnowledgeSelectionShotsTest {
    @get:Rule val rule = createComposeRule()

    private class FakeSelection(var stored: JsonObject, private val failure: HubFailure? = null) : KnowledgeSelection {
        override val changes: Flow<KnowledgeSelectionChange> = emptyFlow()
        val puts = mutableListOf<JsonObject>()
        var reads = 0
        override suspend fun selection(): JsonObject { reads++; return stored }
        override suspend fun put(document: JsonObject): JsonObject {
            puts += document
            failure?.let { throw it }
            stored = document
            return document
        }
        override suspend fun candidates(source: String) = KnowledgeSelectionCandidates(source = source)
        override suspend fun documents() = KnowledgeList()
        override suspend fun document(id: String): KnowledgeDetail = error("not used")
    }

    private enum class Scenario(val file: String, val height: Int) {
        Defaults("defaults", 2400),
        SharedLevels("multiple-shared-levels", 2400),
        RuleDays("rule-days", 2400),
        CachedPicker("cached-chat-picker-estimate", 1500),
        ReadOnly("read-only", 2400),
        Refused("refused-change", 2400),
    }

    @Test @Config(sdk = [35], qualifiers = "en-w360dp-h2400dp-mdpi")
    fun englishShots() = shots("en")

    @Test @Config(sdk = [35], qualifiers = "zh-w360dp-h2400dp-mdpi")
    fun chineseShots() = shots("zh")

    private fun shots(locale: String) {
        val out = File(System.getProperty("persona.shots") ?: "build/knowledge-selection-shots").also { it.mkdirs() }
        var scenario by mutableStateOf(Scenario.Defaults)
        var api by mutableStateOf(FakeSelection(document(scenario).root))
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var scale by mutableStateOf(1f)
        rule.setContent {
            val density = LocalDensity.current
            CompositionLocalProvider(LocalDensity provides Density(density.density, scale)) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Box(Modifier.width(360.dp).height(scenario.height.dp).background(PersonaTheme.palette.background).testTag("shot")) {
                        if (scenario == Scenario.CachedPicker) CachedPicker()
                        else KnowledgeSelectionPage(api, null, onBack = {})
                    }
                }
            }
        }
        Scenario.entries.forEach { nextScenario ->
            val failure = when (nextScenario) {
                Scenario.ReadOnly -> HubFailure(HubError.Server, "read-only device", 403)
                Scenario.Refused -> HubFailure(HubError.Server, "days must be from 1 to 36500", 400)
                else -> null
            }
            rule.runOnIdle {
                scenario = nextScenario
                api = FakeSelection(document(nextScenario).root, failure)
                style = AppStyle.Glass
                dark = false
                scale = 1f
                Snapshot.sendApplyNotifications()
            }
            val scenario = nextScenario
            if (scenario == Scenario.CachedPicker) {
                rule.waitUntil(5_000) { rule.onAllNodesWithText("Product design", substring = true).fetchSemanticsNodes().isNotEmpty() ||
                    rule.onAllNodesWithText("产品讨论组", substring = true).fetchSemanticsNodes().isNotEmpty() }
            } else {
                rule.waitUntil(5_000) { api.reads > 0 }
                if (scenario == Scenario.ReadOnly || scenario == Scenario.Refused) {
                    rule.onNodeWithTag("knowledge-rule-messages-mentions").performClick()
                    rule.onNode(hasSetTextAction() and hasAnyAncestor(hasTestTag("knowledge-rule-days"))).performTextInput("30")
                    rule.onNodeWithTag("knowledge-rule-save").assertIsEnabled().performClick()
                    rule.waitUntil(5_000) { api.puts.size == 1 }
                }
            }
            rule.waitForIdle()
            val frames = mutableMapOf<Triple<Float, AppStyle, Boolean>, Bitmap>()
            for (textScale in listOf(1f, 2f)) for (appStyle in AppStyle.entries) for (darkTheme in listOf(false, true)) {
                rule.runOnIdle {
                    scale = textScale
                    style = appStyle
                    dark = darkTheme
                    Snapshot.sendApplyNotifications()
                }
                rule.waitForIdle()
                Snapshot.sendApplyNotifications()
                rule.waitForIdle()
                val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                assertTrue("unexpected 360dp capture width ${bitmap.width}", bitmap.width == 360)
                frames[Triple(textScale, appStyle, darkTheme)] = bitmap
            }
            for (textScale in listOf(1f, 2f)) {
                for (appStyle in AppStyle.entries) {
                    assertFalse("light and dark must differ", frames[Triple(textScale, appStyle, false)]!!
                        .sameAs(frames[Triple(textScale, appStyle, true)]!!))
                    assertFalse("1x and 2x text must differ", frames[Triple(1f, appStyle, false)]!!
                        .sameAs(frames[Triple(2f, appStyle, false)]!!))
                }
                writeComposite(File(out, "${scenario.file}-$locale-${textScale.toInt()}x.png"), textScale, frames)
            }
        }
    }

    @Test @Config(sdk = [35], qualifiers = "en-w360dp-h1200dp-mdpi")
    fun levelActionsSheet() {
        val out = File(System.getProperty("persona.shots") ?: "build/knowledge-selection-shots").also { it.mkdirs() }
        val api = FakeSelection(document(Scenario.SharedLevels).root)
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                KnowledgeSelectionPage(api, null, onBack = {})
            }
        }
        rule.waitUntil(5_000) { api.reads > 0 }
        rule.onNodeWithTag("knowledge-priority-messages-1").performClick()
        rule.waitForIdle()
        val frames = mutableListOf<Bitmap>()
        for ((nextStyle, nextDark) in listOf(AppStyle.Glass to false, AppStyle.Board to true)) {
            rule.runOnIdle {
                style = nextStyle
                dark = nextDark
                Snapshot.sendApplyNotifications()
            }
            rule.waitForIdle()
            frames += rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
        }
        val header = 48
        val width = frames.maxOf { it.width }
        val height = frames.maxOf { it.height }
        val bitmap = Bitmap.createBitmap(width * frames.size, height + header, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(bitmap)
        val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply { textSize = 24f }
        frames.forEachIndexed { index, frame ->
            val left = index * width
            paint.color = android.graphics.Color.rgb(232, 232, 232)
            canvas.drawRect(left.toFloat(), 0f, (left + width).toFloat(), header.toFloat(), paint)
            paint.color = android.graphics.Color.BLACK
            canvas.drawText(if (index == 0) "Glass · light · level actions" else "Board · dark · level actions",
                left + 12f, 33f, paint)
            canvas.drawBitmap(frame, left.toFloat(), header.toFloat(), null)
        }
        File(out, "level-actions-en.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @androidx.compose.runtime.Composable
    private fun CachedPicker() {
        val zh = RuntimeEnvironment.getApplication().resources.configuration.locales[0].language == "zh"
        val candidates = KnowledgeSelectionCandidates(
            source = "lark", cached = true, listedAt = "2026-11-15T04:00:00Z",
            chats = listOf(
                KnowledgeSelectionCandidate(id = "design", name = if (zh) "产品讨论组" else "Product design", kind = "group",
                    source = "lark-team", estimate = 1280, read = "whole", days = 30),
                KnowledgeSelectionCandidate(id = "direct", name = if (zh) "直接消息" else "Direct message", kind = "dm",
                    source = "lark-personal", estimate = 46, read = "personal"),
            ),
        )
        JListPage(title = if (zh) "添加聊天" else "Add chat") {
            item("picker") {
                KnowledgeChatPickerContent(
                    sources = listOf(HubConnector("lark", "Lark", capabilities = listOf("history"))), source = "lark",
                    candidates = candidates, loading = false, unavailable = false, error = null,
                    onSource = {}, onCandidate = {}, onRetry = {},
                )
            }
        }
    }

    private fun document(scenario: Scenario): KnowledgeSelectionDocument {
        val base = KnowledgeSelectionDocument.defaults()
        return when (scenario) {
            Scenario.SharedLevels -> base.regroup(KnowledgeSelectionPart.Messages, "dm", joinPrevious = true)
                .regroup(KnowledgeSelectionPart.Messages, "bot", joinPrevious = true)
            Scenario.RuleDays -> base.setRule(KnowledgeSelectionPart.Messages, "mentions", true, 30, 10, false, null)
                .setMineContext(25)
                .setItem(KnowledgeSelectionItem.newChat("lark", "design", "Product design")
                    .copy(mode = KnowledgeItemMode.All, days = 45))
            else -> base
        }
    }

    private fun writeComposite(
        file: File,
        scale: Float,
        frames: Map<Triple<Float, AppStyle, Boolean>, Bitmap>,
    ) {
        val sample = frames.getValue(Triple(scale, AppStyle.Glass, false))
        val header = 48
        val bitmap = Bitmap.createBitmap(sample.width * AppStyle.entries.size, (sample.height + header) * 2, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(bitmap)
        val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply { textSize = 24f }
        AppStyle.entries.forEachIndexed { column, style ->
            for ((row, dark) in listOf(false, true).withIndex()) {
                val frame = frames.getValue(Triple(scale, style, dark))
                val left = column * frame.width
                val top = row * (frame.height + header)
                paint.color = android.graphics.Color.rgb(232, 232, 232)
                canvas.drawRect(left.toFloat(), top.toFloat(), (left + frame.width).toFloat(), (top + header).toFloat(), paint)
                paint.color = android.graphics.Color.BLACK
                canvas.drawText("${style.name} · ${if (!dark) "light" else "dark"} · ${scale.toInt()}x", left + 12f, top + 33f, paint)
                canvas.drawBitmap(frame, left.toFloat(), (top + header).toFloat(), null)
            }
        }
        file.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}
