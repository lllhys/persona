package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

/**
 * Source subscriptions (#2041): the Subscriptions row on an extension's details and on a connector's page, shown only once the hub
 * says they deliver (source_subscriptions) and only for a connector that can be subscribed; it opens the pages for that one.
 * Shots of each, before (no feature: as today) and after, in every style, light and dark, to `build/subscriptions-entry-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
class SubscriptionsEntryShotsTest {
    @get:Rule val rule = createComposeRule()

    private val detail = ExtensionDetail(HubExtension("telegram", "Telegram", "connectors", "running", source = "builtin", enabled = true,
        description = "Telegram 连接器（personad 自带）", contributes = mapOf("connectors" to listOf("telegram"))))
    private val telegram = HubConnector("telegram", "Telegram", status = "ok", capabilities = listOf("inbox"), enabled = true, inbox = false)
    private val github = HubConnector("github", "GitHub", status = "ok", capabilities = listOf("inbox"), enabled = true, inbox = true, account = "example")
    private val shown = MutableStateFlow(false)

    private val extensions = object : ExtensionApiStub() {
        override val subscriptionsAvailable: Flow<Boolean> = shown
        override suspend fun extension(id: String) = detail
        override suspend fun subscriptionSummary() = listOf(
            SubscriptionSummaryLine("telegram", "telegram", subscribable = true),
            SubscriptionSummaryLine("github", "", subscribable = true, on = 1, failing = 1),
            SubscriptionSummaryLine("examplenotes", "example-notes", subscribable = false),
        )
    }
    private val connectors = object : ConnectorsApiStub() {
        override suspend fun connectors(all: Boolean) = listOf(telegram, github)
    }

    private fun capture(dir: String, name: String) {
        rule.waitForIdle()
        val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val out = File("build/subscriptions-entry-shots/$dir").also { it.mkdirs() }
        File(out, "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun everyLook(dir: String, phase: String, set: (AppStyle, Boolean) -> Unit) {
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            set(s, d)
            capture(dir, "$phase-${s.key}-${if (d) "dark" else "light"}")
        }
    }

    @Test fun anExtensionsDetails() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var opened = 0
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                    ExtensionPage(extensions, "telegram", {}, connectors = connectors, onConnector = {}, onSubscriptions = { opened++ })
                }
            }
        }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("连接器 Telegram").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("订阅").assertDoesNotExist()
        everyLook("extension", "before") { s, d -> style = s; dark = d }
        shown.value = true
        rule.waitUntil(5_000) { rule.onAllNodesWithText("可以订阅").fetchSemanticsNodes().isNotEmpty() }
        everyLook("extension", "after") { s, d -> style = s; dark = d }
        rule.onNodeWithText("订阅").performClick()
        rule.waitForIdle()
        assertEquals(1, opened)
    }

    @Test fun aConnectorsPage() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var opened = 0
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                    HubConnectorPage(connectors, "github", onBack = {}, extensions = extensions, onSubscriptions = { opened++ })
                }
            }
        }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("example").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("订阅").assertDoesNotExist()
        everyLook("connector", "before") { s, d -> style = s; dark = d }
        shown.value = true
        rule.waitUntil(5_000) { rule.onAllNodesWithText("1 个开着 · 1 个出错").fetchSemanticsNodes().isNotEmpty() }
        everyLook("connector", "after") { s, d -> style = s; dark = d }
        rule.onNodeWithText("订阅").performClick()
        rule.waitForIdle()
        assertEquals(1, opened)
    }

    @Test fun anotherManagersExtensionHasNoRow() {
        shown.value = true
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = true) {
                ExtensionPage(extensions, "telegram", {}, connectors = connectors, manager = "m-2", onSubscriptions = {})
            }
        }
        rule.waitUntil(5_000) { rule.onAllNodesWithText("Telegram").fetchSemanticsNodes().isNotEmpty() }
        rule.waitForIdle()
        rule.onNodeWithText("订阅").assertDoesNotExist()
    }
}
