package persona.platform.phonetools

import androidx.health.connect.client.records.ExerciseSessionRecord
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class WorkoutMathTest {
    private val t0: Instant = Instant.parse("2026-11-15T06:00:00Z")
    private fun at(seconds: Long): Instant = t0.plusSeconds(seconds)

    /** Each 500 m sample takes 150 s, so a kilometre is two samples = 300 s. */
    private fun steady(count: Int): List<WorkoutMath.Sample> = (0 until count).map { i ->
        WorkoutMath.Sample(at(i * 150L), at((i + 1) * 150L), 500.0)
    }

    @Test fun wholeKilometresOnlyAndEachTakesItsOwnSeconds() {
        // Five samples = 2.5 km: two whole kilometres, the half kilometre is left out.
        val splits = WorkoutMath.splits(steady(5), at(0), at(750), emptyList())
        assertEquals(listOf(1 to 300.0, 2 to 300.0), splits.map { it.km to it.seconds })
    }

    @Test fun pausesAreLeftOutOfTheSeconds() {
        // A 60 s pause inside the first kilometre: that split takes 240 s of active time.
        val splits = WorkoutMath.splits(steady(2), at(0), at(300), listOf(at(100) to at(160)))
        assertEquals(240.0, splits.single().seconds, 0.0)
    }

    @Test fun aSampleOutsideTheWorkoutGivesNoSplitsAtAll() {
        val outside = steady(2) + WorkoutMath.Sample(at(300), at(900), 500.0)
        assertTrue(WorkoutMath.splits(outside, at(0), at(600), emptyList()).isEmpty())
    }

    @Test fun aSplitShorterThanOneSecondGivesNoSplitsAtAll() {
        // 1 km covered in 0.5 s: a wrong pace is worse than none.
        val blink = listOf(WorkoutMath.Sample(at(0), at(0).plusMillis(500), 1000.0))
        assertTrue(WorkoutMath.splits(blink, at(0), at(1), emptyList()).isEmpty())
    }

    @Test fun activeTimeIgnoresPausesOutsideTheInterval() {
        val pause = listOf(at(0) to at(10))
        assertEquals(20.0, WorkoutMath.active(at(5), at(30), pause), 0.0)
        assertEquals(0.0, WorkoutMath.active(at(30), at(5), pause), 0.0)
    }

    @Test fun overlappingPausesAreTakenOffOnce() {
        // 0–300 s window; pauses 100–200 and 150–250 merge to 100–250 (150 s), so 150 s are active.
        assertEquals(150.0, WorkoutMath.active(at(0), at(300), listOf(at(100) to at(200), at(150) to at(250))), 0.0)
    }

    @Test fun paceNeedsAtLeast500MetresAndAKilometreActivity() {
        assertEquals(300.0, WorkoutMath.pace(1000.0, 300.0, "running")!!, 0.0)
        assertNull(WorkoutMath.pace(499.0, 300.0, "running"))
        assertNull(WorkoutMath.pace(1000.0, 300.0, "swimming"))
        assertNull(WorkoutMath.pace(null, 300.0, "running"))
    }

    @Test fun paceUsesActiveTimeNotElapsed() {
        // 5 km in 1500 s of moving time: 300 s/km, whatever the pause did to the elapsed time.
        assertEquals(300.0, WorkoutMath.pace(5000.0, 1500.0, "running")!!, 0.0)
    }

    @Test fun heartRatePairOnlyWhenItMakesSense() {
        assertEquals(140.0 to 170.0, WorkoutMath.heartRatePair(140.0, 170.0))
        assertNull(WorkoutMath.heartRatePair(180.0, 170.0))   // average above maximum
        assertNull(WorkoutMath.heartRatePair(140.0, 310.0))   // over 300 bpm
        assertNull(WorkoutMath.heartRatePair(0.0, 100.0))
        assertNull(WorkoutMath.heartRatePair(null, 100.0))
    }

    @Test fun elevationKeepsOnlyPlausibleGains() {
        assertEquals(120.0, WorkoutMath.elevation(119.6)!!, 0.0)
        assertNull(WorkoutMath.elevation(-1.0))
        assertNull(WorkoutMath.elevation(20001.0))
    }

    @Test fun minuteRatesAverageEachMinuteAndDropBadSamples() {
        val samples = listOf(
            at(10) to 100.0, at(50) to 120.0,   // minute 0: average 110
            at(70) to 130.0,                    // minute 1
            at(75) to 0.0, at(80) to 400.0,     // dropped: no bpm
            at(-5) to 90.0,                     // dropped: before the start
        )
        val rates = WorkoutMath.minuteRates(at(0), at(120), samples)
        assertEquals(listOf(0 to 110.0, 1 to 130.0), rates)
    }

    @Test fun activityNamesMapToTheSchemaKeysTheFixtureLists() {
        assertTrue(Workouts.activities.getValue("strength_training").contains(ExerciseSessionRecord.EXERCISE_TYPE_WEIGHTLIFTING))
        assertEquals(
            listOf("cycling", "hiking", "interval_training", "running", "strength_training", "swimming", "walking", "yoga"),
            Workouts.activities.keys.sorted(),
        )
    }

    @Test fun workoutPermissionsAreTheExerciseAndElevationReads() {
        assertEquals(
            setOf("android.permission.health.READ_EXERCISE", "android.permission.health.READ_ELEVATION_GAINED"),
            Workouts.permissions,
        )
        assertTrue(Health.permissions.containsAll(Workouts.permissions))
    }
}
