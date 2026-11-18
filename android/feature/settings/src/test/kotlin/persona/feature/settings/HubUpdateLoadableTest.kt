package persona.feature.settings

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.hub.HubError
import persona.core.hub.HubFailure

/**
 * Settings › Hub version through the loadable container: the first read, a failed read with Retry, a retry, a stale status with
 * the failure over it, and a check that keeps what is shown; in every style, the three languages and at a large font size.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp")
class HubUpdateLoadableTest {
    @get:Rule val rule = createComposeRule()

    private val status = HubUpdateStatus(version = "1.42.0", build = 420, channel = "release", canUpdate = true, state = "idle")

    /** Answers [status] after [failures] failed reads. */
    private class Hub(var status: HubUpdateStatus, var failures: Int) : HubUpdates {
        var reads = 0
        override suspend fun status(): HubUpdateStatus { reads++; if (failures > 0) { failures--; throw HubFailure(HubError.Server, "db down", 500) }; return status }
        override suspend fun check() = status
        override suspend fun start(now: Boolean) = status
        override suspend fun settings(auto: Boolean?, noticeSeen: Boolean?) = status
        override suspend fun history() = emptyList<HubUpdateRecord>()
    }

    private fun show(state: HubUpdateState, model: HubUpdateModel? = HubUpdateModel(Hub(status, failures = 0)), style: AppStyle = AppStyle.Glass) = rule.setContent {
        PersonaTheme(darkTheme = false, reduceMotion = true, style = style) { HubUpdateContent(state, emptyList(), {}, null, model) }
    }

    private fun failed(text: String) = SettingsUiText.Raw(text)

    @Test fun theFirstReadIsASpinner() {
        show(HubUpdateState(loading = true))
        rule.onNode(hasContentDescription("Loading")).assertIsDisplayed()
    }

    @Test fun aFailedFirstReadSaysWhyWithRetry() {
        show(HubUpdateState(error = failed("Couldn't read the hub's version: db down")))
        rule.onNodeWithText("Couldn't read the hub's version: db down").assertIsDisplayed()
        rule.onNodeWithText("Retry").assertIsDisplayed()
    }

    @Test fun retryReadsAgainAndThePageShowsTheStatus() {
        val hub = Hub(status, failures = 1)
        val model = HubUpdateModel(hub)
        runBlocking { model.load() }
        var state by mutableStateOf(model.state.value)
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) { HubUpdateContent(state, emptyList(), {}, null, model) }
        }
        rule.onNodeWithText("Retry").performClick()
        rule.waitUntil(5_000) { model.state.value.status != null }
        state = model.state.value
        rule.waitForIdle()
        rule.onNodeWithText("1.42.0").assertIsDisplayed()
        assertEquals(2, hub.reads)
    }

    @Test fun aStaleStatusKeepsItsRowsWithTheFailureOverThem() {
        show(HubUpdateState(status = status, error = failed("Couldn't read the hub's version: db down")))
        rule.onNodeWithText("Couldn't read the hub's version: db down").assertIsDisplayed()
        rule.onNodeWithText("1.42.0").assertIsDisplayed()
        rule.onNodeWithText("Retry").assertIsDisplayed()
    }

    @Test fun aCheckUnderWayKeepsTheRows() {
        show(HubUpdateState(status = status, checking = true))
        rule.onNodeWithText("1.42.0").assertIsDisplayed()
    }

    @Test fun aHubFromBeforeUpdatesIsNotAFailure() {
        show(HubUpdateState(unsupported = true))
        rule.onNodeWithText("Retry").assertDoesNotExist()
    }

    @Test fun everyStyleDrawsFailureAndStaleStatus() {
        var style by mutableStateOf(AppStyle.Glass)
        var state by mutableStateOf(HubUpdateState(error = failed("down")))
        rule.setContent { PersonaTheme(darkTheme = false, reduceMotion = true, style = style) { HubUpdateContent(state, emptyList(), {}, null, null) } }
        for (next in AppStyle.entries) {
            style = next
            for (each in listOf(HubUpdateState(loading = true), HubUpdateState(error = failed("down")), HubUpdateState(status = status, error = failed("down")), HubUpdateState(status = status))) {
                state = each
                rule.waitForIdle()
            }
        }
        rule.onNodeWithText("1.42.0").assertIsDisplayed()
    }

    @Config(qualifiers = "zh-w400dp-h900dp")
    @Test fun failureAndRetryInSimplifiedChinese() {
        show(HubUpdateState(error = failed("读不到 Hub 的版本：数据库没连上")))
        rule.onNodeWithText("读不到 Hub 的版本：数据库没连上").assertIsDisplayed()
        rule.onNodeWithText("重试").assertIsDisplayed()
    }

    @Config(qualifiers = "ja-w400dp-h900dp")
    @Test fun failureAndRetryInJapanese() {
        show(HubUpdateState(error = failed("ハブのバージョンを読み取れません：DB に接続できません")))
        rule.onNodeWithText("再試行").assertIsDisplayed()
    }

    @Test fun failureAndRetryAtTheLargestFontSize() {
        RuntimeEnvironment.setFontScale(2.0f)
        show(HubUpdateState(error = failed("Couldn't read the hub's version: the database did not answer in time")))
        rule.onNodeWithText("Retry").assertIsDisplayed()
        rule.onNode(hasText("Retry")).performClick()
    }

}
