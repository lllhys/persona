package persona.core.data

import persona.core.contracts.HubScope
import persona.core.contracts.SessionKey
import persona.core.contracts.api.MessageEvidence
import persona.core.contracts.api.MessageRequest
import persona.core.contracts.api.MessageTransport
import persona.core.contracts.api.MessageTransportResult
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class OutboxLateReceiptTest {
    private val pairing = HubScope("test-hub", "generation-a")
    private val session = SessionKey("machine", "session")
    private val message = OutboxEntry("message", session, "test message",
        state = OutboxState.NeedsConfirmation, createdAt = 0)

    @Test fun successfulReceiptSettlesMessageAfterReviewTimeoutWithoutResending() = runTest {
        val store = MemoryOutboxStore().also { it.enqueue(message) }
        val transport = ReceiptTransport { MessageTransportResult.Accepted }
        val sender = OutboxMessageSender(pairing, store, transport, backgroundScope)
        runCurrent()
        transport.readyToSend.value = true
        runCurrent()
        assertEquals(OutboxState.Sent, store.state(message.clientMessageId))
        assertTrue(transport.requests.isEmpty())
        sender.close()
    }

    @Test fun unknownOrFailedReceiptDoesNotConfirmDeliveryAfterReviewTimeout() = runTest {
        for (result in listOf(MessageTransportResult.NoReceipt,
            MessageTransportResult.HubError("internal", "unknown"),
            MessageTransportResult.HubError("invalid_request", "none"))) {
            val store = MemoryOutboxStore().also { it.enqueue(message) }
            val transport = ReceiptTransport { result }
            val sender = OutboxMessageSender(pairing, store, transport, backgroundScope)
            runCurrent()
            transport.readyToSend.value = true
            runCurrent()
            assertEquals(result.toString(), OutboxState.NeedsConfirmation, store.state(message.clientMessageId))
            assertTrue(transport.requests.isEmpty())
            sender.close()
        }
    }

    @Test fun successfulReceiptDoesNotRestoreMessageDiscardedWhileLookupWasPending() = runTest {
        val store = MemoryOutboxStore().also { it.enqueue(message) }
        val lookupStarted = CompletableDeferred<Unit>()
        val receipt = CompletableDeferred<MessageTransportResult>()
        val transport = ReceiptTransport {
            lookupStarted.complete(Unit)
            receipt.await()
        }
        val sender = OutboxMessageSender(pairing, store, transport, backgroundScope)
        runCurrent()
        transport.readyToSend.value = true
        runCurrent()
        assertTrue(lookupStarted.isCompleted)
        assertTrue(sender.discard(message.clientMessageId))
        receipt.complete(MessageTransportResult.Accepted)
        runCurrent()
        assertEquals(OutboxState.Cancelled, store.state(message.clientMessageId))
        assertTrue(transport.requests.isEmpty())
        sender.close()
    }

    private class ReceiptTransport(val lookup: suspend () -> MessageTransportResult) : MessageTransport {
        override val readyToSend = MutableStateFlow(false)
        override val supportsClientOps = true
        val requests = mutableListOf<MessageRequest>()
        override suspend fun reconcile(request: MessageRequest, createdAt: Long, allowTextMatch: Boolean) =
            MessageEvidence.Receipt(lookup())
        override suspend fun send(request: MessageRequest): MessageTransportResult {
            requests += request
            error("Receipt reconciliation must not resend a message")
        }
    }
}
