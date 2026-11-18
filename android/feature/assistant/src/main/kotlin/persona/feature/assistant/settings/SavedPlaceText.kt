package persona.feature.assistant.settings

import android.icu.text.MeasureFormat
import android.icu.util.Measure
import android.icu.util.MeasureUnit
import persona.core.contracts.api.SavedPlace
import java.util.Locale

/**
 * What the place editor decides without the hub or a geocoder request: when it may save, the radius choices,
 * how a place is described under its name (same rules as iOS `SavedPlaceText`).
 */
internal object SavedPlaceText {
    val radii: List<Int> get() = SavedPlace.PRESET_RADII

    /** A name and a location: what the hub needs. */
    fun canSave(place: SavedPlace, located: Boolean): Boolean =
        located && place.name.trim().isNotEmpty()

    /**
     * Editable fields only (name, address, coordinates, radius). Timestamps and other hub metadata must not
     * keep Save always enabled (#9293 rework R3).
     */
    fun sameEdit(a: SavedPlace, b: SavedPlace): Boolean =
        a.name.trim() == b.name.trim() &&
            a.address.trim() == b.address.trim() &&
            a.latitude == b.latitude &&
            a.longitude == b.longitude &&
            a.radiusMeters == b.radiusMeters

    /**
     * Whether the editor may save for [placeId]: a new place is always ready; an existing one needs the list
     * loaded and the row present (otherwise Save would POST a duplicate after process death, #9293 rework R2).
     */
    fun editorReady(placeId: String?, entries: List<SavedPlace>?): Boolean {
        if (placeId == null) return true
        if (entries == null) return false
        return entries.any { it.id == placeId }
    }

    /** The choices with [current] among them (a radius the assistant saved that is not one of the presets). */
    fun radiiWith(current: Int): List<Int> =
        if (current in radii) radii else (radii + current).sorted()

    /** "150 m" / "150米", "1 km": a short road distance via MeasureFormat (same idea as iOS Measurement). */
    fun radius(meters: Int, locale: Locale = Locale.getDefault()): String {
        val format = MeasureFormat.getInstance(locale, MeasureFormat.FormatWidth.SHORT)
        return if (meters >= 1000 && meters % 1000 == 0) {
            format.formatMeasures(Measure(meters / 1000, MeasureUnit.KILOMETER))
        } else if (meters >= 1000) {
            format.formatMeasures(Measure(meters / 1000.0, MeasureUnit.KILOMETER))
        } else {
            format.formatMeasures(Measure(meters, MeasureUnit.METER))
        }
    }

    /** The line under a place's name: its address, else its coordinates. */
    fun detail(place: SavedPlace): String =
        if (place.address.isEmpty()) coordinates(place) else place.address

    /** "31.23040, 121.47370". */
    fun coordinates(place: SavedPlace): String =
        String.format(Locale.US, "%.5f, %.5f", place.latitude, place.longitude)

    enum class SearchOutcome { Found, Nothing, Failed }

    /** What the location section's footer says after a search for [query]; null when it found something. */
    fun searchNote(outcome: SearchOutcome, query: String, nothing: (String) -> String, failed: String): String? =
        when (outcome) {
            SearchOutcome.Found -> null
            SearchOutcome.Nothing -> nothing(query)
            SearchOutcome.Failed -> failed
        }
}
