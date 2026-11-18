package persona.feature.settings

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.core.app.RemoteInput
import androidx.lifecycle.Lifecycle
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.EXTENSION_NOTIFICATION_REMOTE_INPUT
import persona.core.contracts.api.ExtensionNotificationActionStore
import persona.core.contracts.api.ExtensionNotificationScope
import persona.core.contracts.api.ExtensionUiContext
import persona.core.contracts.api.ExtensionUiResult
import persona.core.contracts.api.ExtensionView
import persona.core.contracts.api.ExtensionViewAction
import persona.core.contracts.api.ExtensionViewPush
import persona.core.contracts.generated.ExtensionNotifyAction
import persona.core.contracts.generated.ExtensionNotifyActionInput
import persona.core.contracts.generated.ExtensionUIContext
import persona.core.contracts.generated.ExtensionUIStreamCapability
import persona.core.contracts.generated.ExtensionUIStreamOpenRequest
import persona.core.contracts.generated.ExtensionUIStreamRequest
import persona.core.contracts.stream.NativeStreamIdentity
import persona.core.contracts.stream.NativeStreamSession
import persona.core.contracts.stream.NativeStreamTransport
import persona.core.designsystem.JNotice
import persona.core.designsystem.PersonaTheme

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class ExtensionNotificationPageTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val scope = ExtensionNotificationScope("hub/generation", null, "assistant")
    private var current = scope
    private var time = 1_000L
    private var tokens = 0
    private val store = ExtensionNotificationActionStore(now = { time }, currentScope = { current },
        token = { "%032x".format(++tokens) })

    @Test fun foregroundTokenClaimsItsActionAndInputOnlyOnce() = runBlocking {
        val record = store.register("alert", "example-gateway", "usage", "state",
            ExtensionNotifyAction("note", "Reply", foreground = true, input = ExtensionNotifyActionInput("Text")), scope)!!
        val first = resolve(record, "hello")
        assertEquals("hello", first.execution!!.values["text"])
        assertTrue(first.requiresHttp)
        val second = resolve(record, "again")
        assertNull(second.execution)
        assertNull(second.error)
        assertTrue(second.requiresHttp)
    }

    @Test fun wrongScopeAndExpiryShowErrorsWithoutExecuting() = runBlocking {
        val record = store.register("alert", "example-gateway", "usage", "state",
            ExtensionNotifyAction("stop", "Stop", foreground = true), scope)!!
        current = scope.copy(namespace = "other")
        assertEquals(ExtensionNotificationPageError.WrongScope, resolve(record, null).error)
        current = scope
        time += 900_001
        assertEquals(ExtensionNotificationPageError.Expired, resolve(record, null).error)
        val mismatched = store.register("alert", "example-gateway", "usage", "state",
            ExtensionNotifyAction("stop", "Stop"), scope)!!
        assertEquals(ExtensionNotificationPageError.Invalid,
            resolveExtensionNotificationPage(mismatched.token, "example-gateway", "usage", "other", null, store).error)
    }

    @Test fun missingAndBodyTokensLeaveTheOrdinaryPageAlone() = runBlocking {
        val missing = resolveExtensionNotificationPage("0".repeat(32), "example-gateway", "usage", null, null, store)
        assertNull(missing.record)
        assertNull(missing.execution)
        assertNull(missing.error)
        val body = store.register("alert", "example-gateway", "usage", "state", null, scope)!!
        val ordinary = resolve(body, null)
        assertNull(ordinary.execution)
        assertNull(ordinary.error)
        assertTrue(!ordinary.requiresHttp)
    }

    @Test fun lockedForegroundResolutionFailsBeforeClaimAndCanContinueAfterUnlock() = runBlocking {
        val record = store.register("alert", "example-gateway", "usage", "state",
            ExtensionNotifyAction("stop", "Stop", foreground = true), scope)!!
        val locked = resolveExtensionNotificationPage(record.token, record.extension, record.ui!!,
            record.scope.manager, null, store) { true }
        assertEquals(ExtensionNotificationPageError.Locked, locked.error)
        assertNull(locked.execution)

        val unlocked = resolve(record, null)
        assertTrue(unlocked.execution != null)
        assertNull(resolve(record, null).execution)
    }

    @Test fun foregroundPressDoesNotLoadBeforeRunningAndUsesTheReturnedView() = runBlocking {
        val record = store.register("alert", "example-gateway", "usage", "state",
            ExtensionNotifyAction("note", "Reply", foreground = true, input = ExtensionNotifyActionInput()), scope)!!
        val execution = resolve(record, "hello").execution!!
        val api = PageUi(ExtensionUiResult(view = ExtensionView(title = "After", state = "next"), toast = "Done"))
        val model = ExtensionViewModel(api, record.extension, record.ui!!, initialState = record.state)
        val outcome = model.press(ExtensionViewAction(execution.action.id), execution.values)
        assertEquals(0, api.renders)
        assertEquals(listOf(Triple("note", "state", mapOf("text" to "hello"))), api.actions)
        assertEquals("After", model.state.value.view!!.title)
        assertEquals("Done", outcome.toast)
    }

    @Test fun foregroundOutcomesKeepTheExistingToastLinkAndNavigationResults() = runBlocking {
        for (result in listOf(ExtensionUiResult(toast = "Done"), ExtensionUiResult(openUrl = "https://example.com"),
            ExtensionUiResult(push = ExtensionViewPush(ui = "next")), ExtensionUiResult(pop = true))) {
            val record = store.register("alert:${result.hashCode()}", "example-gateway", "usage", "state",
                ExtensionNotifyAction("stop", "Stop", foreground = true), scope)!!
            val execution = resolve(record, null).execution!!
            val api = PageUi(result)
            val model = ExtensionViewModel(api, record.extension, record.ui!!, initialState = record.state)
            val outcome = model.press(ExtensionViewAction(execution.action.id), execution.values)
            assertEquals(result.toast, outcome.toast)
            assertEquals(result.openUrl, outcome.openUrl)
            assertEquals(result.push, outcome.push)
            assertEquals(result.pop, outcome.pop)
            assertEquals(0, api.renders)
        }
    }

    @Test fun unlockSettleAcceptsAnUnlockThatLandsShortlyAfterTheIntent() = runTest {
        var reads = 0
        assertFalse(extensionNotificationLockedAfterUnlockSettles({ ++reads <= 3 }, settleMillis = 100, pollMillis = 10))
        assertTrue(extensionNotificationLockedAfterUnlockSettles({ true }, settleMillis = 20, pollMillis = 1))
    }

    @Test fun composeRunsOnceAcrossRecompositionBackAndActivityRecreation() {
        val record = store.register("alert", "example-gateway", "usage", "state",
            ExtensionNotifyAction("stop", "Stop", foreground = true), scope)!!
        val api = PageUi(ExtensionUiResult(view = ExtensionView(title = "After")))
        var tick by mutableIntStateOf(0)
        var open by mutableStateOf(true)
        fun content() = rule.setContent {
            PersonaTheme(reduceMotion = true) {
                val currentTick = tick
                if (open) {
                    ExtensionUiPage(api, record.extension, record.ui!!, "Page", onBack = { open = false },
                        shown = currentTick >= 0, notificationActionToken = record.token,
                        notificationStore = store, notificationLocked = { false })
                } else {
                    JNotice("Returned")
                }
            }
        }
        content()
        rule.waitForIdle()
        assertEquals(1, api.actions.size)
        assertEquals(0, api.renders)

        rule.runOnIdle { tick++ }
        rule.waitForIdle()
        assertEquals(1, api.actions.size)
        rule.onNodeWithContentDescription("Back").performClick()
        rule.waitForIdle()
        assertFalse(open)
        rule.runOnIdle { open = true }
        rule.waitForIdle()
        assertEquals(1, api.actions.size)

        rule.activityRule.scenario.recreate()
        content()
        rule.waitForIdle()
        assertEquals(1, api.actions.size)
    }

    @Test fun composeReadsRemoteInputAndDoesNotReplayItAfterRecreation() {
        rule.mainClock.autoAdvance = false
        val record = store.register("alert", "example-gateway", "usage", "state",
            ExtensionNotifyAction("note", "Reply", foreground = true, input = ExtensionNotifyActionInput("Text")), scope)!!
        val api = PageUi(ExtensionUiResult(view = ExtensionView(title = "After")))
        val intent = Intent(rule.activity, ComponentActivity::class.java)
        RemoteInput.addResultsToIntent(arrayOf(RemoteInput.Builder(EXTENSION_NOTIFICATION_REMOTE_INPUT).build()), intent,
            Bundle().apply { putCharSequence(EXTENSION_NOTIFICATION_REMOTE_INPUT, "hello") })
        assertEquals("hello", RemoteInput.getResultsFromIntent(intent)?.getCharSequence(EXTENSION_NOTIFICATION_REMOTE_INPUT)?.toString())
        showPage(api, record.token, notificationIntent = intent)
        rule.mainClock.advanceTimeBy(100)
        assertEquals(1, api.actions.size)
        assertEquals(Triple("note", "state", mapOf("text" to "hello")), api.actions.single())

        rule.activityRule.scenario.recreate()
        showPage(api, record.token, notificationIntent = intent)
        rule.mainClock.advanceTimeBy(100)
        assertEquals(1, api.actions.size)
    }

    @Config(sdk = [30])
    @Test fun composeChecksLockBeforeClaimOnApi30() = composeChecksLockBeforeClaim()

    @Config(sdk = [35])
    @Test fun composeChecksLockBeforeClaimOnApi35() = composeChecksLockBeforeClaim()

    private fun composeChecksLockBeforeClaim() {
        val record = store.register("alert", "example-gateway", "usage", "state",
            ExtensionNotifyAction("stop", "Stop", foreground = true), scope)!!
        val api = PageUi(ExtensionUiResult(view = ExtensionView(title = "After")))
        var locked = true
        showPage(api, record.token, locked = { locked })
        rule.waitForIdle()
        assertTrue(api.actions.isEmpty())
        rule.onNodeWithText(rule.activity.getString(R.string.settings_ext_notification_unlock)).assertExists()

        locked = false
        rule.activityRule.scenario.moveToState(Lifecycle.State.CREATED)
        rule.activityRule.scenario.moveToState(Lifecycle.State.RESUMED)
        rule.waitForIdle()
        assertEquals(1, api.actions.size)
        rule.activityRule.scenario.moveToState(Lifecycle.State.CREATED)
        rule.activityRule.scenario.moveToState(Lifecycle.State.RESUMED)
        rule.waitForIdle()
        assertEquals(1, api.actions.size)
    }

    @Test fun composeForcesHttpForActionsButKeepsNativeForAnOrdinaryOrMissingTokenPage() {
        val requests = mutableListOf<ExtensionUIStreamRequest>()
        val native = nativeFixture(requests)
        val record = store.register("alert", "example-gateway", "usage", "state",
            ExtensionNotifyAction("stop", "Stop", foreground = true), scope)!!
        val api = PageUi(ExtensionUiResult(view = ExtensionView(title = "HTTP action")))
        showPage(api, record.token, availableAdapter = native.adapter)
        rule.waitForIdle()
        assertEquals(1, api.actions.size)
        assertEquals(0, api.renders)
        assertTrue(requests.isEmpty())
        rule.onNodeWithText("HTTP action").assertExists()

        rule.activityRule.scenario.recreate()
        showPage(api, "0".repeat(32), availableAdapter = native.adapter)
        rule.onNodeWithText("Native ordinary page").assertExists()
        assertEquals(0, api.renders)
        assertEquals(1, api.actions.size)
        runBlocking { native.session.close() }
    }

    private fun showPage(
        api: PageUi,
        token: String,
        availableAdapter: NativeExtensionStreamAdapter? = null,
        locked: suspend () -> Boolean = { false },
        notificationIntent: Intent? = null,
    ) {
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                ExtensionUiPage(api, "example-gateway", "usage", "Page", onBack = {},
                    availableAdapter = availableAdapter, notificationActionToken = token, notificationIntent = notificationIntent,
                    notificationStore = store, notificationLocked = locked)
            }
        }
    }

    private class NativeFixture(val adapter: NativeExtensionStreamAdapter, val session: NativeStreamSession)

    private fun nativeFixture(requests: MutableList<ExtensionUIStreamRequest>): NativeFixture {
        val identity = NativeStreamIdentity("owner", "group-a", "example-gateway", "usage", "navigation-a")
        val payload = """{"protocol":1,"session":"session-a","epoch":"epoch-a","version":1,"root_key":"root","view":{"title":"Native","sections":[{"key":"main","items":[{"type":"text","key":"text","text":"Native ordinary page"}]}]}}"""
        lateinit var session: NativeStreamSession
        session = NativeStreamSession(identity, ExtensionUIContext(platform = "android", locale = "en", uiVersion = 6,
            stream = ExtensionUIStreamCapability(protocol = 1, components = listOf("text"))),
            NativeStreamTransport { request ->
                requests += request
                if (request is ExtensionUIStreamOpenRequest) session.snapshot(identity, request.requestId, payload)
            })
        return NativeFixture(NativeExtensionStreamAdapter(session, shown = true), session)
    }

    private suspend fun resolve(record: persona.core.contracts.api.ExtensionNotificationActionRecord, text: String?) =
        resolveExtensionNotificationPage(record.token, record.extension, record.ui!!, record.scope.manager, text, store)

    private class PageUi(private val result: ExtensionUiResult) : ExtensionApiStub() {
        var renders = 0
        val actions = mutableListOf<Triple<String, String?, Map<String, String>?>>()
        override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?): ExtensionView {
            renders++
            return ExtensionView()
        }
        override suspend fun uiAction(extension: String, ui: String, action: String, context: ExtensionUiContext?, state: String?,
            values: Map<String, String>?): ExtensionUiResult {
            actions += Triple(action, state, values)
            return result
        }
    }
}
