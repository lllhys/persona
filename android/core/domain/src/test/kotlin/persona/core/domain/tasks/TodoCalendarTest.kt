package persona.core.domain.tasks

import java.time.DayOfWeek
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.YearMonth
import java.time.ZoneId
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import persona.core.contracts.TodoDue
import persona.core.contracts.TodoState

// The month's rules (#2046) and a drop on it (#2045), the cases of web/src/lib/todo-calendar.test.ts that the month needs.
class TodoCalendarTest {
    private val zone = ZoneId.of("Asia/Shanghai")
    private val now = Instant.parse("2026-11-05T09:30:00Z") // 17:30 on Nov 5 in zone
    private val none: (SessionKey) -> Session? = { null }

    private fun entry(todo: Todo) = todo.calendarEntry(none, now, zone)

    @Test fun aMomentIsOnItsLocalDayAtItsTime() {
        // 23:30 UTC on Nov 11 is 07:30 on Nov 12 here
        val e = entry(Todo("a", title = "A", dueAt = "2026-11-11T23:30:00Z"))!!
        assertEquals(LocalDate.of(2026, 11, 12), e.day)
        assertEquals(LocalTime.of(7, 30), e.time)
        assertFalse(e.allDay)
        assertFalse(e.overdue)
    }

    @Test fun aDayIsAllDayAndWithoutEitherNothing() {
        val e = entry(Todo("a", dueDate = "2026-11-12"))!!
        assertEquals(LocalDate.of(2026, 11, 12), e.day)
        assertTrue(e.allDay)
        assertNull(entry(Todo("b")))
        assertNull(entry(Todo("c", dueDate = "next week")))
    }

    @Test fun marksFollowTheList() {
        val late = entry(Todo("a", dueAt = "2026-11-05T08:00:00Z", priority = "high", remindAt = "2026-11-06T01:00:00Z"))!!
        assertTrue(late.overdue)
        assertTrue(late.high)
        assertTrue(late.reminding)
        assertEquals(TodoState.NotStarted, late.state)
        // done: never overdue, no flag, no bell
        val done = entry(Todo("b", dueAt = "2026-11-05T08:00:00Z", priority = "high", remindAt = "2026-11-06T01:00:00Z", doneAt = "2026-11-05T09:00:00Z"))!!
        assertTrue(done.done)
        assertFalse(done.overdue || done.high || done.reminding)
        assertEquals(TodoState.Done, done.state)
        // a reminder already pushed is no bell; an all-day to-do is late only once its day is over
        assertFalse(entry(Todo("c", dueDate = "2026-11-06", remindAt = "2026-11-06T01:00:00Z", remindedAt = "2026-11-06T01:00:01Z"))!!.reminding)
        assertFalse(entry(Todo("d", dueDate = "2026-11-05"))!!.overdue)
        assertTrue(entry(Todo("e", dueDate = "2026-11-04"))!!.overdue)
    }

    @Test fun theStateIsTheList() {
        val s = Session(machineId = "m", sessionId = "s", state = "needs_you")
        val e = Todo("a", dueDate = "2026-11-06", agents = listOf(TodoAgent("m", "s"))).calendarEntry({ if (it == s.key) s else null }, now, zone)!!
        assertEquals(TodoState.NeedsYou, e.state)
    }

    @Test fun entriesInRangeByDayInDrawOrder() {
        val todos = listOf(
            Todo("late", title = "B", dueAt = "2026-11-06T10:00:00Z"),
            Todo("early", title = "Z", dueAt = "2026-11-06T01:00:00Z"),
            Todo("allday", title = "Y", dueDate = "2026-11-06"),
            Todo("done", title = "A", dueAt = "2026-11-06T01:00:00Z", doneAt = "2026-11-04T00:00:00Z"),
            Todo("before", dueDate = "2026-10-31"),
            Todo("after", dueDate = "2026-12-01"),
            Todo("undated"),
        )
        val all = calendarEntries(todos, none, true, LocalDate.of(2026, 11, 1), LocalDate.of(2026, 12, 1), now, zone)
        assertEquals(setOf(LocalDate.of(2026, 11, 6)), all.keys)
        // all day first, then by time; at one time open before done
        assertEquals(listOf("allday", "early", "done", "late"), all.getValue(LocalDate.of(2026, 11, 6)).map { it.todoId })
        val open = calendarEntries(todos, none, false, LocalDate.of(2026, 11, 1), LocalDate.of(2026, 12, 1), now, zone)
        assertEquals(listOf("allday", "early", "late"), open.getValue(LocalDate.of(2026, 11, 6)).map { it.todoId })
    }

    @Test fun undatedCountsOpenOnesOnly() {
        assertEquals(1, undatedCount(listOf(Todo("a"), Todo("b", doneAt = "2026-11-01T00:00:00Z"), Todo("c", dueDate = "2026-11-06"),
            Todo("d", dueAt = "2026-11-06T01:00:00Z"))))
    }

    @Test fun aDayFitsItsLinesOrALineFewerAndMore() {
        assertEquals(DayFit(listOf(1, 2, 3), 0), fitDay(listOf(1, 2, 3), 3))
        assertEquals(DayFit(listOf(1, 2), 3), fitDay(listOf(1, 2, 3, 4, 5), 3))
        assertEquals(DayFit(emptyList<Int>(), 0), fitDay(emptyList<Int>(), 3))
    }

    @Test fun theMonthIsTheWholeWeeksItTouches() {
        // November 2026 starts on a Sunday and ends on a Monday
        val monday = monthDays(YearMonth.of(2026, 11), DayOfWeek.MONDAY)
        assertEquals(LocalDate.of(2026, 10, 26), monday.first())
        assertEquals(LocalDate.of(2026, 12, 6), monday.last())
        assertEquals(42, monday.size)
        val sunday = monthDays(YearMonth.of(2026, 11), DayOfWeek.SUNDAY)
        assertEquals(LocalDate.of(2026, 11, 1), sunday.first())
        assertEquals(LocalDate.of(2026, 12, 5), sunday.last())
        assertEquals(35, sunday.size)
        // February 2027 from Monday: exactly four weeks
        assertEquals(28, monthDays(YearMonth.of(2027, 2), DayOfWeek.MONDAY).size)
        assertEquals(listOf(DayOfWeek.SUNDAY, DayOfWeek.MONDAY), weekdays(DayOfWeek.SUNDAY).take(2))
        assertEquals(DayOfWeek.SATURDAY, weekdays(DayOfWeek.SUNDAY).last())
    }

    @Test fun weeksStartWhereTheRegionStartsThem() {
        assertEquals(DayOfWeek.MONDAY, weekStart(Locale.forLanguageTag("zh-CN")))
        assertEquals(DayOfWeek.MONDAY, weekStart(Locale.forLanguageTag("zh")))
        assertEquals(DayOfWeek.MONDAY, weekStart(Locale.forLanguageTag("zh-Hans")))
        assertEquals(DayOfWeek.SUNDAY, weekStart(Locale.forLanguageTag("zh-Hant")))
        assertEquals(DayOfWeek.SUNDAY, weekStart(Locale.forLanguageTag("en-US")))
        assertEquals(DayOfWeek.MONDAY, weekStart(Locale.forLanguageTag("en-GB")))
        assertEquals(DayOfWeek.SUNDAY, weekStart(Locale.forLanguageTag("ja")))
        assertEquals(DayOfWeek.MONDAY, weekStart(Locale.forLanguageTag("de-DE")))
        assertEquals(DayOfWeek.SATURDAY, weekStart(Locale.forLanguageTag("ar-EG")))
    }

    // #2045, as the web's dropOnDay / dropPatch
    @Test fun aDropOnAnotherDayKeepsTheTimeAndAllDayStaysAllDay() {
        // 18:00 on Nov 12 here
        val at = Todo("a", title = "A", dueAt = "2026-11-12T10:00:00Z")
        val moved = entry(at)!!.dropOnDay(LocalDate.of(2026, 11, 19), zone)
        assertEquals(TodoDue.At(Instant.parse("2026-11-19T10:00:00Z")), moved)
        assertEquals(moved, at.moveTo(moved))
        // seconds kept, as the web keeps them
        val odd = Todo("b", dueAt = "2026-11-12T10:00:30Z")
        assertEquals(TodoDue.At(Instant.parse("2026-11-03T10:00:30Z")), entry(odd)!!.dropOnDay(LocalDate.of(2026, 11, 3), zone))
        val day = Todo("c", dueDate = "2026-11-12")
        assertEquals(TodoDue.Day(LocalDate.of(2026, 12, 1)), entry(day)!!.dropOnDay(LocalDate.of(2026, 12, 1), zone))
        // done ones move too
        val done = Todo("d", dueDate = "2026-11-12", doneAt = "2026-11-04T10:00:00Z")
        assertEquals(TodoDue.Day(LocalDate.of(2026, 11, 13)), done.moveTo(entry(done)!!.dropOnDay(LocalDate.of(2026, 11, 13), zone)))
    }

    @Test fun aDropWhereItIsSendsNothing() {
        val at = Todo("a", dueAt = "2026-11-12T10:00:00Z")
        assertNull(at.moveTo(entry(at)!!.dropOnDay(LocalDate.of(2026, 11, 12), zone)))
        val day = Todo("b", dueDate = "2026-11-12")
        assertNull(day.moveTo(entry(day)!!.dropOnDay(LocalDate.of(2026, 11, 12), zone)))
        // the same day, but all day instead of at a time, is a change
        assertEquals(TodoDue.Day(LocalDate.of(2026, 11, 12)), at.moveTo(TodoDue.Day(LocalDate.of(2026, 11, 12))))
    }

    @Test fun aMovedTodoIsDueByTheOneFieldItWasMovedTo() {
        val at = Todo("a", dueDate = "2026-11-12").withDue(TodoDue.At(Instant.parse("2026-11-19T10:00:00Z")))
        assertEquals("2026-11-19T10:00:00Z", at.dueAt)
        assertNull(at.dueDate)
        val day = Todo("b", dueAt = "2026-11-12T10:00:00Z").withDue(TodoDue.Day(LocalDate.of(2026, 11, 19)))
        assertNull(day.dueAt)
        assertEquals("2026-11-19", day.dueDate)
        assertEquals(TodoDue.Day(LocalDate.of(2026, 11, 19)), day.due)
        assertNull(Todo("c").due)
    }

    @Test fun theDayUnderAPointOfTheMonth() {
        // 700 wide (100 a day), rows 90 high, six of them
        assertEquals(0, monthDropIndex(0f, 0f, 700f, 90f, 6))
        assertEquals(6, monthDropIndex(699f, 89f, 700f, 90f, 6))
        assertEquals(7 + 3, monthDropIndex(350f, 100f, 700f, 90f, 6))
        assertEquals(35 + 6, monthDropIndex(650f, 539f, 700f, 90f, 6))
        // right to left, the first day is at the right
        assertEquals(6, monthDropIndex(0f, 0f, 700f, 90f, 6, rtl = true))
        // off the grid
        assertNull(monthDropIndex(-1f, 10f, 700f, 90f, 6))
        assertNull(monthDropIndex(700f, 10f, 700f, 90f, 6))
        assertNull(monthDropIndex(10f, -1f, 700f, 90f, 6))
        assertNull(monthDropIndex(10f, 540f, 700f, 90f, 6))
    }

    @Test fun theMonthScrollsNearItsEdgesWhileDragging() {
        assertEquals(-1, edgeScroll(10f, 500f, 40f))
        assertEquals(0, edgeScroll(250f, 500f, 40f))
        assertEquals(1, edgeScroll(470f, 500f, 40f))
        // below the part in sight it keeps going down
        assertEquals(1, edgeScroll(600f, 500f, 40f))
    }

    // The day and the week (#4200), the cases of web/src/lib/todo-calendar.test.ts and iOS TodoCalendarTests they need.
    private fun d(s: String) = LocalDate.parse(s)
    private fun timed(id: String, at: String, day: String = "2026-11-12") =
        CalendarEntry(id, id, d(day), LocalTime.parse(at), TodoState.NotStarted, done = false, overdue = false, high = false, reminding = false)

    @Test fun eachModeShowsItsDaysFromTheWeeksFirstDay() {
        // Thursday, November 12 2026
        val anchor = d("2026-11-12")
        assertEquals(listOf(anchor), calendarDays(CalendarMode.Day, anchor, DayOfWeek.MONDAY))
        val monday = calendarDays(CalendarMode.Week, anchor, DayOfWeek.MONDAY)
        assertEquals(d("2026-11-09"), monday.first())
        assertEquals(d("2026-11-15"), monday.last())
        assertEquals(7, monday.size)
        // the same first day as the month
        val sunday = calendarDays(CalendarMode.Week, anchor, DayOfWeek.SUNDAY)
        assertEquals(d("2026-11-08"), sunday.first())
        assertEquals(sunday.first(), monthDays(YearMonth.of(2026, 11), DayOfWeek.SUNDAY)[7])
        assertEquals(monthDays(YearMonth.of(2026, 11), DayOfWeek.MONDAY), calendarDays(CalendarMode.Month, anchor, DayOfWeek.MONDAY))
        // a week's first day is its own week's
        assertEquals(monday, weekOf(d("2026-11-09"), DayOfWeek.MONDAY))
        assertEquals(monday, weekOf(d("2026-11-15"), DayOfWeek.MONDAY))
        // a week across the year
        assertEquals(listOf(d("2026-12-28"), d("2027-01-03")), weekOf(d("2027-01-01"), DayOfWeek.MONDAY).let { listOf(it.first(), it.last()) })
    }

    @Test fun aPageIsADayAWeekOrAMonth() {
        val anchor = d("2026-11-12")
        assertEquals(d("2026-11-13"), stepAnchor(anchor, CalendarMode.Day, 1))
        assertEquals(d("2026-11-11"), stepAnchor(anchor, CalendarMode.Day, -1))
        assertEquals(d("2026-11-19"), stepAnchor(anchor, CalendarMode.Week, 1))
        assertEquals(d("2026-11-05"), stepAnchor(anchor, CalendarMode.Week, -1))
        assertEquals(d("2026-12-12"), stepAnchor(anchor, CalendarMode.Month, 1))
        // a month keeps the day, the shorter month's last
        assertEquals(d("2027-02-28"), stepAnchor(d("2027-01-31"), CalendarMode.Month, 1))
        assertEquals(d("2026-11-30"), stepAnchor(d("2026-12-31"), CalendarMode.Month, -1))
        // across the year
        assertEquals(d("2027-01-01"), stepAnchor(d("2026-12-31"), CalendarMode.Day, 1))
        assertEquals(d("2027-01-07"), stepAnchor(d("2026-12-31"), CalendarMode.Week, 1))
    }

    @Test fun overlappingBlocksSitSideBySide() {
        // blocks 50 minutes long: 09:00 and 09:30 overlap, 09:50 starts as the first ends, 11:00 is on its own
        val placed = calendarLanes(listOf(timed("c", "09:50"), timed("a", "09:00"), timed("b", "09:30"), timed("d", "11:00")), 50f)
        assertEquals(listOf("a", "b", "c", "d"), placed.map { it.entry.todoId })
        // 09:50 takes the first lane again, freed at 09:50; the cluster is two wide
        assertEquals(listOf(0 to 2, 1 to 2, 0 to 2, 0 to 1), placed.map { it.lane to it.lanes })
        assertEquals(listOf(listOf("a", "b", "c"), listOf("d")), calendarClusters(placed.map { it.entry }, 50f).map { g -> g.map { it.todoId } })
        // three at one time: three lanes, open before done, then by title
        val same = calendarLanes(listOf(timed("z", "14:00"), timed("y", "14:00"), timed("x", "14:00")), 50f)
        assertEquals(listOf("x" to 0, "y" to 1, "z" to 2), same.map { it.entry.todoId to it.lane })
        assertTrue(same.all { it.lanes == 3 })
        assertTrue(calendarLanes(emptyList(), 50f).isEmpty())
    }

    @Test fun aTapOnTheAxisSnapsDownToTheHalfHour() {
        assertEquals(LocalTime.of(9, 0), slotTime(9 * 60 + 29.9f))
        assertEquals(LocalTime.of(9, 30), slotTime(9 * 60 + 30f))
        assertEquals(LocalTime.of(0, 0), slotTime(-20f))
        // within the day: the last half hour at the latest
        assertEquals(LocalTime.of(23, 30), slotTime(24 * 60 + 10f))
    }

    // #4200, as the web's dropOnAxis / dropOnDay(…, allDay)
    @Test fun aDropOnTheAxisSnapsTheTopEdgeToTheNearestQuarterHour() {
        assertEquals(LocalTime.of(9, 15), dropTime(9 * 60 + 22f))
        assertEquals(LocalTime.of(9, 30), dropTime(9 * 60 + 23f))
        assertEquals(LocalTime.of(0, 0), dropTime(-40f))
        assertEquals(LocalTime.of(23, 45), dropTime(24 * 60 + 30f))
        assertEquals(LocalTime.of(23, 45), dropTime(23 * 60 + 58f))
        // 14:15 on Nov 19 here
        assertEquals(TodoDue.At(Instant.parse("2026-11-19T06:15:00Z")), dropOnAxis(d("2026-11-19"), 14 * 60 + 10f, zone))
        // an all-day one dropped on the axis is due at that moment
        val allDay = Todo("a", dueDate = "2026-11-12")
        val target = AxisTarget.At(d("2026-11-12"), LocalTime.of(10, 0))
        assertEquals(TodoDue.At(Instant.parse("2026-11-12T02:00:00Z")), allDay.moveTo(entry(allDay)!!.dropOn(target, zone)))
    }

    @Test fun aDropOnTheAllDayRowIsDueThatDayAllDay() {
        // 18:00 on Nov 12 here, dropped on the 14th's All Day row
        val at = Todo("a", dueAt = "2026-11-12T10:00:00Z")
        assertEquals(TodoDue.Day(d("2026-11-14")), entry(at)!!.dropOn(AxisTarget.AllDay(d("2026-11-14")), zone))
        assertEquals(TodoDue.Day(d("2026-11-14")), entry(at)!!.dropOnDay(d("2026-11-14"), zone, allDay = true))
        // its own day's All Day row is a change too; an all-day one dropped on its own row is not
        assertEquals(TodoDue.Day(d("2026-11-12")), at.moveTo(entry(at)!!.dropOn(AxisTarget.AllDay(d("2026-11-12")), zone)))
        val day = Todo("b", dueDate = "2026-11-12")
        assertNull(day.moveTo(entry(day)!!.dropOn(AxisTarget.AllDay(d("2026-11-12")), zone)))
        // dropped back at its own quarter hour, nothing is sent; done ones move too
        assertNull(at.moveTo(entry(at)!!.dropOn(AxisTarget.At(d("2026-11-12"), LocalTime.of(18, 0)), zone)))
        // one off the quarter hour (18:07) let go where it snaps back keeps its time; a quarter on, it snaps
        val off = Todo("d", dueAt = "2026-11-12T10:07:00Z")
        assertNull(off.moveTo(entry(off)!!.dropOn(AxisTarget.At(d("2026-11-12"), LocalTime.of(18, 0)), zone)))
        assertEquals(TodoDue.At(Instant.parse("2026-11-12T10:15:00Z")), off.moveTo(entry(off)!!.dropOn(AxisTarget.At(d("2026-11-12"), LocalTime.of(18, 15)), zone)))
        assertEquals(TodoDue.At(Instant.parse("2026-11-13T10:00:00Z")), off.moveTo(entry(off)!!.dropOn(AxisTarget.At(d("2026-11-13"), LocalTime.of(18, 0)), zone)))
        val done = Todo("c", dueAt = "2026-11-12T10:00:00Z", doneAt = "2026-11-04T10:00:00Z")
        assertEquals(TodoDue.At(Instant.parse("2026-11-13T10:00:00Z")), done.moveTo(entry(done)!!.dropOn(AxisTarget.At(d("2026-11-13"), LocalTime.of(18, 0)), zone)))
    }

    @Test fun theTargetUnderACarriedBlock() {
        val week = calendarDays(CalendarMode.Week, d("2026-11-12"), DayOfWeek.MONDAY)
        // seven columns of 100, hours 60 high: the block's top at 9:20 lands at 9:15 on the third day
        assertEquals(AxisTarget.At(d("2026-11-11"), LocalTime.of(9, 15)), axisDropTarget(250f, 700f, week, false, false, 9 * 60f + 20f, 60f))
        // on the All Day row, the day under the finger
        assertEquals(AxisTarget.AllDay(d("2026-11-15")), axisDropTarget(699f, 700f, week, false, true, 0f, 60f))
        // right to left, the first day is at the right
        assertEquals(AxisTarget.AllDay(d("2026-11-15")), axisDropTarget(0f, 700f, week, true, true, 0f, 60f))
        // a block above the axis' top lands at midnight, below its end at 23:45
        assertEquals(LocalTime.of(0, 0), (axisDropTarget(10f, 700f, week, false, false, -30f, 60f) as AxisTarget.At).time)
        assertEquals(LocalTime.of(23, 45), (axisDropTarget(10f, 700f, week, false, false, 24 * 60f, 60f) as AxisTarget.At).time)
        // one day: its whole width
        assertEquals(AxisTarget.At(d("2026-11-12"), LocalTime.of(14, 0)), axisDropTarget(390f, 400f, listOf(d("2026-11-12")), false, false, 14 * 120f, 120f))
        // off the columns
        assertNull(axisDropTarget(-1f, 700f, week, false, false, 0f, 60f))
        assertNull(axisDropTarget(700f, 700f, week, false, false, 0f, 60f))
        assertNull(axisColumn(10f, 700f, 0))
    }

    @Test fun theAxisFirstShowsTheMorningsTodosAndNow() {
        val week = calendarDays(CalendarMode.Week, d("2026-11-12"), DayOfWeek.MONDAY)
        // nothing on it, today not shown: the morning
        val next = calendarDays(CalendarMode.Week, d("2026-11-19"), DayOfWeek.MONDAY)
        assertEquals(CalendarMorning, firstHour(next, emptyList(), now, zone))
        // an hour before the first timed one
        assertEquals(6, firstHour(next, listOf(timed("a", "10:00", "2026-11-17"), timed("b", "07:30", "2026-11-19")), now, zone))
        assertEquals(0, firstHour(next, listOf(timed("a", "00:30", "2026-11-17")), now, zone))
        // one off the days shown counts for nothing
        assertEquals(CalendarMorning, firstHour(next, listOf(timed("a", "07:30", "2026-11-12")), now, zone))
        // today shown (17:30 now): an hour and a half before now, or earlier for the morning's to-dos
        val nov5 = calendarDays(CalendarMode.Week, d("2026-11-05"), DayOfWeek.MONDAY)
        assertEquals(16, firstHour(nov5, emptyList(), now, zone))
        assertEquals(8, firstHour(nov5, listOf(timed("a", "09:00", "2026-11-06")), now, zone))
        // just after midnight, the start of the day
        assertEquals(0, firstHour(listOf(d("2026-11-05")), emptyList(), Instant.parse("2026-11-04T16:30:00Z"), zone))
        assertEquals(CalendarMorning, firstHour(week, emptyList(), now, zone))
    }
}
