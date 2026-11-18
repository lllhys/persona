package persona.feature.settings

import android.content.Context
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.GitHubAuth
import persona.core.contracts.api.GitHubMachine
import persona.core.contracts.api.GitHubMissing
import persona.core.contracts.api.HubConnector
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray

/**
 * Settings › Connectors › GitHub in Glass, Brief and Board, light and dark, English and Simplified Chinese.
 * The page is a lazy list, so the shot is tall enough to show the sign-in source and the Inbox section together.
 * Written under `build/github-connector-shots/` (or `-Dpersona.shots`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class GitHubConnectorShotsTest {
    @get:Rule val rule = createComposeRule()

    private enum class Mode { Device, Computer }

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/github-connector-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "en-w400dp-h2800dp-xhdpi")
    @Test fun english() = shots("en")

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2800dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh")

    private fun shots(language: String) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var mode by mutableStateOf(Mode.Device)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                key(mode) {
                    Box(Modifier.testTag("shot").width(400.dp).height(2600.dp).background(PersonaTheme.palette.background)) {
                        HubConnectorPage(if (mode == Mode.Device) DeviceApi else ComputerApi, "github", onBack = {})
                    }
                }
            }
        }
        val res = ApplicationProvider.getApplicationContext<Context>().resources
        val missing = res.getString(R.string.settings_gh_missing_header)
        val signIn = res.getString(R.string.settings_src_connector_sign_in, "GitHub")
        val deviceFooter = res.getString(R.string.settings_gh_device_footer)
        val inbox = res.getString(R.string.settings_gh_inbox)
        for (s in AppStyle.entries) for (d in listOf(false, true)) for (next in Mode.entries) {
            style = s
            dark = d
            mode = next
            val marker = if (next == Mode.Device) signIn else missing
            rule.waitUntil(timeoutMillis = 5_000) { rule.onAllNodesWithText(marker).fetchSemanticsNodes().isNotEmpty() }
            rule.waitForIdle()
            if (next == Mode.Device) {
                rule.onNodeWithText(missing).assertDoesNotExist()
                rule.onNodeWithTag("github.missingScopes").assertDoesNotExist()
                rule.onNodeWithText(deviceFooter).assertExists()
                rule.onNodeWithTag("github.source.device").assertIsSelected()
                rule.onNodeWithTag("github.source.laptop").assertIsNotSelected()
            } else {
                rule.onNodeWithText(signIn).assertDoesNotExist()
                rule.onNodeWithText(deviceFooter).assertDoesNotExist()
                rule.onNodeWithTag("github.missingScopes").assertExists()
                rule.onNodeWithText("gh auth refresh", substring = true).assertExists()
                rule.onNodeWithTag("github.source.studio").assertIsSelected()
                rule.onNodeWithTag("github.source.device").assertIsNotSelected()
            }
            rule.onNodeWithText(inbox).assertExists()
            rule.onNodeWithText("acme/app").assertExists()
            if (language == "en") assertNoChinese(next.name)
            capture("github-${next.name.lowercase()}-$language-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }

    private fun assertNoChinese(page: String) {
        val cjk = Regex("[　-〿぀-ヿ㐀-䶿一-鿿＀-￯]")
        val texts = mutableListOf<String>()
        fun walk(node: androidx.compose.ui.semantics.SemanticsNode) {
            node.config.getOrNull(SemanticsProperties.Text)?.forEach { texts += it.text }
            node.config.getOrNull(SemanticsProperties.EditableText)?.let { texts += it.text }
            node.config.getOrNull(SemanticsProperties.ContentDescription)?.let { texts += it }
            node.children.forEach(::walk)
        }
        walk(rule.onRoot(useUnmergedTree = true).fetchSemanticsNode())
        val chrome = setOf("返回")
        val chinese = texts.filter { cjk.containsMatchIn(it) && it !in chrome }
        assertTrue("$page shows Chinese in English: $chinese", chinese.isEmpty())
    }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private object DeviceApi : ConnectorsApiStub() {
        override suspend fun connectors(all: Boolean) = listOf(githubShotConnector.copy(status = "not_configured"))
        override suspend fun gitHubAuth() = GitHubAuth(
            source = "device",
            machines = listOf(
                GitHubMachine(id = "studio", name = "Studio", state = "not_signed_in"),
                GitHubMachine(id = "laptop", name = "Laptop", state = "offline"),
            ),
        )
    }

    private object ComputerApi : ConnectorsApiStub() {
        private val missing = listOf(GitHubMissing(scope = "notifications", affects = listOf("CI status on pull requests")))
        override suspend fun connectors(all: Boolean) = listOf(githubShotConnector.copy(status = "ok", account = "octo"))
        override suspend fun gitHubAuth() = GitHubAuth(
            source = "machine",
            machine = "studio",
            machines = listOf(
                GitHubMachine(id = "studio", name = "Studio", state = "signed_in", account = "octo", missing = missing),
                GitHubMachine(id = "laptop", name = "Laptop", state = "offline"),
            ),
            missing = missing,
        )
    }
}

private val githubShotOptions = buildJsonObject {
    put("ci", "mine")
    putJsonArray("muted_repos") {
        add("acme/app")
        add("acme/web")
    }
    putJsonArray("ignore_branches") { add("train/*") }
}

private val githubShotConnector = HubConnector(
    "github", "GitHub", enabled = true, archive = true, inbox = true,
    capabilities = listOf("login", "archive", "inbox"), options = githubShotOptions,
)
