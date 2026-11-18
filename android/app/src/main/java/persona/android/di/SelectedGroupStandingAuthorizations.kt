package persona.android.di

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import persona.core.contracts.api.AssistantManagement
import persona.core.contracts.api.StandingAuthorizationsSource
import persona.core.hub.HubError
import persona.core.hub.HubFailure

/**
 * One hub's standing-authorizations client, still speaking that hub's plain machine ids.
 * [identity] is the group id the page keys on. [names] turns those ids into the merged lists' ids.
 */
internal class StandingHub(
    val identity: String,
    val names: GroupNames,
    val management: AssistantManagement,
)

/**
 * Settings › Standing Authorizations for the selected group. Reads go through [AssistantManagement.inGroup], the same
 * translation the assistant profile's count uses, so a computer's name is that hub's and not another hub's machine
 * that happens to share the raw id. [pinned] keeps revoke on the hub that listed the row.
 * [current] is null when no hub is selected. [pinned] still returns: the page is composed with that client, and the
 * read fails afterwards as "no network" instead of crashing composition.
 */
internal class SelectedGroupStandingAuthorizations(
    private val current: () -> StandingHub?,
    override val hubIdentity: StateFlow<String>,
) : StandingAuthorizationsSource {
    private fun translated(hub: StandingHub): AssistantManagement = hub.management.inGroup(hub.names)

    override fun pinned(): StandingAuthorizationsSource {
        val hub = current() ?: return offline()
        val api = translated(hub)
        return object : StandingAuthorizationsSource {
            override val hubIdentity: StateFlow<String> = MutableStateFlow(hub.identity)
            override fun pinned(): StandingAuthorizationsSource = this
            override suspend fun list() = api.standingAuthorizations()
            override suspend fun revoke(id: String) = api.revokeStandingAuthorization(id)
        }
    }

    override suspend fun list() = hub().standingAuthorizations()
    override suspend fun revoke(id: String) = hub().revokeStandingAuthorization(id)

    private fun hub(): AssistantManagement = current()?.let(::translated) ?: throw HubFailure(HubError.NoNetwork)

    /** Identity "" so the page treats this as its own hub. The calls fail once the model asks. */
    private fun offline() = object : StandingAuthorizationsSource {
        override val hubIdentity: StateFlow<String> = MutableStateFlow("")
        override fun pinned(): StandingAuthorizationsSource = this
        override suspend fun list() = throw HubFailure(HubError.NoNetwork)
        override suspend fun revoke(id: String) = throw HubFailure(HubError.NoNetwork)
    }
}
