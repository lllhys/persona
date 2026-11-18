package persona.core.domain

import persona.core.contracts.CoreText
import persona.core.contracts.CoreText.Key

/**
 * A time as a work line writes it (iOS `WorkLine.duration`): "39 s", "2 min", "1m 20s", "1 hr 5 min". The work line, a
 * subagent's card and a ScheduleWakeup's delay say their time this way.
 */
fun workTime(seconds: Long): CoreText {
    val s = seconds.coerceAtLeast(0)
    return when {
        s < 60 -> CoreText.of(Key.WorkTimeSeconds, s)
        s < 3600 -> if (s % 60 == 0L) CoreText.of(Key.WorkTimeMinutes, s / 60) else CoreText.of(Key.WorkTimeMinutesSeconds, s / 60, s % 60)
        else -> CoreText.of(Key.WorkTimeHoursMinutes, s / 3600, s % 3600 / 60)
    }
}
