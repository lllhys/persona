package persona.feature.settings

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
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
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.*
import persona.core.designsystem.*

/**
 * A hub behind Cloudflare Access (#3502): the service token fields on the pairing confirm page, the row in Settings ›
 * Hubs and its page, in every style, light and dark (English), plus Chinese and Japanese; PNGs in `build/access-shots/`.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class HubAccessShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val token = HubAccess("0123abcd.access", "secret-value")
    private val home = HubTunnelInfo("h1", "Home server", "https://hub.example.com/", online = true, access = token)
    private val office = HubTunnelInfo("h3", "Office", "https://office.example.com/")
    private val groups = object : HubGroups {
        override val groups = MutableStateFlow(listOf(
            HubGroupInfo("home", "Home", null, selected = true, state = ConnectionState.Online, hubs = listOf(
                HubMemberInfo("h1", "Home server", paired = true, connected = true, online = true))),
            HubGroupInfo("office", "Office", "o", selected = false, state = ConnectionState.Connecting, hubs = listOf(
                HubMemberInfo("h3", "Office", paired = true, online = true))),
        ))
        override suspend fun select(groupId: String) = Unit
        override suspend fun rename(groupId: String, name: String) = Unit
        override suspend fun removeGroup(groupId: String) = Unit
        override suspend fun removeHub(groupId: String, hubId: String) = Unit
    }
    private class FakePairing(private val error: Exception? = null) : PairingService {
        override val isPaired = MutableStateFlow(false)
        val requests = mutableListOf<PairingRequest>()
        override suspend fun pair(request: PairingRequest) { requests += request; error?.let { throw it } }
        override suspend fun unpair() = Unit
    }
    private val sessions = object : SessionsRepository {
        override val machines = MutableStateFlow(emptyList<Machine>())
        override val sessions = MutableStateFlow(emptyList<Session>())
        override suspend fun refresh() = Unit
    }
    private val link = "persona://pair?url=https%3A%2F%2Fhub.example.com&token=mock"

    private enum class Shot { Pairing, Hubs, Access }

    @Config(sdk = [35], qualifiers = "en-w400dp-h2400dp-xhdpi")
    @Test fun english() = shots("en", AppStyle.entries.flatMap { listOf(it to false, it to true) })

    @Config(sdk = [35], qualifiers = "zh-w400dp-h2400dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", listOf(AppStyle.Glass to false))

    @Config(sdk = [35], qualifiers = "ja-w400dp-h2400dp-xhdpi")
    @Test fun japanese() = shots("ja", listOf(AppStyle.Glass to false))

    private fun shots(language: String, variants: List<Pair<AppStyle, Boolean>>) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var shot by mutableStateOf(Shot.Pairing)
        val tunnels = FakeHubTunnels(home, office)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(2400.dp).background(PersonaTheme.palette.background)) {
                    key(shot, style, dark) {
                        when (shot) {
                            Shot.Pairing -> PairingScreen(FakePairing(), sessions, initialLink = link, onReady = {}, onDiagnostics = {}, skipWelcome = true)
                            Shot.Hubs -> HubsScreen(groups, onAdd = {}, onBack = {}, tunnels = tunnels)
                            Shot.Access -> HubAccessPage(tunnels, "h1", onBack = {})
                        }
                    }
                }
            }
        }
        for ((s, d) in variants) for (next in Shot.entries) {
            style = s; dark = d; shot = next
            rule.waitForIdle()
            when (next) {
                Shot.Pairing -> {
                    tap(R.string.settings_access_title)
                    rule.onAllNodes(hasSetTextAction())[0].performTextReplacement(token.clientId)
                    rule.onAllNodes(hasSetTextAction())[1].performTextReplacement(token.clientSecret)
                    rule.waitForIdle()
                    // Masked: what is drawn is dots, never the secret's characters.
                    rule.onAllNodes(hasSetTextAction())[1].assert(SemanticsMatcher("masked") {
                        val drawn = it.config.getOrElseNullable(androidx.compose.ui.semantics.SemanticsProperties.EditableText) { null }?.text
                        drawn != null && drawn.length == token.clientSecret.length && drawn.all { c: Char -> c == '•' }
                    })
                }
                Shot.Hubs -> {
                    // One row per paired hub; a group of one hub names none.
                    rule.onAllNodesWithText(text(R.string.settings_access_title)).assertCountEquals(2)
                    rule.onAllNodesWithText(text(R.string.settings_access_on)).assertCountEquals(1)
                }
                Shot.Access -> rule.onNodeWithText(text(R.string.settings_access_clear)).assertExists()
            }
            capture("access-$language-${next.name.lowercase()}-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }

    /** The token typed before pairing goes with the pairing request; Access refusing it leads back to the fields. */
    @Config(sdk = [35], qualifiers = "en-w400dp-h2400dp-xhdpi")
    @Test fun theTokenGoesWithThePairingAndARefusalLeadsBackToIt() {
        val pairing = FakePairing(PairingException(PairingFailure.AccessRejected))
        rule.setContent { PersonaTheme(reduceMotion = true) { PairingScreen(pairing, sessions, initialLink = link, onReady = {}, onDiagnostics = {}, skipWelcome = true) } }
        rule.waitForIdle()
        tap(R.string.settings_access_title)
        rule.onAllNodes(hasSetTextAction())[0].performTextReplacement(" ${token.clientId} ")
        rule.onAllNodes(hasSetTextAction())[1].performTextReplacement(token.clientSecret)
        tap(R.string.settings_pairing_confirm)
        rule.waitUntil(5_000) { rule.onAllNodesWithText(text(R.string.settings_pairing_failure_access_rejected)).fetchSemanticsNodes().isNotEmpty() }
        assertEquals(token, pairing.requests.single().access)
        tap(R.string.settings_pairing_recovery_access)
        // The confirm page again, with the fields open and kept.
        rule.onAllNodes(hasSetTextAction()).assertCountEquals(2)
        rule.onAllNodes(hasSetTextAction())[0].assert(hasText(" ${token.clientId} "))
    }

    /** A token typed for one hub never goes to another: a new link starts empty, and a closed section sends nothing. */
    @Config(sdk = [35], qualifiers = "en-w400dp-h2400dp-xhdpi")
    @Test fun theTokenStaysWithTheHubItWasTypedFor() {
        val pairing = FakePairing(PairingException(PairingFailure.Timeout))
        rule.setContent { PersonaTheme(reduceMotion = true) { PairingScreen(pairing, sessions, initialLink = link, onReady = {}, onDiagnostics = {}, skipWelcome = true) } }
        rule.waitForIdle()
        tap(R.string.settings_access_title)
        rule.onAllNodes(hasSetTextAction())[0].performTextReplacement(token.clientId)
        rule.onAllNodes(hasSetTextAction())[1].performTextReplacement(token.clientSecret)
        // Another hub's link pasted into the form.
        tap(R.string.settings_pairing_edit_input)
        rule.onAllNodes(hasSetTextAction())[0].performTextReplacement("persona://pair?url=https%3A%2F%2Fother.example.com&token=mock")
        rule.waitForIdle()
        rule.onAllNodes(hasSetTextAction()).assertCountEquals(0)
        tap(R.string.settings_access_title)
        rule.onAllNodes(hasSetTextAction())[0].assert(hasText(""))
        rule.onAllNodes(hasSetTextAction())[0].performTextReplacement(token.clientId)
        rule.onAllNodes(hasSetTextAction())[1].performTextReplacement(token.clientSecret)
        // Typed, then folded away: what can't be seen isn't sent.
        tap(R.string.settings_access_title)
        tap(R.string.settings_pairing_confirm)
        rule.waitUntil(5_000) { pairing.requests.isNotEmpty() }
        assertEquals("https://other.example.com", pairing.requests.single().url.trimEnd('/'))
        assertNull(pairing.requests.single().access)
    }

    /** Settings saves a changed token and removes it. */
    @Config(sdk = [35], qualifiers = "en-w400dp-h2400dp-xhdpi")
    @Test fun settingsSavesAndRemovesTheToken() {
        val tunnels = FakeHubTunnels(home)
        rule.setContent { PersonaTheme(reduceMotion = true) { HubAccessPage(tunnels, "h1", onBack = {}) } }
        rule.waitForIdle()
        rule.onNode(hasText(text(R.string.settings_access_save)) and hasClickAction()).assertIsNotEnabled()
        rule.onAllNodes(hasSetTextAction())[1].performTextReplacement("rotated-secret")
        tap(R.string.settings_access_save)
        assertEquals(HubAccess(token.clientId, "rotated-secret"), tunnels.hubs.value.single().access)
        tap(R.string.settings_access_clear)
        assertNull(tunnels.hubs.value.single().access)
        assertEquals(listOf("saveAccess ${token.clientId}", "saveAccess null"), tunnels.calls)
    }

    private fun text(id: Int, vararg args: Any) = rule.activity.getString(id, *args)
    private fun tap(id: Int) { rule.onNode(hasText(text(id)) and hasClickAction()).performClick(); rule.waitForIdle() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(File("build/access-shots").also { it.mkdirs() }, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}
