package persona.core.contracts.api

import persona.core.contracts.CoreTextException
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.SessionKey
import kotlinx.coroutines.flow.StateFlow
import java.io.File

data class ComposerRefusal(
    val session: SessionKey,
    val clientMessageId: String,
    val code: String,
    val draft: String? = null,
    val clearable: String? = null,
)

data class ComposerDelivery(val state: DeliveryState, val refusal: ComposerRefusal? = null)

/** Bind this and MessageTransport to the same pairing-scoped instance. */
interface ChatComposerService {
    val deliveries: StateFlow<Map<String, ComposerDelivery>>
    suspend fun upload(session: SessionKey, name: String, file: File): String
    /**
     * Upload with an explicit destination. Ordinary chat files ask for `attachment`; a Health conversation keeps `health`.
     * The default keeps older fakes and alternate transports on their existing temporary-upload behavior.
     */
    suspend fun upload(session: SessionKey, name: String, file: File, destination: String): String = upload(session, name, file)
    suspend fun interrupt(session: SessionKey)
    /** Retry only a recorded refusal, retaining the original operation id and payload. */
    suspend fun retryRefused(clientMessageId: String, clearDraft: Boolean): ComposerDelivery
}

class ComposerOperationException(text: CoreText) : CoreTextException(text)
