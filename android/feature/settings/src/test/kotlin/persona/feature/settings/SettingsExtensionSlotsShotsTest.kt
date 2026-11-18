package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.flow.flowOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.AssistantManagerChoice
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.ExtensionListing
import persona.core.contracts.api.ExtensionStat
import persona.core.contracts.api.ExtensionUiPlace
import persona.core.contracts.api.ExtensionView
import persona.core.contracts.api.ExtensionViewBadge
import persona.core.contracts.api.ExtensionViewItem
import persona.core.contracts.api.ExtensionViewSection
import persona.core.contracts.api.HubExtension
import persona.core.contracts.api.ManagerRoster
import persona.core.designsystem.AppStyle
import persona.core.designsystem.LocalManagerRoster
import persona.core.designsystem.PersonaTheme
import java.io.File

/**
 * #9219: extension slots live on Settings › Extensions, not the Settings home. JVM shots of the home (assistant rows
 * under its heading) and the Extensions page (one overview_card and one nav_page), compact and two-pane, every style,
 * light and dark, English and Chinese. PNGs go to `build/settings-extension-slots-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class SettingsExtensionSlotsShotsTest {
    @get:Rule val rule = createComposeRule()
    @After fun reset() = AssistantManagerChoice.reset()

    private val roster = ManagerRoster(ManagersSample, "assistant")
    private val res get() = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    private val cardView = ExtensionView(sections = listOf(ExtensionViewSection(items = listOf(
        ExtensionViewItem("stat_grid", stats = listOf(ExtensionStat("PRs", "3", "+1", "positive"), ExtensionStat("Reviews", "12"))),
    ))))
    private val slots = ExtensionSlotsState(available = true, places = listOf(
        ExtensionUiPlace("example-github", "today", "Today", "overview_card", extensionName = "GitHub", size = "compact", opens = "github"),
        ExtensionUiPlace("example-github", "github", "GitHub", "nav_page", extensionName = "GitHub", icon = "list",
            badge = ExtensionViewBadge("2", "danger")),
    ))
    private val handle = ExtensionViewHandle({ _, _ -> }, { _, _ -> }, {}, {})
    private val cardSlots = listOf(ExtensionCardSlot(slots.cards.single(), ExtensionViewState(cardView), handle) {})

    private val extensions = object : ExtensionApiStub() {
        override suspend fun listing() = ExtensionListing(listOf(
            HubExtension("example-notes", "Example Notes", "connectors", "running", source = "installed", enabled = true),
        ), canInstall = true)
        override suspend fun extensions() = listing().extensions
    }

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/settings-extension-slots-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    private fun top(text: String) = rule.onAllNodesWithText(text)[0].fetchSemanticsNode().boundsInRoot.top

    @Composable
    private fun homePage(services: SettingsServices) = SettingsHomePage(
        services, "", {}, true, false, null, false, null, false,
        ConnectionState.Online, null, null, null, null, {}, {}, null, null, null, {},
    )

    @Composable
    private fun extensionsPage() = ExtensionsPage(extensions, onBack = {}, onExtension = { _, _ -> }, onMarket = {}, slots = {
        extensionSlotItems(slots, cardSlots, selected = { false }, onOpen = {})
    })

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2400dp-xhdpi")
    @Test fun compactChinese() = compact("zh", heading = "助手的设置", models = "模型")

    @Config(sdk = [35], qualifiers = "en-w400dp-h2400dp-xhdpi")
    @Test fun compactEnglish() = compact("en", heading = "助手's settings", models = "Models")

    @Config(sdk = [35], qualifiers = "zh-w840dp-h1400dp-xhdpi")
    @Test fun twoPaneChinese() = twoPane("zh", heading = "助手的设置", models = "模型")

    @Config(sdk = [35], qualifiers = "en-w840dp-h1400dp-xhdpi")
    @Test fun twoPaneEnglish() = twoPane("en", heading = "助手's settings", models = "Models")

    /** Settings › Extensions draws slots only when a tap can open them (`onNavigate`), same as the old home. */
    @Config(sdk = [35], qualifiers = "en-w400dp-h1400dp-xhdpi")
    @Test fun slotsAreOmittedWhenOnNavigateIsNull() {
        val api = object : ExtensionApiStub() {
            override val slotsAvailable = flowOf(true)
            override suspend fun listing() = ExtensionListing(listOf(
                HubExtension("example-notes", "Example Notes", "connectors", "running", source = "installed", enabled = true),
            ), canInstall = true)
            override suspend fun extensions() = listing().extensions
            override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?) = slots.places
            override suspend fun renderUi(extension: String, ui: String, context: persona.core.contracts.api.ExtensionUiContext?, state: String?) = cardView
        }
        val extensionSlots = ExtensionSlots(api)
        val services = demoSettingsServices(res).copy(demo = false, managers = FakeManagers(), extensions = api, extensionSlots = extensionSlots)
        AssistantManagerChoice.choose("hub", null, "assistant")
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalManagerRoster provides roster) {
                    LaunchedEffect(extensionSlots) { extensionSlots.run(flowOf(true)) }
                    Box(Modifier.fillMaxSize()) { SettingsScreen(services, initialPage = "plugins") }
                }
            }
        }
        rule.waitUntil(5_000) {
            extensionSlots.state.value.navPages.isNotEmpty() &&
                rule.onAllNodesWithText("Example Notes").fetchSemanticsNodes().isNotEmpty()
        }
        rule.waitForIdle()
        assertEquals(0, rule.onAllNodesWithText("GitHub").fetchSemanticsNodes().size)
        assertEquals(0, rule.onAllNodesWithText("Today").fetchSemanticsNodes().size)
    }

    private fun compact(language: String, heading: String, models: String) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var page by mutableStateOf("home")
        val services = demoSettingsServices(res).copy(demo = false, managers = FakeManagers())
        AssistantManagerChoice.choose("hub", null, "assistant")
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalManagerRoster provides roster) {
                    Box(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                        if (page == "home") homePage(services) else extensionsPage()
                    }
                }
            }
        }
        rule.waitUntil(5_000) { rule.onAllNodesWithText(heading).fetchSemanticsNodes().isNotEmpty() }
        assertTrue("$language home: $heading above $models", top(heading) < top(models))
        assertEquals(0, rule.onAllNodesWithText("GitHub").fetchSemanticsNodes().size)
        assertEquals(0, rule.onAllNodesWithText("Today").fetchSemanticsNodes().size)
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d; page = "home"
            capture("home-compact-$language-${s.key}-${if (d) "dark" else "light"}")
            page = "extensions"
            rule.waitUntil(5_000) { rule.onAllNodesWithText("GitHub").fetchSemanticsNodes().isNotEmpty() }
            rule.onNodeWithText("Today").assertExists()
            capture("extensions-compact-$language-${s.key}-${if (d) "dark" else "light"}")
        }
    }

    private fun twoPane(language: String, heading: String, models: String) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        val services = demoSettingsServices(res).copy(demo = false, managers = FakeManagers())
        AssistantManagerChoice.choose("hub", null, "assistant")
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                CompositionLocalProvider(LocalManagerRoster provides roster) {
                    Row(Modifier.testTag("shot").fillMaxSize().background(PersonaTheme.palette.background)) {
                        Box(Modifier.weight(1f).fillMaxHeight()) { homePage(services) }
                        Box(Modifier.weight(1f).fillMaxHeight()) { extensionsPage() }
                    }
                }
            }
        }
        rule.waitUntil(5_000) {
            rule.onAllNodesWithText(heading).fetchSemanticsNodes().isNotEmpty() &&
                rule.onAllNodesWithText("GitHub").fetchSemanticsNodes().isNotEmpty()
        }
        assertTrue("$language two-pane: $heading above $models", top(heading) < top(models))
        rule.onNodeWithText("Today").assertExists()
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            capture("two-pane-$language-${s.key}-${if (d) "dark" else "light"}")
        }
    }
}
