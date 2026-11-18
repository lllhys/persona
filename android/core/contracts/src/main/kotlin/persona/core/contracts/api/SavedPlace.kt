package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * A saved place (Settings › Assistant › Saved Places, #1598 / #9293): what a reminder by place goes off at when
 * the user says "home" or "the office". The hub keeps them; the assistant reads them (`list_places`).
 *
 * [radiusMeters] defaults to 150 when the hub omits it; the hub clamps to 100–2000.
 */
@Serializable
data class SavedPlace(
    val id: String = "",
    val name: String = "",
    val address: String = "",
    val latitude: Double = 0.0,
    val longitude: Double = 0.0,
    @SerialName("radius_meters") val radiusMeters: Int = DEFAULT_RADIUS,
    @SerialName("created_at") val createdAt: String? = null,
    @SerialName("updated_at") val updatedAt: String? = null,
) {
    companion object {
        const val DEFAULT_RADIUS = 150
        val RADIUS_RANGE = 100..2000
        /** Reminder-radius choices in the editor, in meters (same as iOS). */
        val PRESET_RADII = listOf(100, 150, 300, 500, 1000, 2000)
    }
}
