package persona.platform.phonetools

import android.content.Context
import persona.core.contracts.HubScope
import persona.core.contracts.api.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Semaphore

/**
 * Owned by the paired hub scope, never by a settings screen or an Activity.
 *
 * Calls are received concurrently: a confirmed write waiting for the user does not stop later calls (reads
 * included) from being accepted and executed. A bounded [Semaphore] caps how many calls are in flight so a
 * flood of calls still applies back-pressure to the transport queue. A confirmed write whose local execution
 * has started is no longer killed by the enabled / permission / visibility watcher: it must produce a definite
 * result or failure, which is then delivered via [PhoneToolResultOutbox] if the socket cannot take it.
 */
class PhoneToolsRuntime(
    context: Context, hubScope: HubScope, private val deviceId: String,
    private val connection: HubConnection, private val transport: PhoneToolsTransport,
    parentScope: CoroutineScope, private val simulator: Boolean = false,
    private val automation: Boolean = false,
    /** What the hub and settings call this phone (register_phone device_name): "manufacturer model", as the old app sent. */
    private val deviceName: String = listOf(android.os.Build.MANUFACTURER, android.os.Build.MODEL)
        .filter { it.isNotBlank() }.joinToString(" ").ifBlank { "Android" },
    /** Test seam: replaces the platform backend so the runtime's concurrency can be exercised without Android
     * system providers. Defaults to the real `AndroidPhoneTools.execute`. */
    executeOverride: (suspend (String, kotlinx.serialization.json.JsonObject) -> PhoneToolResult)? = null,
    prepareConfirmationOverride: (suspend (String, kotlinx.serialization.json.JsonObject) -> PhonePreparedConfirmation)? = null,
) : AutoCloseable {
    private val job = SupervisorJob(parentScope.coroutineContext[Job])
    private val scope = CoroutineScope(parentScope.coroutineContext + job + Dispatchers.Main.immediate)
    val settings = PhoneToolSettings(context.applicationContext, hubScope, scope)
    val permissions = PhonePermissions(context.applicationContext)
    val confirmations = PhoneConfirmations(context.applicationContext)
    private val visibility = MutableStateFlow(false)
    private val resident = MutableStateFlow(false)
    private val permissionRevision = MutableStateFlow(0)
    private val backend = AndroidPhoneTools(context) { visibility.value }
    private val outbox = PhoneToolResultOutbox()
    private val inFlight = Semaphore(MAX_IN_FLIGHT)
    private fun available() = !automation && connection.state.value == ConnectionState.Online && (visibility.value || resident.value)
    val registry = PhoneToolRegistry({ settings.enabled.value }, ::available, { visibility.value }, permissions::allowed,
        confirmations::confirm, executeOverride ?: backend::execute, prepareConfirmationOverride ?: backend::prepareConfirmation)

    init {
        require(deviceId.isNotBlank())
        scope.launch {
            connection.state.collectLatest { state ->
                transport.discardPending()
                if (state != ConnectionState.Online) {
                    transport.register(deviceId, deviceName, simulator, emptyList())
                    return@collectLatest
                }
                // Reconnected: results held while the socket was down go out first, in their original order.
                // The hub matches them by call_id whatever connection they arrive on and decides whether it
                // still waits; nothing here re-executes a tool.
                outbox.flush { callId, result -> transport.result(callId, result) }
                coroutineScope {
                    launch {
                        combine(settings.enabled, visibility, resident, permissionRevision) { _, _, _, _ -> registry.enabledSpecs() }
                            .distinctUntilChanged().collect { specs -> transport.register(deviceId, deviceName, simulator, specs) }
                    }
                    transport.calls.collect { call ->
                        // Acquire the permit *inside* the collector, before launching: the transport channel
                        // and the semaphore together bound how many calls are in flight, so a flood backs up
                        // into the channel (which then refuses new calls) instead of spawning unbounded
                        // waiting coroutines. collectLatest cancels this acquire when the connection drops,
                        // so a call that never started is discarded rather than surviving into the next
                        // socket. Calls that already acquired a permit and launched live on the pairing
                        // scope and keep running across a transient reconnect.
                        inFlight.acquire()
                        scope.launch {
                            try {
                                coroutineScope {
                                    val execution = async { registry.call(call) }
                                    val watcher = launch {
                                        combine(visibility, resident, settings.enabled, permissionRevision, registry.writesInFlight) {
                                                visible, _, _, _, writes -> visible to writes
                                        }.collect { (visible, writes) ->
                                            // A confirmed write whose local execute has started must finish on
                                            // its own; the result may end up uncertain but it is never silently
                                            // abandoned. Pairing close still cancels through the parent job.
                                            if (call.callId in writes) return@collect
                                            val tool = PhoneToolCatalog.all.firstOrNull { it.spec.name == call.name }
                                            if (!available() || tool != null &&
                                                (tool.source !in settings.enabled.value || !permissions.allowed(call.name, tool.source) || tool.foregroundOnly && !visible)) {
                                                execution.cancel()
                                            }
                                        }
                                    }
                                    try {
                                        val result = try { execution.await() }
                                        catch (cancelled: CancellationException) {
                                            currentCoroutineContext().ensureActive()
                                            PhoneToolText.interrupted()
                                        }
                                        // Send now if the socket can take it; otherwise hold only the result
                                        // (never the call) for a later flush on reconnect.
                                        outbox.send(call.callId, result) { transport.result(call.callId, result) }
                                    } finally { watcher.cancel() }
                                }
                            } finally {
                                inFlight.release()
                            }
                        }
                    }
                }
            }
        }
    }
    /** Feed application foreground state, not the visibility of the settings destination. */
    fun setForeground(value: Boolean) { visibility.value = value; confirmations.setForeground(value); refreshPermissions() }
    /** True only while the shared foreground service is actually keeping this connection alive. */
    fun setResidentConnection(value: Boolean) { resident.value = value }
    fun refreshPermissions() { permissionRevision.value += 1 }
    override fun close() {
        transport.register(deviceId, deviceName, simulator, emptyList())
        confirmations.close(); outbox.close(); job.cancel(); transport.discardPending()
    }

    private companion object { const val MAX_IN_FLIGHT = 8 }
}
