package persona.feature.assistant.settings

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.Address
import android.location.Geocoder
import android.location.Location
import android.location.LocationManager
import android.os.Build
import androidx.core.content.ContextCompat
import androidx.core.location.LocationManagerCompat
import androidx.core.os.CancellationSignal
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.runInterruptible
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeoutOrNull
import java.io.IOException
import java.util.Locale
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/** One hit from a forward geocode (address search). */
internal data class SavedPlaceSearchHit(
    val name: String,
    val address: String,
    val latitude: Double,
    val longitude: Double,
)

/**
 * Location helpers for Saved Places (#9293): current fix and Geocoder search, using only the Android SDK
 * (same approach as `platform/phonetools` LocationTool; feature/assistant must not depend on that module).
 */
internal object SavedPlaceLocation {
    fun hasLocationPermission(context: Context): Boolean {
        val fine = ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION)
        val coarse = ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_COARSE_LOCATION)
        return fine == PackageManager.PERMISSION_GRANTED || coarse == PackageManager.PERMISSION_GRANTED
    }

    val locationPermissions: Array<String> = arrayOf(
        Manifest.permission.ACCESS_COARSE_LOCATION,
        Manifest.permission.ACCESS_FINE_LOCATION,
    )

    /**
     * A current fix, or null when permission is missing, services are off, nothing arrived in time, or the
     * last-known fix is older than [LAST_KNOWN_MAX_AGE_MILLIS] (#9293 rework R5).
     */
    @Suppress("MissingPermission")
    suspend fun currentFix(context: Context): Location? {
        if (!hasLocationPermission(context)) return null
        val manager = context.getSystemService(LocationManager::class.java) ?: return null
        if (!manager.isLocationEnabled) return null
        val enabled = manager.getProviders(true)
        val providers = listOf("fused", LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER)
            .filter { it in enabled }
        val fresh = if (providers.isEmpty()) null else withTimeoutOrNull(FIX_TIMEOUT_MILLIS) {
            coroutineScope {
                val results = Channel<Location?>(providers.size)
                val jobs = providers.map { provider ->
                    launch {
                        val fix = try { locate(context, manager, provider) }
                        catch (cancelled: CancellationException) { throw cancelled }
                        catch (_: Exception) { null }
                        results.send(fix)
                    }
                }
                try {
                    var fix: Location? = null
                    repeat(providers.size) { if (fix == null) fix = results.receive() }
                    fix
                } finally {
                    jobs.forEach { it.cancel() }
                    results.close()
                }
            }
        }
        if (fresh != null) return fresh
        val last = enabled.mapNotNull { runCatching { manager.getLastKnownLocation(it) }.getOrNull() }
            .maxByOrNull { it.time } ?: return null
        val age = System.currentTimeMillis() - last.time
        return last.takeIf { age in 0..LAST_KNOWN_MAX_AGE_MILLIS }
    }

    suspend fun reverseGeocode(context: Context, latitude: Double, longitude: Double): String {
        if (!Geocoder.isPresent()) return ""
        val place = withTimeoutOrNull(GEOCODE_TIMEOUT_MILLIS) {
            lookup(context, latitude, longitude)
        } ?: return ""
        return formatAddress(place)
    }

    /**
     * Forward geocode. Returns null when the device has no Geocoder; empty list when nothing matched;
     * throws on Geocoder errors and timeouts so the caller shows the failure text (#9293 rework R4).
     */
    suspend fun forwardGeocode(context: Context, query: String, max: Int = 5): List<SavedPlaceSearchHit>? {
        if (!Geocoder.isPresent()) return null
        val trimmed = query.trim()
        if (trimmed.isEmpty()) return emptyList()
        val places = withTimeoutOrNull(GEOCODE_TIMEOUT_MILLIS) {
            lookupName(context, trimmed, max)
        } ?: throw IOException("Geocoder timed out")
        return places.map { place ->
            SavedPlaceSearchHit(
                name = place.featureName?.takeIf { it.isNotBlank() } ?: trimmed,
                address = formatAddress(place),
                latitude = place.latitude,
                longitude = place.longitude,
            )
        }
    }

    private suspend fun lookup(context: Context, latitude: Double, longitude: Double): Address? {
        val geocoder = Geocoder(context, Locale.getDefault())
        return if (Build.VERSION.SDK_INT >= 33) suspendCancellableCoroutine { continuation ->
            geocoder.getFromLocation(latitude, longitude, 1, object : Geocoder.GeocodeListener {
                override fun onGeocode(addresses: MutableList<Address>) {
                    if (continuation.isActive) continuation.resume(addresses.firstOrNull())
                }
                override fun onError(errorMessage: String?) {
                    if (continuation.isActive) continuation.resume(null)
                }
            })
        } else runInterruptible(Dispatchers.IO) {
            @Suppress("DEPRECATION") geocoder.getFromLocation(latitude, longitude, 1)?.firstOrNull()
        }
    }

    private suspend fun lookupName(context: Context, query: String, max: Int): List<Address> {
        val geocoder = Geocoder(context, Locale.getDefault())
        return if (Build.VERSION.SDK_INT >= 33) suspendCancellableCoroutine { continuation ->
            geocoder.getFromLocationName(query, max, object : Geocoder.GeocodeListener {
                override fun onGeocode(addresses: MutableList<Address>) {
                    if (continuation.isActive) continuation.resume(addresses.toList())
                }
                override fun onError(errorMessage: String?) {
                    if (continuation.isActive) {
                        continuation.resumeWithException(
                            IOException(errorMessage?.takeIf { it.isNotBlank() } ?: "Geocoder failed"),
                        )
                    }
                }
            })
        } else runInterruptible(Dispatchers.IO) {
            @Suppress("DEPRECATION") geocoder.getFromLocationName(query, max).orEmpty()
        }
    }

    private suspend fun locate(context: Context, manager: LocationManager, provider: String): Location? =
        suspendCancellableCoroutine { continuation ->
            val cancellation = CancellationSignal()
            continuation.invokeOnCancellation { cancellation.cancel() }
            @Suppress("MissingPermission")
            LocationManagerCompat.getCurrentLocation(manager, provider, cancellation, context.mainExecutor) { location ->
                if (continuation.isActive) continuation.resume(location)
            }
        }

    /** Prefer the first address line; else join parts the way Chinese addresses read best without separators. */
    private fun formatAddress(place: Address): String {
        place.getAddressLine(0)?.takeIf { it.isNotBlank() }?.let { return it }
        val parts = listOf(
            place.countryName, place.adminArea, place.locality, place.subLocality,
            place.thoroughfare, place.subThoroughfare,
        ).filterNot { it.isNullOrBlank() }
        return parts.joinToString("").ifEmpty { place.featureName.orEmpty() }
    }

    private const val FIX_TIMEOUT_MILLIS = 15_000L
    private const val GEOCODE_TIMEOUT_MILLIS = 10_000L
    private const val LAST_KNOWN_MAX_AGE_MILLIS = 2 * 60 * 1000L
}
