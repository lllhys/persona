package persona.platform.notify

import java.io.IOException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.ExtensionNotificationActionRecord
import persona.core.contracts.api.ExtensionNotificationClaim
import persona.core.contracts.api.ExtensionNotificationScope
import persona.core.contracts.api.ExtensionStreamBinding
import persona.core.contracts.api.ExtensionUi
import persona.core.contracts.api.ExtensionUiContext
import persona.core.contracts.api.ExtensionUiNotFound
import persona.core.contracts.api.ExtensionUiPlace
import persona.core.contracts.api.ExtensionUiResult
import persona.core.contracts.api.ExtensionView
import persona.core.contracts.api.ExtensionViewPush
import persona.core.contracts.generated.ExtensionNotifyAction

class ExtensionNotificationExecutorTest {
    private val scope = ExtensionNotificationScope("hub/generation", null, "assistant")

    @Test fun actionUsesTheExistingUiActionWithItsOriginalStateAndInput() = runBlocking {
        val ui = FakeUi(ExtensionUiResult(toast = "Done"))
        val record = record()
        val report = ExtensionNotificationActionExecutor(Environment(scope, ui)).execute(claimed(record, mapOf("text" to "hello")))
        assertEquals(ExtensionNotificationActionReport.Toast(record, "Done"), report)
        assertEquals(listOf("example-gateway", "usage", "stop", "state"), ui.arguments.take(4))
        assertEquals("android", ui.context?.platform)
        assertEquals(mapOf("text" to "hello"), ui.values)
    }

    @Test fun backgroundNavigationAndViewsAreNotPretendedToBeShown() = runBlocking {
        for (result in listOf(ExtensionUiResult(view = ExtensionView()), ExtensionUiResult(openUrl = "https://example.com"),
            ExtensionUiResult(push = ExtensionViewPush(ui = "next")), ExtensionUiResult(pop = true))) {
            val report = ExtensionNotificationActionExecutor(Environment(scope, FakeUi(result))).execute(claimed(record()))
            assertNull(report)
        }
    }

    @Test fun failuresAndUnavailableOwnershipProduceNoRetryAndNoCrossGroupCall() = runBlocking {
        val offline = FakeUi(ExtensionUiResult(), IOException("offline"))
        assertTrue(ExtensionNotificationActionExecutor(Environment(scope, offline)).execute(claimed(record()))
            is ExtensionNotificationActionReport.Unavailable)
        assertEquals(1, offline.calls)
        val expired = FakeUi(ExtensionUiResult(), ExtensionUiNotFound("gone"))
        assertTrue(ExtensionNotificationActionExecutor(Environment(scope, expired)).execute(claimed(record()))
            is ExtensionNotificationActionReport.PageExpired)
        val wrongScope = FakeUi(ExtensionUiResult())
        val environment = Environment(scope.copy(manager = "other"), wrongScope)
        assertTrue(ExtensionNotificationActionExecutor(environment).execute(claimed(record()))
            is ExtensionNotificationActionReport.WrongScope)
        assertEquals(0, wrongScope.calls)
    }

    @Test fun defaultAndExplicitManagersResolveDifferentExistingApis() = runBlocking {
        val default = FakeUi(ExtensionUiResult(toast = "default"))
        val explicit = FakeUi(ExtensionUiResult(toast = "explicit"))
        var current = scope
        val environment = object : ExtensionNotificationEnvironment {
            override fun currentScope() = current
            override fun extensionUi(record: ExtensionNotificationActionRecord): ExtensionUi? = when (record.scope.manager) {
                "assistant" -> default
                "other" -> explicit
                else -> null
            }
        }
        assertEquals(ExtensionNotificationActionReport.Toast(record(), "default"),
            ExtensionNotificationActionExecutor(environment).execute(claimed(record())))
        current = scope.copy(manager = "other")
        val other = record().copy(scope = current)
        assertEquals(ExtensionNotificationActionReport.Toast(other, "explicit"),
            ExtensionNotificationActionExecutor(environment).execute(claimed(other)))
    }

    @Test fun cancellationDoesNotBecomeSuccessOrAnAutomaticRetry() = runBlocking {
        val ui = FakeUi(ExtensionUiResult(toast = "late"), CancellationException("cancelled"))
        var thrown = false
        try { ExtensionNotificationActionExecutor(Environment(scope, ui)).execute(claimed(record())) }
        catch (_: CancellationException) { thrown = true }
        assertTrue(thrown)
        assertEquals(1, ui.calls)
    }

    @Test fun receiverDeadlineIsUnconfirmedAndNeverRetries() = runTest {
        val ui = FakeUi(ExtensionUiResult(toast = "late")).apply { blocked = true }
        val record = record()
        val dispatch = runExtensionNotificationWithDeadline(claimed(record),
            ExtensionNotificationActionExecutor(Environment(scope, ui)), timeoutMillis = 10)
        assertEquals(ExtensionNotificationDispatch.Timeout(record), dispatch)
        assertEquals(1, ui.calls)
    }

    @Test fun duplicateReceiverCompletionCallsFinishOnlyOnce() {
        val completion = ExtensionNotificationReceiverCompletion()
        var finishes = 0
        completion.finish { finishes++ }
        completion.finish { finishes++ }
        assertEquals(1, finishes)
    }

    private fun record() = ExtensionNotificationActionRecord("0".repeat(32), "alert", "example-gateway", "usage", "state",
        ExtensionNotifyAction("stop", "Stop"), scope, 1, "dedupe")
    private fun claimed(record: ExtensionNotificationActionRecord, values: Map<String, String> = emptyMap()) =
        ExtensionNotificationClaim.Claimed(record, values)

    private class Environment(private val current: ExtensionNotificationScope, private val ui: ExtensionUi) : ExtensionNotificationEnvironment {
        override fun currentScope() = current
        override fun extensionUi(record: ExtensionNotificationActionRecord) = ui.takeIf { current == record.scope }
    }

    private class FakeUi(private val result: ExtensionUiResult, private val failure: Exception? = null) : ExtensionUi {
        var calls = 0
        lateinit var arguments: List<String?>
        var context: ExtensionUiContext? = null
        var values: Map<String, String>? = null
        var blocked = false
        override val nativeStream: Flow<ExtensionStreamBinding?> = flowOf(null)
        override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?): List<ExtensionUiPlace> = emptyList()
        override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?) = ExtensionView()
        override suspend fun uiAction(extension: String, ui: String, action: String, context: ExtensionUiContext?, state: String?,
            values: Map<String, String>?): ExtensionUiResult {
            calls++
            arguments = listOf(extension, ui, action, state)
            this.context = context
            this.values = values
            if (blocked) awaitCancellation()
            failure?.let { throw it }
            return result
        }
    }
}
