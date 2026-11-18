package persona.core.contracts.api

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import persona.core.contracts.StandingAuthorizationList

/**
 * The current assistant's standing authorizations (GET and DELETE
 * `/api/v1/assistant/standing-authorizations`). A device without the full-access role is answered 403.
 *
 * [hubIdentity] changes when the selected hub changes. [pinned] keeps list and revoke on the hub that was selected
 * when it was called, so a page that stays open does not revoke a grant on a hub the row did not come from.
 */
interface StandingAuthorizationsSource {
    /** The selected hub. Empty when this source does not follow a hub (a test, or one that is already pinned). */
    val hubIdentity: StateFlow<String>
        get() = NoHub

    /**
     * List and revoke on the hub selected now, and keep using it after the selection changes.
     * A source that is already one hub returns itself.
     */
    fun pinned(): StandingAuthorizationsSource = this

    suspend fun list(): StandingAuthorizationList
    suspend fun revoke(id: String)

    companion object {
        val NoHub: StateFlow<String> = MutableStateFlow("")
    }
}
