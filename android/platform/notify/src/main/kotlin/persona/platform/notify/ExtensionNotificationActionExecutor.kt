package persona.platform.notify

import java.io.IOException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.withTimeoutOrNull
import persona.core.contracts.api.ExtensionNotificationActionRecord
import persona.core.contracts.api.ExtensionNotificationClaim
import persona.core.contracts.api.ExtensionUiContext
import persona.core.contracts.api.ExtensionUiNotFound
import persona.core.contracts.api.ExtensionUiUnsupported

sealed interface ExtensionNotificationActionReport {
    data class Toast(val record: ExtensionNotificationActionRecord, val text: String) : ExtensionNotificationActionReport
    data class Unavailable(val record: ExtensionNotificationActionRecord) : ExtensionNotificationActionReport
    data class PageExpired(val record: ExtensionNotificationActionRecord) : ExtensionNotificationActionReport
    data class WrongScope(val record: ExtensionNotificationActionRecord) : ExtensionNotificationActionReport
    data class ActionFailed(val record: ExtensionNotificationActionRecord) : ExtensionNotificationActionReport
}

/** Runs only an already-claimed action through the existing ExtensionUi transport; no socket or retry is created. */
class ExtensionNotificationActionExecutor(private val environment: ExtensionNotificationEnvironment) {
    suspend fun execute(claim: ExtensionNotificationClaim.Claimed): ExtensionNotificationActionReport? {
        val record = claim.record
        if (environment.currentScope() != record.scope) return ExtensionNotificationActionReport.WrongScope(record)
        val ui = environment.extensionUi(record) ?: return ExtensionNotificationActionReport.WrongScope(record)
        val page = record.ui ?: return ExtensionNotificationActionReport.ActionFailed(record)
        val action = record.action ?: return ExtensionNotificationActionReport.ActionFailed(record)
        return try {
            val result = ui.uiAction(record.extension, page, action.id, ExtensionUiContext(platform = "android"),
                record.state, claim.values.takeIf { it.isNotEmpty() })
            result.toast?.takeIf { it.isNotBlank() }?.let { ExtensionNotificationActionReport.Toast(record, it) }
        } catch (cancelled: CancellationException) {
            if (environment.currentScope() != record.scope) ExtensionNotificationActionReport.WrongScope(record) else throw cancelled
        } catch (_: ExtensionUiNotFound) {
            ExtensionNotificationActionReport.PageExpired(record)
        } catch (_: ExtensionUiUnsupported) {
            ExtensionNotificationActionReport.PageExpired(record)
        } catch (_: IOException) {
            ExtensionNotificationActionReport.Unavailable(record)
        } catch (_: Exception) {
            ExtensionNotificationActionReport.ActionFailed(record)
        }
    }
}

internal sealed interface ExtensionNotificationDispatch {
    data class Completed(val report: ExtensionNotificationActionReport?) : ExtensionNotificationDispatch
    data class Timeout(val record: ExtensionNotificationActionRecord) : ExtensionNotificationDispatch
}

/** A receiver deadline means unconfirmed completion, never success and never another uiAction. */
internal suspend fun runExtensionNotificationWithDeadline(
    claim: ExtensionNotificationClaim.Claimed,
    executor: ExtensionNotificationActionExecutor,
    timeoutMillis: Long = 8_000,
): ExtensionNotificationDispatch = withTimeoutOrNull(timeoutMillis) {
    ExtensionNotificationDispatch.Completed(executor.execute(claim))
} ?: ExtensionNotificationDispatch.Timeout(claim.record)
