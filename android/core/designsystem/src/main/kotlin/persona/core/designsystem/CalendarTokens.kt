package persona.core.designsystem

import androidx.compose.ui.unit.dp

/** The to-dos' month grid (#2046), the day's and the week's time axis (#4200), and a to-do dragged on them (#2045); iOS `Theme.CalendarGrid`. */
object CalendarTokens {
    /** An hour on the time axis: a to-do's block ([block] high) takes most of one, so two an hour apart never overlap. */
    val hour = 52.dp
    /** A to-do's block on the axis: a touch target high, whatever its title. */
    val block = 44.dp
    /** The column of hours at the start of a day: "09:00", "11:00 AM" and the now line's "2:20 PM" fit. */
    val gutter = 60.dp
    /** The same on a phone's week, whose seven columns need the room: the hours alone, and the moment's "14:20". */
    val gutterNarrow = 40.dp
    /** The width from which the week's blocks show their time and state; narrower, a block is its title alone. */
    val wide = 600.dp
    /** The All Day row, at the least: room to tap for a new all-day to-do. */
    val allDayRow = 28.dp
    /** The circle behind a date in the day view's week strip and the week's row of days. */
    val stripMark = 32.dp
    /** A to-do's dot under a date of the week strip. */
    val stripDot = 6.dp
    /** The now line's dot. */
    val nowDot = 8.dp
    /** Dots under a date of the week strip: at most this many. */
    const val stripDots = 3
    /** How far the hours' column and the dates' circles on the axis grow with the text size, at the most. */
    const val textScaleMaximum = 1.6f
    /** Lines a day of a phone's week shows in its All Day row: its to-dos, or one fewer and "+N". */
    const val allDayLines = 2
    /** A day of the month grid, at the least: its date and three lines. */
    val monthCell = 92.dp
    /** The circle behind today's date. */
    val dayMark = 22.dp
    /** The state edge on a day's to-do. */
    val edge = 2.dp
    /** A day's to-do's corners. */
    val chipRadius = 3.dp
    /** A to-do's state dot in the day's list. */
    val dot = 8.dp
    /** How far a sideways swipe on the grid goes before it turns the month. */
    val pageSwipe = 48.dp
    /** The outline of the day a dragged to-do would land on (#2045). */
    val dropRing = 2.dp
    /** The outline of a carried to-do (in the accent) and of where it would land. */
    val liftLine = 1.dp
    /** How near the top or bottom of a month taller than its space a dragged to-do scrolls it. */
    val dragEdge = 48.dp
    /** How far that scroll goes each frame. */
    val dragScroll = 6.dp
    /** Lines a day of the month shows: its to-dos, or one fewer and "+N". */
    const val monthLines = 3
    /** A done to-do, faded. */
    const val doneFade = 0.5f
    /** A day that belongs to the month before or after. */
    const val outsideFade = 0.45f
    /** A dragged to-do where it was, while it is carried (#2045). */
    const val liftedFade = 0.35f
}
