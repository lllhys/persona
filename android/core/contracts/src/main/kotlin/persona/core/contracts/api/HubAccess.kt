package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * A Cloudflare Access service token for one paired hub published behind Cloudflare Access (docs/operations/network.md
 * "Behind Cloudflare Access"). Sent as [ClientIdHeader] and [ClientSecretHeader] on every request to that hub's own
 * HTTPS origins only; the secret is stored like the hub token and never printed.
 */
@Serializable
data class HubAccess(
    @SerialName("client_id") val clientId: String,
    @SerialName("client_secret") val clientSecret: String,
) {
    override fun toString() = "HubAccess(<redacted>)"

    companion object {
        const val ClientIdHeader = "CF-Access-Client-Id"
        const val ClientSecretHeader = "CF-Access-Client-Secret"

        /** Both values trimmed, or null when either is empty (no token). */
        fun of(clientId: String, clientSecret: String): HubAccess? =
            HubAccess(clientId.trim(), clientSecret.trim()).takeIf { it.clientId.isNotEmpty() && it.clientSecret.isNotEmpty() }
    }
}
