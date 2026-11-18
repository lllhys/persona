package persona.feature.settings

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
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
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.currentCoroutineContext
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.LocalDetailKey
import persona.core.navigation.ModelPurposeRoute
import java.io.File

/** Real model-page disclosure and owner routing; captures are Native JVM evidence, not device screenshots. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h2200dp-xhdpi")
class ModelConfigurationScreenTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    @Before fun start() = AssistantManagerChoice.choose("example-hub", "work", "assistant")
    @After fun reset() = AssistantManagerChoice.reset()
    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/model-configuration-shots").also { it.mkdirs() }

    private val roster = ManagerRoster(listOf(
        HubManager("assistant", "Personal", online = true, isDefault = true),
        HubManager("work", "Work", online = true),
    ), "assistant")
    private val purposes = listOf(
        ModelPurpose("assistant", "Main chat", "Everyday uses", effective = ModelBinding(model = "Example chat model")),
        ModelPurpose("inbox.triage", "Information screening", "Everyday uses", kind = "api", effective = ModelBinding(model = "Example light model")),
        ModelPurpose("lark.agent", "Lark /agent", "Agents", parent = "agent.new",
            effective = ModelBinding(model = "Example agent model"), fields = listOf("provider", "model", "effort")),
    )
    private val reads = mutableListOf<String?>()
    private val models = object : ModelConfiguration {
        override suspend fun catalog(): ModelCatalog {
            reads += currentCoroutineContext()[RequestManager]?.id
            return ModelCatalog(purposes)
        }
        override suspend fun purpose(id: String): ModelPurposeDetail =
            purposes.firstOrNull { it.id == id }?.let { ModelPurposeDetail(it) } ?: throw ModelPurposeNotFound(id)
        override suspend fun save(purpose: ModelPurpose, draft: ModelConfigurationDraft): ModelPurposeSave =
            error("Disclosure must not save model configuration")
        override suspend fun providerModels(provider: String, refresh: Boolean, machine: String?): ProviderModels? = null
    }
    private val providers = object : ProviderSetup {
        override suspend fun presets(): List<ProviderPreset> = error("Disclosure must not start the provider wizard")
        override suspend fun add(provider: NewProvider) = error("Disclosure must not add a provider")
    }
    private fun label(id: Int) = rule.activity.getString(id)
    private fun advanced() = rule.onNodeWithText(label(R.string.settings_pedit_advanced))
    private fun advancedReads(open: Boolean) = advanced().assert(SemanticsMatcher.expectValue(
        SemanticsProperties.StateDescription, if (open) "Expanded" else "Collapsed",
    ))

    @Test fun purposesStayFirstAndAdvancedRemainsReversibleInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var selected: Pair<String, String>? = null
        var providerOpens = 0
        rule.setContent {
            CompositionLocalProvider(LocalManagerRoster provides roster,
                LocalDetailKey provides ModelPurposeRoute("lark.agent", "work")) {
                PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                    ModelConfigurationPage(models, onBack = {}, onPurpose = { id, owner -> selected = id to owner },
                        providers = providers, onProviders = { providerOpens++ })
                }
            }
        }
        for (look in AppStyle.entries) {
            rule.runOnIdle { style = look }
            rule.waitForIdle()
            advancedReads(false)
            rule.onNodeWithText(label(R.string.settings_providers_title)).assertDoesNotExist()
            rule.onNodeWithText(label(R.string.settings_model_add_provider)).assertDoesNotExist()
            rule.onNodeWithText(label(R.string.settings_model_how_runs)).assertDoesNotExist()
            val main = rule.onNodeWithText("Main chat").assertIsDisplayed().fetchSemanticsNode().boundsInRoot
            assertTrue("purposes precede Advanced in $look", main.bottom < advanced().fetchSemanticsNode().boundsInRoot.top)
            rule.onNodeWithText("Lark /agent").assertIsSelected().performClick()
            assertEquals("lark.agent" to "work", selected)
            advanced().performClick()
            advancedReads(true)
            assertTrue(advanced().fetchSemanticsNode().boundsInRoot.height / rule.activity.resources.displayMetrics.density >= 48f)
            rule.onNodeWithText(label(R.string.settings_model_add_provider)).assertHasClickAction().assertIsDisplayed()
            rule.onNodeWithText(label(R.string.settings_model_how_runs_detail)).assertIsDisplayed()
            rule.onNodeWithText(label(R.string.settings_src_model_caption)).assertIsDisplayed()
            rule.onNodeWithText(label(R.string.settings_providers_title)).performClick()
            assertEquals(AppStyle.entries.indexOf(look) + 1, providerOpens)
            advanced().performClick()
            advancedReads(false)
            rule.onNodeWithText("Main chat").assertIsDisplayed()
        }
        assertTrue(reads.isNotEmpty())
        assertTrue("catalog reads retain the current assistant", reads.all { it == "work" })
    }

    @Test fun anOmittedProviderCallbackAddsNoDeadEntry() {
        rule.setContent {
            CompositionLocalProvider(LocalManagerRoster provides roster) {
                PersonaTheme(darkTheme = false, reduceMotion = true) {
                    ModelConfigurationPage(models, onBack = {}, onPurpose = { _, _ -> })
                }
            }
        }
        rule.waitForIdle()
        advanced().performClick()
        advancedReads(true)
        rule.onNodeWithText(label(R.string.settings_providers_title)).assertDoesNotExist()
        rule.onNodeWithText(label(R.string.settings_model_add_provider)).assertDoesNotExist()
        rule.onNodeWithText(label(R.string.settings_model_how_runs_detail)).assertIsDisplayed()
    }

    @Test fun searchKeepsMatchingUsesReachableWithoutOpeningAdvanced() {
        rule.setContent {
            CompositionLocalProvider(LocalManagerRoster provides roster) {
                PersonaTheme(darkTheme = false, reduceMotion = true) {
                    ModelConfigurationPage(models, onBack = {}, onPurpose = { _, _ -> }, onProviders = {})
                }
            }
        }
        rule.waitForIdle()
        rule.onNode(hasSetTextAction()).performTextInput("screening")
        rule.onNodeWithText("Information screening").assertIsDisplayed().assertHasClickAction()
        rule.onNodeWithText("Main chat").assertDoesNotExist()
        advancedReads(false)
    }

    /**
     * #5086 and #5806: the hub's sentence for a purpose is under its name in the list and on its own page; the listed
     * background supervisor is drawn once, after the last use of its group, and is not asked for by id.
     */
    @Test fun aPurposeSaysWhatItIsForInTheListAndOnItsPage() {
        val about = "Follows up the tasks no leader supervises, so the main chat stays free for you."
        val supervise = ModelPurpose(BackgroundSupervisor.PURPOSE, "Background supervisor", "Everyday uses", parent = "assistant",
            description = about, effective = ModelBinding(model = "Example chat model"), inheritedFrom = "assistant",
            fields = listOf("provider", "model", "effort"))
        val described = purposes.map { if (it.id == "assistant") it.copy(description = "Answers you in the Persona conversation.") else it } + supervise
        val asked = mutableListOf<String>()
        // not delegated to `models`: its forManager would hand the page the catalog without the descriptions
        val listing = object : ModelConfiguration {
            override suspend fun catalog() = ModelCatalog(described)
            override suspend fun purpose(id: String): ModelPurposeDetail {
                asked += id
                return described.firstOrNull { it.id == id }?.let { ModelPurposeDetail(it) } ?: throw ModelPurposeNotFound(id)
            }
            override suspend fun save(purpose: ModelPurpose, draft: ModelConfigurationDraft): ModelPurposeSave =
                error("Reading a description must not save model configuration")
            override suspend fun providerModels(provider: String, refresh: Boolean, machine: String?): ProviderModels? = null
        }
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var page by mutableStateOf<String?>(null)
        rule.setContent {
            CompositionLocalProvider(LocalManagerRoster provides roster) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Box(Modifier.testTag("model-shot").width(400.dp).height(1200.dp).background(PersonaTheme.palette.background)) {
                        val open = page
                        if (open == null) ModelConfigurationPage(listing, onBack = {}, onPurpose = { _, _ -> })
                        else ModelPurposePage(listing, open, onBack = {}, manager = "work")
                    }
                }
            }
        }
        // under the reports folder, which CI uploads
        val out = File(System.getProperty("persona.shots") ?: "build/reports/tests/model-described-shots").also { it.mkdirs() }
        for (look in AppStyle.entries) for (night in listOf(false, true)) {
            style = look; dark = night; page = null
            Snapshot.sendApplyNotifications(); rule.waitForIdle()
            rule.onNodeWithText("Answers you in the Persona conversation.", substring = true).assertIsDisplayed()
            rule.onNodeWithText(about, substring = true).assertIsDisplayed()
            // one row, last of its group; a purpose without the sentence reads as before
            rule.onAllNodesWithText("Background supervisor").assertCountEquals(1)
            val screening = rule.onNodeWithText("Information screening").assertIsDisplayed().fetchSemanticsNode().boundsInRoot
            val supervisor = rule.onNodeWithText("Background supervisor").fetchSemanticsNode().boundsInRoot
            val agents = rule.onNodeWithText("Lark /agent").fetchSemanticsNode().boundsInRoot
            assertTrue("the background supervisor is last of its group in $look", screening.bottom <= supervisor.top && supervisor.bottom <= agents.top)
            val variant = "${look.name.lowercase()}-${if (night) "dark" else "light"}"
            File(out, "models-described-$variant.png").outputStream().use {
                assertTrue(rule.onNodeWithTag("model-shot").captureToImage().asAndroidBitmap().compress(Bitmap.CompressFormat.PNG, 100, it))
            }
            assertTrue("a listed background supervisor is not asked for by id", asked.isEmpty())
            page = BackgroundSupervisor.PURPOSE
            Snapshot.sendApplyNotifications(); rule.waitForIdle()
            rule.onNodeWithTag("model-purpose-about").assertIsDisplayed().assertTextEquals(about)
            File(out, "model-purpose-described-$variant.png").outputStream().use {
                assertTrue(rule.onNodeWithTag("model-shot").captureToImage().asAndroidBitmap().compress(Bitmap.CompressFormat.PNG, 100, it))
            }
            assertEquals(listOf(BackgroundSupervisor.PURPOSE), asked.distinct())
            asked.clear()
        }
        // a task's purpose, an extension's and an older hub's have no sentence: no line on the page
        page = "inbox.triage"
        Snapshot.sendApplyNotifications(); rule.waitForIdle()
        rule.onAllNodesWithTag("model-purpose-about").assertCountEquals(0)
    }

    /** Light/dark, default/200% text, closed/open; all captures use reduced motion. */
    @Test fun capturesEveryStyleAppearanceAndTextSize() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var scale by mutableStateOf(1f)
        rule.setContent {
            val density = LocalDensity.current.density
            CompositionLocalProvider(LocalManagerRoster provides roster, LocalDensity provides Density(density, scale)) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    Box(Modifier.testTag("model-shot").width(400.dp).height(2100.dp).background(PersonaTheme.palette.background)) {
                        ModelConfigurationPage(models, onBack = {}, onPurpose = { _, _ -> }, providers = providers, onProviders = {})
                    }
                }
            }
        }
        val out = out()
        val defaultSizes = mutableMapOf<AppStyle, Bitmap>()
        for (look in AppStyle.entries) for (fontScale in listOf(1f, 2f)) {
            var light: Bitmap? = null
            for (night in listOf(false, true)) {
                style = look; dark = night; scale = fontScale
                Snapshot.sendApplyNotifications(); rule.waitForIdle()
                advancedReads(false)
                for (open in listOf(false, true)) {
                    if (open) advanced().performClick()
                    advancedReads(open)
                    val bitmap = rule.onNodeWithTag("model-shot").captureToImage().asAndroidBitmap()
                    val name = "models-${look.name.lowercase()}-${if (night) "dark" else "light"}-${if (fontScale == 1f) "default" else "large"}-${if (open) "open" else "closed"}-reduced.png"
                    File(out, name).outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
                    if (!open) {
                        if (night) assertFalse("light and dark images must differ", bitmap.sameAs(light!!))
                        else {
                            light = bitmap
                            if (fontScale == 1f) defaultSizes[look] = bitmap
                            else assertFalse("default and large text images must differ", bitmap.sameAs(defaultSizes.getValue(look)))
                        }
                    } else rule.onNodeWithText(label(R.string.settings_model_how_runs_detail)).assertIsDisplayed()
                }
                advanced().performClick()
            }
        }
    }
}
