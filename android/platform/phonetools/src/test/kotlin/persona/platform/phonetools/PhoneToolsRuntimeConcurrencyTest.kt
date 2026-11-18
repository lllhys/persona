package persona.platform.phonetools

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestDispatcher
import kotlinx.coroutines.withTimeoutOrNull
import kotlin.coroutines.ContinuationInterceptor
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import org.junit.Assert.*
import org.junit.After
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Hello
import persona.core.contracts.ServerEvent
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.PhoneToolCall
import persona.core.contracts.api.PhoneToolResult
import persona.core.contracts.api.PhoneToolSpec
import persona.core.contracts.api.PhoneToolsTransport
import persona.core.contracts.HubScope

/**
 * Concurrency contract for `PhoneToolsRuntime`: a write waiting for the user does not stop later calls, results
 * that cannot be sent on the socket are held and flushed (not re-executed), and the watcher does not kill a
 * confirmed write whose local execute has started.
 *
 * Uses Robolectric for the Android Context the runtime needs (settings, permissions, notifications). No emulator.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class PhoneToolsRuntimeConcurrencyTest {
    /** A transport whose `result` outcome the test controls, to exercise the outbox. */
    private class FakeTransport : PhoneToolsTransport {
        val incoming = Channel<PhoneToolCall>(capacity = Channel.UNLIMITED)
        override val calls: Flow<PhoneToolCall> = incoming.receiveAsFlow()
        val sent = mutableListOf<Pair<String, PhoneToolResult>>()
        val registered = mutableListOf<List<PhoneToolSpec>>()
        /** When false the next `result()` call fails; the runtime must hold the result in its outbox. */
        @Volatile var sendSucceeds = true

        override fun discardPending() { while (incoming.tryReceive().isSuccess) { /* drop */ } }
        override fun register(deviceId: String, deviceName: String, simulator: Boolean, tools: List<PhoneToolSpec>): Boolean {
            registered += tools; return true
        }
        override fun result(callId: String, result: PhoneToolResult): Boolean {
            if (!sendSucceeds) return false
            sent += callId to result
            return true
        }
    }

    private class FakeConnection(initial: ConnectionState = ConnectionState.Online) : HubConnection {
        override val state = MutableStateFlow(initial)
        override val events = MutableSharedFlow<ServerEvent>(extraBufferCapacity = 16)
        override fun reconnect() {}
        override fun send(message: JsonObject): Boolean = true
        override val resumed: Boolean get() = false
        override val lastHello: Hello? get() = null
    }

    private fun runtime(
        scope: CoroutineScope,
        transport: FakeTransport,
        connection: FakeConnection,
        execute: (suspend (String, JsonObject) -> PhoneToolResult)? = null,
    ): PhoneToolsRuntime {
        val scheduler = (scope.coroutineContext[ContinuationInterceptor] as TestDispatcher).scheduler
        Dispatchers.setMain(StandardTestDispatcher(scheduler))
        val context = ApplicationProvider.getApplicationContext<Context>()
        // Grant every runtime permission the tools check, so calls reach the backend instead of being refused.
        org.robolectric.Shadows.shadowOf(context as android.app.Application).grantPermissions(
            android.Manifest.permission.READ_CALENDAR, android.Manifest.permission.WRITE_CALENDAR,
            android.Manifest.permission.READ_CONTACTS, android.Manifest.permission.ACCESS_COARSE_LOCATION,
            android.Manifest.permission.ACCESS_FINE_LOCATION, android.Manifest.permission.POST_NOTIFICATIONS,
        )
        val hubScope = HubScope("test-hub", "gen-1")
        val rt = PhoneToolsRuntime(context, hubScope, "device-1", connection, transport, scope, simulator = true, executeOverride = execute)
        return rt
    }

    private suspend fun enableAllSources(rt: PhoneToolsRuntime) {
        for (source in PhoneSource.entries) rt.settings.setEnabled(source, true)
    }

    @After fun resetMainDispatcher() = Dispatchers.resetMain()

    /**
     * #4456: transport delivers write-wait, two reads and a second write; the reads complete while the first
     * write still waits for the user. The semaphore caps in-flight calls without losing any.
     */
    @Test fun aWaitingWriteDoesNotStopLaterReadsAndTheSecondWriteQueues() = runTest {
        val transport = FakeTransport()
        val connection = FakeConnection()
        val scope = CoroutineScope(coroutineContext + SupervisorJob())
        val readResults = mutableListOf<String>()
        val rt = runtime(scope, transport, connection) { name, _ ->
            if (name == "phone_device_info" || name == "phone_calendar_events") readResults += name
            PhoneToolResult.text("ok")
        }
        enableAllSources(rt)
        rt.setForeground(true)
        advanceUntilIdle()

        val writeArgs = buildJsonObject {
            put("title", kotlinx.serialization.json.JsonPrimitive("测试日程"))
            put("start", kotlinx.serialization.json.JsonPrimitive("2026-11-01T10:00:00+08:00"))
            put("end", kotlinx.serialization.json.JsonPrimitive("2026-11-01T11:00:00+08:00"))
        }
        val empty = buildJsonObject {}
        transport.incoming.send(PhoneToolCall("w1", "phone_calendar_create_event", writeArgs))
        transport.incoming.send(PhoneToolCall("r1", "phone_device_info", empty))
        transport.incoming.send(PhoneToolCall("r2", "phone_device_info", empty))
        transport.incoming.send(PhoneToolCall("w2", "phone_calendar_create_event", writeArgs))
        runCurrent()
        // Reads finish; writes wait for the user (only one card on screen at a time).
        val readIds = withTimeoutOrNull(2_000) {
            while (transport.sent.count { it.first == "r1" || it.first == "r2" } < 2) kotlinx.coroutines.delay(10)
            transport.sent.map { it.first }
        }
        assertNotNull("reads must complete while a write waits for the user", readIds)
        assertTrue(transport.sent.none { it.first == "w1" || it.first == "w2" })
        // Answer the first write as Deny; the second write's card comes up next.
        val firstToken = rt.confirmations.pending.value?.token
        assertNotNull(firstToken)
        rt.confirmations.resolve(firstToken!!, false)
        advanceUntilIdle()
        assertTrue(transport.sent.any { it.first == "w1" })
        // The second write is now current and still unanswered.
        val secondToken = rt.confirmations.pending.value?.token
        assertNotNull(secondToken)
        assertNotEquals(firstToken, secondToken)
        rt.confirmations.resolve(secondToken!!, false)
        advanceUntilIdle()
        assertTrue(transport.sent.any { it.first == "w2" })
        rt.close()
        scope.cancel()
    }

    /**
     * #4456: a confirmed write whose result could not be sent (socket dropped right after Allow) is held and
     * re-sent on reconnect, not re-executed.
     */
    @Test fun aWriteResultThatCouldNotBeSentIsHeldAndFlushedOnReconnect() = runTest {
        val transport = FakeTransport()
        val connection = FakeConnection()
        val scope = CoroutineScope(coroutineContext + SupervisorJob())
        var executions = 0
        val rt = runtime(scope, transport, connection) { _, _ ->
            executions++
            PhoneToolResult.text("created")
        }
        enableAllSources(rt)
        rt.setForeground(true)
        advanceUntilIdle()

        val writeArgs = buildJsonObject {
            put("title", kotlinx.serialization.json.JsonPrimitive("测试日程"))
            put("start", kotlinx.serialization.json.JsonPrimitive("2026-11-01T10:00:00+08:00"))
            put("end", kotlinx.serialization.json.JsonPrimitive("2026-11-01T11:00:00+08:00"))
        }
        transport.incoming.send(PhoneToolCall("w1", "phone_calendar_create_event", writeArgs))
        runCurrent()
        val token = rt.confirmations.pending.value?.token
        assertNotNull(token)
        // The socket drops right as the user allows: transport.result returns false.
        transport.sendSucceeds = false
        rt.confirmations.resolve(token!!, true)
        advanceUntilIdle()
        // Nothing was sent; the result is held in the outbox.
        assertTrue(transport.sent.isEmpty())
        // Reconnect: the runtime flushes the outbox in FIFO order, without re-executing the tool.
        transport.sendSucceeds = true
        connection.state.value = ConnectionState.Connecting
        runCurrent()
        connection.state.value = ConnectionState.Online
        advanceUntilIdle()
        // The held result has now been delivered. The hub matches it by call_id; no second execute happened.
        assertTrue(transport.sent.any { it.first == "w1" })
        assertEquals(1, executions)
        rt.close()
        scope.cancel()
    }

    /**
     * #4456: once a confirmed write has entered its local execute (writesInFlight), the watcher stops trying
     * to cancel it. The result may end up uncertain but it is not silently abandoned mid-write.
     */
    @Test fun theWatcherDoesNotKillAConfirmedWriteWhoseExecuteHasStarted() = runTest {
        val transport = FakeTransport()
        val connection = FakeConnection()
        val scope = CoroutineScope(coroutineContext + SupervisorJob())
        val executeStarted = CompletableDeferred<Unit>()
        val executeGate = CompletableDeferred<Unit>()
        val rt = runtime(scope, transport, connection) { _, _ ->
            executeStarted.complete(Unit)
            executeGate.await()  // hold the local execute so writesInFlight stays observable
            PhoneToolResult.text("created")
        }
        enableAllSources(rt)
        rt.setForeground(true)
        advanceUntilIdle()

        val writeArgs = buildJsonObject {
            put("title", kotlinx.serialization.json.JsonPrimitive("测试日程"))
            put("start", kotlinx.serialization.json.JsonPrimitive("2026-11-01T10:00:00+08:00"))
            put("end", kotlinx.serialization.json.JsonPrimitive("2026-11-01T11:00:00+08:00"))
        }
        transport.incoming.send(PhoneToolCall("w1", "phone_calendar_create_event", writeArgs))
        runCurrent()
        val token = rt.confirmations.pending.value?.token
        assertNotNull(token)
        // Allow: the write enters its local execute. Hold it there so the watcher would fire if it still could.
        rt.confirmations.resolve(token!!, true)
        executeStarted.await()
        // The write is now executing: writesInFlight carries the call-id.
        assertTrue(rt.registry.writesInFlight.value.contains("w1"))
        // Turn the Calendar source off mid-write: the watcher fires (it observes settings.enabled), and before
        // this lane it would have cancelled the call. An in-flight confirmed write must not be killed.
        rt.settings.setEnabled(PhoneSource.Calendar, false)
        runCurrent()
        // The execute is still running (not cancelled); no result has been sent yet.
        assertTrue(executeGate.isActive)
        assertTrue(transport.sent.isEmpty())
        // Let the local execute finish: the write produces a result, which is then delivered (or held). It is
        // not silently abandoned just because the toggle changed mid-write.
        executeGate.complete(Unit)
        advanceUntilIdle()
        assertTrue(transport.sent.any { it.first == "w1" })
        rt.close()
        scope.cancel()
    }

    /**
     * Back-pressure: the permit is acquired inside the collector, before launch. Once every permit is taken
     * by a still-executing call, the collector suspends on the next acquire and stops draining the transport
     * channel. The number of calls that started executing stays bounded no matter how many more arrive;
     * there is no unbounded set of waiting coroutines.
     */
    @Test fun backpressureBoundsHowManyCallsStartExecuting() = runTest {
        val transport = FakeTransport()
        val connection = FakeConnection()
        val scope = CoroutineScope(coroutineContext + SupervisorJob())
        val gate = CompletableDeferred<Unit>()
        var started = 0
        val rt = runtime(scope, transport, connection) { _, _ ->
            started++
            gate.await()  // hold every execute so permits are never released
            PhoneToolResult.text("ok")
        }
        enableAllSources(rt)
        rt.setForeground(true)
        advanceUntilIdle()
        val empty = buildJsonObject {}
        // Far more calls than permits (8) plus the transport channel's own capacity.
        repeat(8 + 16 + 10) { i -> transport.incoming.send(PhoneToolCall("c$i", "phone_device_info", empty)) }
        runCurrent()
        // Only the permits' worth of calls started; the rest are still in the channel or suspended on acquire.
        assertEquals(8, started)
        gate.complete(Unit)
        advanceUntilIdle()
        rt.close()
        scope.cancel()
    }

    /**
     * A confirmed write that arrived on the old socket and is still waiting for a permit must not survive
     * into the next connection. collectLatest cancels the collector (and with it the pending acquire) when
     * the connection drops, so the queued write never launches: no confirmation card, no execute.
     */
    @Test fun aCallWaitingForAPermitDoesNotSurviveAReconnect() = runTest {
        val transport = FakeTransport()
        val connection = FakeConnection()
        val scope = CoroutineScope(coroutineContext + SupervisorJob())
        val gate = CompletableDeferred<Unit>()
        var executions = 0
        val rt = runtime(scope, transport, connection) { _, _ ->
            executions++
            gate.await()
            PhoneToolResult.text("ok")
        }
        enableAllSources(rt)
        rt.setForeground(true)
        advanceUntilIdle()
        // Fill every permit with reads whose execute never finishes.
        repeat(8) { i -> transport.incoming.send(PhoneToolCall("fill-$i", "phone_device_info", buildJsonObject {})) }
        runCurrent()
        assertEquals(8, executions)
        // This confirmed write arrives now and suspends on acquire: it has not launched.
        val writeArgs = buildJsonObject {
            put("title", kotlinx.serialization.json.JsonPrimitive("测试日程"))
            put("start", kotlinx.serialization.json.JsonPrimitive("2026-11-01T10:00:00+08:00"))
            put("end", kotlinx.serialization.json.JsonPrimitive("2026-11-01T11:00:00+08:00"))
        }
        transport.incoming.send(PhoneToolCall("queued-write", "phone_calendar_create_event", writeArgs))
        runCurrent()
        assertNull(rt.confirmations.pending.value)
        // Drop the connection: the collector's pending acquire is cancelled, discarding the queued write.
        connection.state.value = ConnectionState.Connecting
        runCurrent()
        connection.state.value = ConnectionState.Online
        advanceUntilIdle()
        // The queued write never showed a card and never executed, even though permits are about to free up.
        assertNull(rt.confirmations.pending.value)
        assertEquals(8, executions)
        gate.complete(Unit)
        advanceUntilIdle()
        rt.close()
        scope.cancel()
    }
}
