package persona.core.hub

import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.encodeToString
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.*
import persona.core.contracts.api.*

class CodexFirstMessageTransportTest {
    private val pairing = HubScope("hub", "pairing")
    private val waiting = Session("computer", "waiting", agent = "codex", status = "idle", paneId = "pane", awaitingFirstMessage = true)
    private val thread = waiting.copy(machineId = "", sessionId = "thread", awaitingFirstMessage = false)
    private fun response(code: Int, body: String) = Response.Builder().protocol(Protocol.HTTP_1_1)
        .request(Request.Builder().url("https://example.com/").build()).code(code).message("test").body(body.toResponseBody()).build()

    @Test fun productionComposerAndItsRefusalRetryBothAdoptTheReturnedThread() = runBlocking {
        val jobs = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val adopted = mutableListOf<Session>()
        val fingerprint = java.util.Base64.getUrlEncoder().withoutPadding().encodeToString(ByteArray(32))
        OkHttpHubConnection(HubConnectionConfig("hub", fingerprint, "test-token", emptyList(), "test"),
            HubNetwork("initial", available = false)).use { connection ->
            try {
                val http = HttpMessageTransport(pairing, connection, jobs, onSessionStarted = { _, session -> adopted += session })
                val transport = object : MessageTransport by http { override val readyToSend = MutableStateFlow(true) }
                var refuse = false
                val service = HttpChatComposerService(pairing, transport, request = { _, _, _ ->
                    if (refuse) {
                        refuse = false
                        response(409, """{"error":"draft","code":"input_draft","delivered":"none","draft":"draft","clearable":"draft"}""")
                    } else response(200, """{"ok":true,"session":${JsonCodec.encodeToString(thread)}}""")
                })
                assertEquals(MessageTransportResult.Accepted, service.send(MessageRequest(pairing, waiting.key, "Review", "first")))
                assertEquals(thread.copy(machineId = "computer"), adopted.single())
                adopted.clear()
                refuse = true
                assertTrue(service.send(MessageRequest(pairing, waiting.key, "Review", "second")) is MessageTransportResult.HubError)
                assertTrue(adopted.isEmpty())
                assertEquals(DeliveryState.Sent, service.retryRefused("second", clearDraft = true).state)
                assertEquals(thread.copy(machineId = "computer"), adopted.single())
            } finally { jobs.cancel() }
        }
    }

}
