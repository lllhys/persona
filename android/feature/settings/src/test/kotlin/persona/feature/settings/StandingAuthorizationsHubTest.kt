package persona.feature.settings

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasScrollToNodeAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.StandingAuthorizationList
import persona.core.contracts.api.StandingAuthorizationsSource
import persona.core.designsystem.PersonaTheme
import persona.core.hub.HubError
import persona.core.hub.HubFailure

/**
 * The page keys its list on the hub, and a revoke that was already confirmed stays on the hub that listed the row.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class StandingAuthorizationsHubTest {
    @get:Rule val rule = createComposeRule()

    @Test fun switchingHubsDropsTheOldListAndRevokeStaysOnTheHubThatListedTheRow() {
        val identity = MutableStateFlow("hub-a")
        val calls = mutableListOf<String>()
        val gate = CompletableDeferred<Unit>()
        val source = object : StandingAuthorizationsSource {
            override val hubIdentity: StateFlow<String> = identity
            override fun pinned(): StandingAuthorizationsSource {
                val hub = identity.value
                return object : StandingAuthorizationsSource {
                    override suspend fun list(): StandingAuthorizationList {
                        calls += "$hub list"
                        return StandingAuthorizationList(1, listOf(
                            persona.core.contracts.StandingAuthorization(id = hub, machineId = "mac", directory = "/code/weather", actions = listOf("merge")),
                        ))
                    }
                    override suspend fun revoke(id: String) {
                        calls += "$hub revoke $id"
                        gate.await()
                    }
                }
            }
            override suspend fun list() = error("the page must pin the hub")
            override suspend fun revoke(id: String) = error("the page must pin the hub")
        }
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Box(Modifier.width(400.dp).height(1400.dp)) {
                    StandingAuthorizationsPage(source, emptyList(), onBack = {})
                }
            }
        }
        rule.waitUntil(5_000) { rule.onAllNodes(hasTestTag("standing-row-hub-a")).fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithTag("standing-row-hub-a").performTouchInput { longClick() }
        rule.onNodeWithText("Revoke Authorization…").assertIsDisplayed().performClick()
        rule.onNodeWithText("Revoke this authorization?").assertIsDisplayed()
        rule.onNodeWithText("Cancel").performClick()
        rule.waitUntil(5_000) { rule.onAllNodes(hasText("Revoke this authorization?")).fetchSemanticsNodes().isEmpty() }
        rule.onNodeWithTag("standing-row-hub-a").performClick()
        rule.waitUntil(5_000) { rule.onAllNodes(hasTestTag("standing-detail")).fetchSemanticsNodes().isNotEmpty() }
        rule.onNode(hasScrollToNodeAction().and(hasAnyAncestor(hasTestTag("standing-detail")))).performScrollToNode(hasTestTag("standing-revoke"))
        rule.onNodeWithTag("standing-revoke").performClick()
        rule.onNodeWithText("Revoke Authorization").performClick()
        rule.waitUntil(5_000) { calls.any { it == "hub-a revoke hub-a" } }
        rule.runOnIdle { identity.value = "hub-b" }
        rule.waitUntil(5_000) { rule.onAllNodes(androidx.compose.ui.test.hasTestTag("standing-row-hub-b")).fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithTag("standing-row-hub-a").assertDoesNotExist()
        rule.runOnIdle {
            assertEquals(listOf("hub-a list", "hub-a revoke hub-a", "hub-b list"), calls.toList())
            gate.complete(Unit)
        }
    }

    @Test fun clearingTheHubShowsTheLoadError() {
        val identity = MutableStateFlow("hub-a")
        val source = object : StandingAuthorizationsSource {
            override val hubIdentity: StateFlow<String> = identity
            override fun pinned(): StandingAuthorizationsSource {
                val hub = identity.value
                return object : StandingAuthorizationsSource {
                    override val hubIdentity: StateFlow<String> = MutableStateFlow(hub)
                    override fun pinned(): StandingAuthorizationsSource = this
                    override suspend fun list(): StandingAuthorizationList {
                        if (hub.isEmpty()) throw HubFailure(HubError.NoNetwork)
                        return StandingAuthorizationList(1, listOf(
                            persona.core.contracts.StandingAuthorization(id = hub, machineId = "mac", directory = "/code/weather", actions = listOf("merge")),
                        ))
                    }
                    override suspend fun revoke(id: String) = error("unused")
                }
            }
            override suspend fun list() = error("the page must pin the hub")
            override suspend fun revoke(id: String) = error("the page must pin the hub")
        }
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Box(Modifier.width(400.dp).height(800.dp)) {
                    StandingAuthorizationsPage(source, emptyList(), onBack = {})
                }
            }
        }
        rule.waitUntil(5_000) { rule.onAllNodes(hasTestTag("standing-row-hub-a")).fetchSemanticsNodes().isNotEmpty() }
        rule.runOnIdle { identity.value = "" }
        rule.waitUntil(5_000) { rule.onAllNodes(hasTestTag("standing-error")).fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithTag("standing-row-hub-a").assertDoesNotExist()
        rule.onNodeWithText("Couldn't load standing authorizations").assertIsDisplayed()
    }
}
