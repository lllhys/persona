package persona.platform.phonetools

import android.content.Context
import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.aggregate.AggregateMetric
import androidx.health.connect.client.aggregate.AggregationResult
import androidx.health.connect.client.permission.HealthPermission
import androidx.health.connect.client.records.ActiveCaloriesBurnedRecord
import androidx.health.connect.client.records.DistanceRecord
import androidx.health.connect.client.records.ElevationGainedRecord
import androidx.health.connect.client.records.ExerciseSegment
import androidx.health.connect.client.records.ExerciseSessionRecord
import androidx.health.connect.client.records.HeartRateRecord
import androidx.health.connect.client.records.Record
import androidx.health.connect.client.request.AggregateRequest
import androidx.health.connect.client.request.ReadRecordsRequest
import androidx.health.connect.client.time.TimeRangeFilter
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import persona.core.contracts.api.PhoneToolResult
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import kotlin.reflect.KClass

/**
 * `phone_workouts_recent` and `phone_workout_detail` (iOS `WorkoutTool`) on Health Connect: exercise sessions, read
 * only. Each workout's numbers come from the app that saved the session (its data origin), as iOS reads a workout's
 * own samples, so another app's distance over the same hours does not add to them. No route or location is read.
 */
object Workouts {
    private const val HIIT = ExerciseSessionRecord.EXERCISE_TYPE_HIGH_INTENSITY_INTERVAL_TRAINING

    /** The schema's activity names (lowercase, `_` for spaces) and the Health Connect exercise types each covers. */
    val activities: Map<String, List<Int>> = linkedMapOf(
        "cycling" to listOf(ExerciseSessionRecord.EXERCISE_TYPE_BIKING, ExerciseSessionRecord.EXERCISE_TYPE_BIKING_STATIONARY),
        "hiking" to listOf(ExerciseSessionRecord.EXERCISE_TYPE_HIKING),
        "interval_training" to listOf(HIIT),
        "running" to listOf(ExerciseSessionRecord.EXERCISE_TYPE_RUNNING, ExerciseSessionRecord.EXERCISE_TYPE_RUNNING_TREADMILL),
        "strength_training" to listOf(ExerciseSessionRecord.EXERCISE_TYPE_STRENGTH_TRAINING, ExerciseSessionRecord.EXERCISE_TYPE_WEIGHTLIFTING),
        "swimming" to listOf(ExerciseSessionRecord.EXERCISE_TYPE_SWIMMING_POOL, ExerciseSessionRecord.EXERCISE_TYPE_SWIMMING_OPEN_WATER),
        "walking" to listOf(ExerciseSessionRecord.EXERCISE_TYPE_WALKING),
        "yoga" to listOf(ExerciseSessionRecord.EXERCISE_TYPE_YOGA),
    )

    /** The read permissions workouts add to the Health summary's (Settings › Phone data › Health asks for them). */
    val permissions: Set<String> = setOf(ExerciseSessionRecord::class, ElevationGainedRecord::class)
        .map { HealthPermission.getReadPermission(it) }.toSet()

    private val exercisePermission = HealthPermission.getReadPermission(ExerciseSessionRecord::class)
    private val distancePermission = HealthPermission.getReadPermission(DistanceRecord::class)
    private val heartRatePermission = HealthPermission.getReadPermission(HeartRateRecord::class)
    private val caloriesPermission = HealthPermission.getReadPermission(ActiveCaloriesBurnedRecord::class)
    private val elevationPermission = HealthPermission.getReadPermission(ElevationGainedRecord::class)

    /** iOS `WorkoutReader.activityName`: the words a report and the tools use. */
    private fun activityName(type: Int): String = when (type) {
        ExerciseSessionRecord.EXERCISE_TYPE_RUNNING, ExerciseSessionRecord.EXERCISE_TYPE_RUNNING_TREADMILL -> "Running"
        ExerciseSessionRecord.EXERCISE_TYPE_WALKING -> "Walking"
        ExerciseSessionRecord.EXERCISE_TYPE_BIKING, ExerciseSessionRecord.EXERCISE_TYPE_BIKING_STATIONARY -> "Cycling"
        ExerciseSessionRecord.EXERCISE_TYPE_SWIMMING_POOL, ExerciseSessionRecord.EXERCISE_TYPE_SWIMMING_OPEN_WATER -> "Swimming"
        ExerciseSessionRecord.EXERCISE_TYPE_HIKING -> "Hiking"
        ExerciseSessionRecord.EXERCISE_TYPE_YOGA -> "Yoga"
        ExerciseSessionRecord.EXERCISE_TYPE_STRENGTH_TRAINING, ExerciseSessionRecord.EXERCISE_TYPE_WEIGHTLIFTING -> "Strength training"
        HIIT -> "Interval training"
        else -> "Workout (Health Connect exercise type $type)"  // i18n-ignore: tool result for the model
    }

    /** The activity key a session's type belongs to, or null for the types the schema does not filter by. */
    private fun activityKey(type: Int): String? = activities.entries.firstOrNull { type in it.value }?.key

    /** What the phone granted, read once per call: a refused type is treated as no data, never as an error. */
    private class Access(val client: HealthConnectClient, private val granted: Set<String>) {
        fun can(permission: String) = permission in granted
    }

    /** Health Connect missing or outdated, or the exercise permission not granted, is a tool error. */
    private suspend fun access(c: Context): Access {
        Health.modelReason(c)?.let {
            unavailable("$it，读不了运动数据。\nHealth Connect is not available on this Android phone, so there are no workouts to read.")  // i18n-ignore: tool result for the model
        }
        val client = HealthConnectClient.getOrCreate(c)
        val granted = client.permissionController.getGrantedPermissions()
        if (exercisePermission !in granted) {
            unavailable("没有权限：健康 / Permission denied: health.\n" + PhoneToolText.grantHint(PhoneSource.Health))  // i18n-ignore: tool result for the model
        }
        return Access(client, granted)
    }

    suspend fun recent(c: Context, args: JsonObject): PhoneToolResult {
        val access = access(c)
        val types = args.raw("activity")?.trim()?.takeIf { it.isNotEmpty() }?.let { name ->
            val key = name.lowercase().replace(' ', '_')
            activities[key] ?: invalid("unknown activity \"$name\"; use one of ${activities.keys.sorted().joinToString(", ")}")
        }
        val days = args.int("days", 30, 1, 365)
        val limit = args.int("limit", 20, 1, 50)
        val now = Instant.now()
        // Calendar days back from now, as iOS does; a DST change does not shift the window by an hour.
        val since = now.atZone(ZoneId.systemDefault()).minusDays(days.toLong()).toInstant()
        val sessions = access.client.readRecords(
            ReadRecordsRequest(ExerciseSessionRecord::class, TimeRangeFilter.between(since, now), ascendingOrder = false, pageSize = 500),
        ).records
        val rows = sessions
            .filter { types == null || it.exerciseType in types }
            .take(limit)
            .map { row(c, access, it) }
        return PhoneToolResult.text(buildJsonObject {
            put("workouts", buildJsonArray { rows.forEach { add(it) } })
        }.toString())
    }

    suspend fun detail(c: Context, args: JsonObject): PhoneToolResult {
        val access = access(c)
        val id = args.raw("id")?.trim()?.takeIf { it.isNotEmpty() } ?: invalid("id must be a workout's id from phone_workouts_recent")
        val session = try {
            access.client.readRecord(ExerciseSessionRecord::class, id).record
        } catch (e: CancellationException) {
            throw e
        } catch (e: SecurityException) {
            throw e  // a refused read is not a missing workout
        } catch (e: Exception) {
            unavailable("Workout $id could not be read from Health Connect (it may be deleted, or Persona may not read it).")
        }
        val fields = row(c, access, session).toMutableMap()
        // A transient read failure here only drops these fields, as totals do; a refused read is still reported.
        val splits = try { splits(access, session) } catch (e: CancellationException) { throw e }
            catch (e: SecurityException) { throw e } catch (e: Exception) { emptyList() }
        if (splits.isNotEmpty()) {
            fields["splits"] = buildJsonArray {
                splits.forEach { add(buildJsonObject { put("km", it.km); put("seconds", it.seconds) }) }
            }
        }
        val rates = try { heartRates(access, session) } catch (e: CancellationException) { throw e }
            catch (e: SecurityException) { throw e } catch (e: Exception) { emptyList() }
        if (rates.isNotEmpty()) {
            fields["heart_rate"] = buildJsonArray {
                rates.forEach { add(buildJsonObject { put("minute", it.first); put("bpm", it.second) }) }
            }
        }
        return PhoneToolResult.text(JsonObject(fields).toString())
    }

    /** One session's row; fields without data are left out. */
    private suspend fun row(c: Context, access: Access, s: ExerciseSessionRecord): JsonObject {
        val totals = totals(access, s)
        val elapsed = Duration.between(s.startTime, s.endTime).seconds.toDouble()
        val distance = totals?.get(DistanceRecord.DISTANCE_TOTAL)?.inMeters
        val key = activityKey(s.exerciseType)
        // Pace counts the time moving, not the pauses (iOS pace is per kilometre of the workout's active time).
        val active = WorkoutMath.active(s.startTime, s.endTime, pauses(s))
        val pace = WorkoutMath.pace(distance, active, key)
        val energy = totals?.get(ActiveCaloriesBurnedRecord.ACTIVE_CALORIES_TOTAL)?.inKilocalories
        val gain = WorkoutMath.elevation(totals?.get(ElevationGainedRecord.ELEVATION_GAINED_TOTAL)?.inMeters)
        val rate = WorkoutMath.heartRatePair(totals?.get(HeartRateRecord.BPM_AVG)?.toDouble(), totals?.get(HeartRateRecord.BPM_MAX)?.toDouble())
        val out = linkedMapOf<String, JsonElement>(
            "id" to JsonPrimitive(s.metadata.id),
            "activity" to JsonPrimitive(activityName(s.exerciseType)),
            "source" to JsonPrimitive(sourceName(c, s.metadata.dataOrigin.packageName)),
            "start" to JsonPrimitive(iso(s.startTime)),
            "end" to JsonPrimitive(iso(s.endTime)),
            "duration" to JsonPrimitive(elapsed),
        )
        distance?.let { out["distance"] = JsonPrimitive(Math.round(it).toDouble()) }
        energy?.let { out["energy"] = JsonPrimitive(Math.round(it).toDouble()) }
        gain?.let { out["elevation_gain"] = JsonPrimitive(it) }
        rate?.let {
            out["heart_rate_avg"] = JsonPrimitive(it.first)
            out["heart_rate_max"] = JsonPrimitive(it.second)
        }
        pace?.let { out["pace"] = JsonPrimitive(it) }
        return JsonObject(out)
    }

    private fun pauses(s: ExerciseSessionRecord): List<Pair<Instant, Instant>> = s.segments
        .filter { it.segmentType == ExerciseSegment.EXERCISE_SEGMENT_TYPE_PAUSE }
        .map { it.startTime to it.endTime }

    private fun sourceName(c: Context, packageName: String): String = runCatching {
        val pm = c.packageManager
        pm.getApplicationLabel(pm.getApplicationInfo(packageName, 0)).toString()
    }.getOrDefault(packageName)

    /**
     * Totals over the session's window, from the session's own data origin, for the metrics the phone may read.
     * Null when none can be read or the aggregate fails; the fields are then left out.
     */
    private suspend fun totals(access: Access, s: ExerciseSessionRecord): AggregationResult? {
        val metrics = buildSet<AggregateMetric<*>> {
            if (access.can(distancePermission)) add(DistanceRecord.DISTANCE_TOTAL)
            if (access.can(caloriesPermission)) add(ActiveCaloriesBurnedRecord.ACTIVE_CALORIES_TOTAL)
            if (access.can(elevationPermission)) add(ElevationGainedRecord.ELEVATION_GAINED_TOTAL)
            if (access.can(heartRatePermission)) {
                add(HeartRateRecord.BPM_AVG)
                add(HeartRateRecord.BPM_MAX)
            }
        }
        if (metrics.isEmpty()) return null
        return try {
            access.client.aggregate(
                AggregateRequest(metrics, TimeRangeFilter.between(s.startTime, s.endTime), setOf(s.metadata.dataOrigin)),
            )
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            null
        }
    }

    /** Every record of [type] in the session's window from its own data origin, following Health Connect's pages. */
    private suspend fun <T : Record> readAll(access: Access, type: KClass<T>, s: ExerciseSessionRecord): List<T> {
        val out = mutableListOf<T>()
        var token: String? = null
        do {
            val page = access.client.readRecords(
                ReadRecordsRequest(
                    type, TimeRangeFilter.between(s.startTime, s.endTime), setOf(s.metadata.dataOrigin),
                    ascendingOrder = true, pageSize = 1000, pageToken = token,
                ),
            )
            out += page.records
            token = page.pageToken
        } while (token != null)
        return out
    }

    /** Whole kilometres of a run, walk, hike or ride, pauses left out. Needs the distance permission. */
    private suspend fun splits(access: Access, s: ExerciseSessionRecord): List<WorkoutMath.Split> {
        if (activityKey(s.exerciseType) !in WorkoutMath.byTheKilometre || !access.can(distancePermission)) return emptyList()
        val samples = readAll(access, DistanceRecord::class, s).map {
            WorkoutMath.Sample(it.startTime, it.endTime, it.distance.inMeters)
        }
        return WorkoutMath.splits(samples, s.startTime, s.endTime, pauses(s))
    }

    /** The average bpm of each minute from the start, as (minute, bpm). Needs the heart-rate permission. */
    private suspend fun heartRates(access: Access, s: ExerciseSessionRecord): List<Pair<Int, Double>> {
        if (!access.can(heartRatePermission)) return emptyList()
        val samples = readAll(access, HeartRateRecord::class, s).flatMap { record ->
            record.samples.map { it.time to it.beatsPerMinute.toDouble() }
        }
        return WorkoutMath.minuteRates(s.startTime, s.endTime, samples)
    }
}

/**
 * The workout arithmetic, pure so it can be tested without Health Connect. The splits are a port of iOS
 * `WorkoutSplits`; the rest follows the iOS rules the tool descriptions promise.
 */
internal object WorkoutMath {
    data class Sample(val start: Instant, val end: Instant, val meters: Double)
    data class Split(val km: Int, val seconds: Double)

    /** At most this many: the hub refuses more. */
    const val LIMIT = 1000

    /** Activities whose distance is counted in whole kilometres (iOS `WorkoutReader.distanceType`). */
    val byTheKilometre = setOf("running", "walking", "hiking", "cycling")

    /**
     * Whole kilometres in order; the last part kilometre is left out. A sample that cannot give a sensible split (no
     * time, or outside the workout) leaves no splits at all: a wrong pace is worse than none.
     */
    fun splits(samples: List<Sample>, start: Instant, end: Instant, paused: List<Pair<Instant, Instant>>): List<Split> {
        val out = mutableListOf<Split>()
        var covered = 0.0
        var mark = start
        for (s in samples.sortedBy { it.start }) {
            if (s.meters <= 0) continue
            if (s.start < start || s.end < s.start || s.end > end) return emptyList()
            val before = covered
            covered += s.meters
            while (out.size < LIMIT && covered >= (out.size + 1) * 1000.0) {
                val share = ((out.size + 1) * 1000.0 - before) / s.meters
                val span = Duration.between(s.start, s.end).toNanos()
                val at = maxOf(mark, s.start.plusNanos((span * share).toLong()))
                val seconds = Math.round(active(mark, at, paused) * 10) / 10.0
                if (seconds < 1) return emptyList()
                out += Split(out.size + 1, seconds)
                mark = at
            }
        }
        return out
    }

    /** Seconds from [a] to [b] outside the pauses. */
    fun active(a: Instant, b: Instant, paused: List<Pair<Instant, Instant>>): Double {
        if (b <= a) return 0.0
        var time = Duration.between(a, b).toNanos() / 1e9
        // Overlapping pauses are merged first, so time they share is taken off once.
        for ((ps, pe) in mergedPauses(paused)) {
            val lo = maxOf(a, ps)
            val hi = minOf(b, pe)
            if (hi > lo) time -= Duration.between(lo, hi).toNanos() / 1e9
        }
        return maxOf(time, 0.0)
    }

    private fun mergedPauses(paused: List<Pair<Instant, Instant>>): List<Pair<Instant, Instant>> {
        val out = mutableListOf<Pair<Instant, Instant>>()
        for ((start, end) in paused.filter { it.second > it.first }.sortedBy { it.first }) {
            val last = out.lastOrNull()
            if (last != null && start <= last.second) {
                if (end > last.second) out[out.size - 1] = last.first to end
            } else out += start to end
        }
        return out
    }

    /** Seconds per kilometre over the active time, for the activities that have one, from at least 500 m. */
    fun pace(distanceMeters: Double?, activeSeconds: Double, activityKey: String?): Double? =
        if (distanceMeters != null && distanceMeters >= 500 && activityKey in byTheKilometre) {
            Math.round(activeSeconds / (distanceMeters / 1000)).toDouble()
        } else null

    /** Meters climbed, when plausible (0 to 20 000 m), as iOS keeps it. */
    fun elevation(meters: Double?): Double? =
        meters?.takeIf { it.isFinite() && it in 0.0..20000.0 }?.let { Math.round(it).toDouble() }

    /** Average and maximum bpm together, only when they make sense as a pair (iOS `WorkoutReader.heartRate`). */
    fun heartRatePair(average: Double?, maximum: Double?): Pair<Double, Double>? =
        if (average != null && maximum != null && average > 0 && maximum <= 300 && average <= maximum) {
            Math.round(average).toDouble() to Math.round(maximum).toDouble()
        } else null

    /** The average bpm of each minute from [start], for samples inside the workout with a sensible bpm. */
    fun minuteRates(start: Instant, end: Instant, samples: List<Pair<Instant, Double>>): List<Pair<Int, Double>> {
        val last = Duration.between(start, end).toMinutes().toInt()
        val buckets = sortedMapOf<Int, MutableList<Double>>()
        for ((time, bpm) in samples) {
            val minute = Math.floor(Duration.between(start, time).toMillis() / 60000.0).toInt()
            if (minute in 0..last && bpm > 0 && bpm <= 300) buckets.getOrPut(minute) { mutableListOf() }.add(bpm)
        }
        return buckets.map { (minute, values) -> minute to Math.round(values.average()).toDouble() }
    }
}
