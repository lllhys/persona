package persona.platform.phonetools

import android.Manifest
import android.content.ContentUris
import android.content.ContentValues
import android.content.Context
import android.content.OperationApplicationException
import android.net.Uri
import android.provider.CalendarContract as Calendar
import android.provider.ContactsContract
import android.provider.ContactsContract.CommonDataKinds.Nickname
import android.provider.ContactsContract.Data
import android.provider.ContactsContract.RawContacts
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.rule.GrantPermissionRule
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import persona.core.contracts.api.PhoneToolResult
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.time.Instant
import java.time.ZoneId

/**
 * The calendar and contact write tools against the phone's real providers. The Robolectric tests only prove the operations
 * the tools generate; whether an OEM Calendar or Contacts provider accepts them (exception URIs, a new series inserted and
 * the old one cut in one batch, back references, expected counts, what the Instances table expands to, how a recurrence date
 * list is really stored) can only be shown here.
 *
 * Run it on an emulator or a dedicated test phone, never on a phone in daily use: for the duration of a run a placeholder
 * calendar and placeholder contacts exist in the real Calendar and Contacts apps. From the `android/` directory:
 * `./gradlew :platform:phonetools:connectedDebugAndroidTest`. It is not run by the unit test task.
 *
 * The first thing to read from a run is [instancesProjectionAcceptsRruleAndRdate]: it covers the only change to the existing
 * read-only phone_calendar_events query (the RRULE and RDATE projection columns). If it fails, that tool is broken on the
 * device and every other result here is secondary.
 *
 * What the test touches. Everything it writes or deletes is its own: events live in a throwaway local-account calendar
 * ([ACCOUNT]) that it creates with CALLER_IS_SYNCADAPTER and deletes (with every event in it) afterwards, and contacts carry
 * the placeholder family name [FAMILY] and are the only ones deleted. A run that was killed leaves at most those two, and the
 * next run removes them first. What it can only read: the projection test goes through the existing event query, which lists
 * every calendar on the phone, so it reads (and ignores) real events inside its own small time window. Nothing syncs because
 * the account is local.
 *
 * Recurrence date lists (EXDATE / RDATE) are the part that was inferred rather than seen. The tests that seed them read back
 * what the provider actually stored and then check behaviour against that stored form; a form the provider refuses to store
 * is reported as skipped with the provider's own error, never as a pass.
 */
@RunWith(AndroidJUnit4::class)
class CalendarContactsProviderTest {
    @get:Rule val permissions: GrantPermissionRule = GrantPermissionRule.grant(
        Manifest.permission.READ_CALENDAR, Manifest.permission.WRITE_CALENDAR,
        Manifest.permission.READ_CONTACTS, Manifest.permission.WRITE_CONTACTS)

    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val resolver = context.contentResolver
    private val newYork = ZoneId.of("America/New_York")
    private val calendarTools = CalendarTools(context, zone = newYork)
    private val contactsTools = ContactsTool(context)
    private var calendarId = 0L

    @Before fun createPlaceholderCalendar() {
        deleteLocalCalendar()
        val values = ContentValues().apply {
            put(Calendar.Calendars.ACCOUNT_NAME, ACCOUNT); put(Calendar.Calendars.ACCOUNT_TYPE, Calendar.ACCOUNT_TYPE_LOCAL)
            put(Calendar.Calendars.NAME, ACCOUNT); put(Calendar.Calendars.CALENDAR_DISPLAY_NAME, "Placeholder write check")
            put(Calendar.Calendars.OWNER_ACCOUNT, ACCOUNT); put(Calendar.Calendars.CALENDAR_ACCESS_LEVEL, Calendar.Calendars.CAL_ACCESS_OWNER)
            put(Calendar.Calendars.VISIBLE, 1); put(Calendar.Calendars.SYNC_EVENTS, 1)
            put(Calendar.Calendars.CALENDAR_TIME_ZONE, newYork.id)
        }
        calendarId = ContentUris.parseId(requireNotNull(resolver.insert(syncAdapter(Calendar.Calendars.CONTENT_URI), values)))
        deleteContacts()
    }

    @After fun removeEverythingTheTestCreated() {
        deleteLocalCalendar()
        deleteContacts()
    }

    // MARK: the existing read path

    @Test fun instancesProjectionAcceptsRruleAndRdate() {
        val plain = seed("Placeholder plain", utc("2026-11-10T15:00:00Z"), end = utc("2026-11-10T16:00:00Z"))
        val series = seed("Placeholder series", utc("2026-11-10T15:00:00Z"), duration = "PT1H", rrule = "FREQ=DAILY;COUNT=3")
        val extra = seed("Placeholder extra date", utc("2026-11-11T15:00:00Z"), duration = "PT1H", rdate = "20261113T150000Z")
        val result = body(calendarTools.events(buildJsonObject {
            put("start", "2026-11-09T12:00:00-05:00"); put("end", "2026-11-14T00:00:00-05:00"); put("limit", 200)
        }))
        val events = result.getValue("events").jsonArray.map { it.jsonObject }
        fun byId(id: Long) = events.filter { it.getValue("id").jsonPrimitive.content == id.toString() }
        assertTrue("a plain event is listed without recurring", byId(plain).single().containsKey("recurring").not())
        assertEquals("every expanded occurrence of a series says recurring", 3, byId(series).count { it["recurring"]?.jsonPrimitive?.content == "true" })
        assertTrue("an RDATE-only event says recurring", byId(extra).all { it["recurring"]?.jsonPrimitive?.content == "true" } && byId(extra).isNotEmpty())
    }

    // MARK: this occurrence

    @Test fun changingOneOccurrenceMakesOneExceptionAndCopiesTheSeriesRowsOnce() {
        val series = seedWithRows("Placeholder daily", utc("2026-11-10T15:00:00Z"), "FREQ=DAILY;COUNT=5")
        val before = instances()
        assertEquals(5, before.size)
        val third = before[2]

        val result = body(calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-12T10:00:00-05:00", "title" to "Placeholder moved"))))

        val after = instances()
        assertEquals(5, after.size)
        assertEquals(listOf(false, false, true, false, false), after.map { it.title == "Placeholder moved" })
        assertEquals(before.map { it.begin }, after.map { it.begin })
        val exception = after[2]
        assertNotEquals("the changed occurrence is a separate row, not the series", series, exception.eventId)
        assertEquals(exception.eventId.toString(), result.getValue("id").jsonPrimitive.content)
        assertEquals(third.begin, exception.begin)
        // The series itself is untouched.
        assertEquals("Placeholder daily", row(series).title)
        assertEquals("FREQ=DAILY;COUNT=5", row(series).rrule)
        // The provider copies attendees and reminders onto an exception itself and the tool does not write them again: one copy each.
        assertEquals("attendees on the exception", 1, count(Calendar.Attendees.CONTENT_URI, Calendar.Attendees.EVENT_ID, exception.eventId))
        assertEquals("reminders on the exception", 1, count(Calendar.Reminders.CONTENT_URI, Calendar.Reminders.EVENT_ID, exception.eventId))
        assertEquals("attendees on the series", 1, count(Calendar.Attendees.CONTENT_URI, Calendar.Attendees.EVENT_ID, series))
        assertEquals("reminders on the series", 1, count(Calendar.Reminders.CONTENT_URI, Calendar.Reminders.EVENT_ID, series))
    }

    @Test fun deletingOneOccurrenceCancelsOnlyThatOne() {
        val series = seed("Placeholder daily", utc("2026-11-10T15:00:00Z"), duration = "PT1H", rrule = "FREQ=DAILY;COUNT=5")
        val before = instances()
        calendarTools.delete(calendarTools.prepareDelete(args("id" to series, "occurrence_start" to "2026-11-12T10:00:00-05:00")))
        assertEquals(before.filterIndexed { index, _ -> index != 2 }.map { it.begin }, instances().map { it.begin })
        assertEquals("FREQ=DAILY;COUNT=5", row(series).rrule)
    }

    @Test fun anExceptionOfASeriesWithoutASyncIdIsRefused() {
        // Measured on the AOSP provider: an exception of a series without a _sync_id is stored, but the series keeps showing the
        // old occurrence (and loses its other expanded occurrences until the next re-expansion). So the tools refuse it up front.
        val series = seed("Placeholder local", utc("2026-11-10T15:00:00Z"), duration = "PT1H", rrule = "FREQ=DAILY;COUNT=5", synced = false)
        val before = instances()
        assertEquals(5, before.size)
        val change = runCatching { calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-12T10:00:00-05:00", "title" to "Placeholder moved")) }.exceptionOrNull()
        assertTrue("changing one occurrence of an unsynced series is refused ($change)", change is PhoneToolFailure)
        val cancel = runCatching { calendarTools.prepareDelete(args("id" to series, "occurrence_start" to "2026-11-12T10:00:00-05:00")) }.exceptionOrNull()
        assertTrue("cancelling one occurrence of an unsynced series is refused ($cancel)", cancel is PhoneToolFailure)
        assertEquals(before, instances())
        assertTrue("no exception row was written", exceptionsOf(series).isEmpty())
    }

    @Test fun anUnsyncedSeriesCanStillBeSplitForThisAndFuture() {
        val series = seedWithRows("Placeholder local", utc("2026-11-10T15:00:00Z"), "FREQ=DAILY;COUNT=6").also {
            resolver.update(syncAdapter(ContentUris.withAppendedId(Calendar.Events.CONTENT_URI, it)),
                ContentValues().apply { putNull(Calendar.Events._SYNC_ID) }, null, null)
        }
        val before = instances()
        calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-13T10:00:00-05:00", "span" to "future", "title" to "Placeholder new series")))
        val after = instances()
        assertEquals(before.map { it.begin }, after.map { it.begin })
        assertEquals(List(3) { "Placeholder local" } + List(3) { "Placeholder new series" }, after.map { it.title })
        calendarTools.delete(calendarTools.prepareDelete(args(
            "id" to after[3].eventId, "occurrence_start" to "2026-11-14T10:00:00-05:00", "span" to "future")))
        assertEquals(before.take(4).map { it.begin }, instances().map { it.begin })
    }

    // MARK: this and future

    @Test fun changingFutureKeepsThePastExceptionItsRowsAndTheSeriesRows() {
        val series = seedWithRows("Placeholder daily", utc("2026-11-10T15:00:00Z"), "FREQ=DAILY;COUNT=6")
        // The first occurrence is changed on its own: an exception with its own copy of the attendee and the reminder.
        calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-10T10:00:00-05:00", "title" to "Placeholder first, renamed")))
        val pastException = instances().first()
        assertEquals("Placeholder first, renamed", pastException.title)
        assertNotEquals(series, pastException.eventId)

        calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-13T10:00:00-05:00", "span" to "future", "title" to "Placeholder new series")))

        val after = instances()
        assertEquals(List(6) { utc("2026-11-10T15:00:00Z") + it * DAY }, after.map { it.begin })
        assertEquals(listOf("Placeholder first, renamed") + List(2) { "Placeholder daily" } + List(3) { "Placeholder new series" },
            after.map { it.title })
        // The past exception is the same row (not recreated or dropped) and keeps its rows; the old series keeps its rows.
        assertEquals(pastException.eventId, after.first().eventId)
        assertEquals(listOf(Calendar.Events.STATUS_CONFIRMED), exceptionsOf(series).map { it.status })
        assertEquals("attendees on the past exception", 1, count(Calendar.Attendees.CONTENT_URI, Calendar.Attendees.EVENT_ID, pastException.eventId))
        assertEquals("reminders on the past exception", 1, count(Calendar.Reminders.CONTENT_URI, Calendar.Reminders.EVENT_ID, pastException.eventId))
        assertEquals("attendees on the old series", 1, count(Calendar.Attendees.CONTENT_URI, Calendar.Attendees.EVENT_ID, series))
        assertEquals("reminders on the old series", 1, count(Calendar.Reminders.CONTENT_URI, Calendar.Reminders.EVENT_ID, series))
        // The new series carries one copy of each, and starts at the selected occurrence with the rest of the count.
        val newSeries = after[3].eventId
        assertNotEquals(series, newSeries)
        assertEquals("attendees on the new series", 1, count(Calendar.Attendees.CONTENT_URI, Calendar.Attendees.EVENT_ID, newSeries))
        assertEquals("reminders on the new series", 1, count(Calendar.Reminders.CONTENT_URI, Calendar.Reminders.EVENT_ID, newSeries))
        assertEquals("FREQ=DAILY;COUNT=3", row(newSeries).rrule)
        val oldRule = requireNotNull(row(series).rrule)
        assertTrue(oldRule, oldRule.contains("UNTIL=") && !oldRule.contains("COUNT="))
    }

    @Test fun deletingFutureKeepsThePastAndAnExceptionInIt() {
        val series = seed("Placeholder daily", utc("2026-11-10T15:00:00Z"), duration = "PT1H", rrule = "FREQ=DAILY;COUNT=6")
        calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-10T10:00:00-05:00", "title" to "Placeholder first, renamed")))
        assertEquals(1, exceptionsOf(series).size)

        calendarTools.delete(calendarTools.prepareDelete(args(
            "id" to series, "occurrence_start" to "2026-11-13T10:00:00-05:00", "span" to "future")))

        val left = instances()
        assertEquals(listOf("2026-11-10T15:00:00Z", "2026-11-11T15:00:00Z", "2026-11-12T15:00:00Z"), left.map { Instant.ofEpochMilli(it.begin).toString() })
        assertEquals("Placeholder first, renamed", left[0].title)
        // Cutting the series must not drop the change that was made before the cut.
        assertEquals(1, exceptionsOf(series).count { it.status != Calendar.Events.STATUS_CANCELED })
        val rule = requireNotNull(row(series).rrule)
        assertTrue(rule, rule.contains("UNTIL=") && !rule.contains("COUNT="))
    }

    @Test fun changingFutureSplitsACountSeriesIntoTheOldPastAndANewRest() {
        val series = seedWithRows("Placeholder daily", utc("2026-11-10T15:00:00Z"), "FREQ=DAILY;COUNT=6")
        val before = instances()
        val result = body(calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-13T10:00:00-05:00", "span" to "future",
            "title" to "Placeholder new series", "location" to "Placeholder room"))))

        val after = instances()
        assertEquals("every day is still there once", before.map { it.begin }, after.map { it.begin })
        assertEquals(List(3) { "Placeholder daily" } + List(3) { "Placeholder new series" }, after.map { it.title })
        val newSeries = after[3].eventId
        assertNotEquals(series, newSeries)
        assertEquals(setOf(newSeries), after.drop(3).map { it.eventId }.toSet())
        assertEquals(newSeries.toString(), result.getValue("id").jsonPrimitive.content)
        assertEquals("FREQ=DAILY;COUNT=3", row(newSeries).rrule)
        val oldRule = requireNotNull(row(series).rrule)
        assertTrue(oldRule, oldRule.contains("UNTIL=") && !oldRule.contains("COUNT="))
        assertEquals("Placeholder room", row(newSeries).location)
        // Attendees and reminders follow the new series once and stay on the old one.
        assertEquals(1, count(Calendar.Attendees.CONTENT_URI, Calendar.Attendees.EVENT_ID, newSeries))
        assertEquals(1, count(Calendar.Reminders.CONTENT_URI, Calendar.Reminders.EVENT_ID, newSeries))
        assertEquals(1, count(Calendar.Attendees.CONTENT_URI, Calendar.Attendees.EVENT_ID, series))
        assertEquals(1, count(Calendar.Reminders.CONTENT_URI, Calendar.Reminders.EVENT_ID, series))
    }

    @Test fun anUntilSeriesKeepsItsEndWhenSplit() {
        val series = seed("Placeholder daily", utc("2026-11-10T15:00:00Z"), duration = "PT1H", rrule = "FREQ=DAILY;UNTIL=20261115T150000Z")
        val before = instances()
        assertEquals(6, before.size)
        calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-13T10:00:00-05:00", "span" to "future", "title" to "Placeholder new series")))
        val after = instances()
        assertEquals(before.map { it.begin }, after.map { it.begin })
        assertEquals(List(3) { "Placeholder daily" } + List(3) { "Placeholder new series" }, after.map { it.title })
        assertEquals("FREQ=DAILY;UNTIL=20261115T150000Z", row(after[3].eventId).rrule)
    }

    @Test fun aDateOnlyUntilOnATimedSeriesIsCutWithoutDuplicates() {
        // How a provider reads UNTIL=20261115 on a timed series (the whole day, or midnight) is part of what is observed here.
        val series = seed("Placeholder daily", utc("2026-11-10T15:00:00Z"), duration = "PT1H", rrule = "FREQ=DAILY;UNTIL=20261115")
        val before = instances()
        val selectedIndex = 3
        val selected = Instant.ofEpochMilli(before[selectedIndex].begin).atZone(newYork)
        calendarTools.delete(calendarTools.prepareDelete(args(
            "id" to series, "occurrence_start" to selected.toOffsetDateTime().toString(), "span" to "future")))
        assertEquals(before.take(selectedIndex).map { it.begin }, instances().map { it.begin })
    }

    @Test fun anAllDaySeriesIsCutAndSplitOnUtcDates() {
        val first = utc("2026-11-09T00:00:00Z")
        val series = seed("Placeholder all day", first, duration = "P1D", rrule = "FREQ=DAILY;COUNT=5", allDay = true, timezone = "UTC")
        assertEquals(5, instances().size)
        calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-11T00:00:00-05:00", "span" to "future", "title" to "Placeholder all day new")))
        val split = instances()
        assertEquals(List(5) { first + it * DAY }, split.map { it.begin })
        assertEquals(List(2) { "Placeholder all day" } + List(3) { "Placeholder all day new" }, split.map { it.title })
        assertTrue(split.all { it.allDay })

        calendarTools.delete(calendarTools.prepareDelete(args(
            "id" to split[3].eventId, "occurrence_start" to "2026-11-12T00:00:00-05:00", "span" to "future")))
        assertEquals(List(3) { first + it * DAY }, instances().map { it.begin })
    }

    @Test fun aDailySeriesAcrossTheFallBackChangeKeepsItsLocalTimeAndItsRemainingCount() {
        // 10:00 in New York: 14:00Z before the clocks go back on 2026-11-01, 15:00Z after it.
        val series = seed("Placeholder daily", utc("2026-10-30T14:00:00Z"), duration = "PT1H", rrule = "FREQ=DAILY;COUNT=6")
        val before = instances()
        assertEquals(6, before.size)
        calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to "2026-11-02T10:00:00-05:00", "span" to "future", "title" to "Placeholder new series")))
        val after = instances()
        assertEquals("the remaining COUNT is exact across the change", before.map { it.begin }, after.map { it.begin })
        assertEquals(List(3) { "Placeholder daily" } + List(3) { "Placeholder new series" }, after.map { it.title })
        assertTrue(after.all { Instant.ofEpochMilli(it.begin).atZone(newYork).hour == 10 })
    }

    // MARK: all-day length across clock changes

    @Test fun anAllDayEventMovedByItsStartKeepsItsNumberOfDays() {
        val fall = seed("Placeholder trip", utc("2026-10-30T00:00:00Z"), end = utc("2026-11-01T00:00:00Z"), allDay = true, timezone = "UTC")
        calendarTools.update(calendarTools.prepareUpdate(args("id" to fall, "start" to "2026-11-01")))
        assertEquals(utc("2026-11-01T00:00:00Z") to utc("2026-11-03T00:00:00Z"), row(fall).start to row(fall).end)

        val spring = seed("Placeholder trip", utc("2027-03-06T00:00:00Z"), end = utc("2027-03-08T00:00:00Z"), allDay = true, timezone = "UTC")
        calendarTools.update(calendarTools.prepareUpdate(args("id" to spring, "start" to "2027-03-13")))
        assertEquals(utc("2027-03-13T00:00:00Z") to utc("2027-03-15T00:00:00Z"), row(spring).start to row(spring).end)
    }


    // MARK: a series whose zone is not the phone's, with extra and excluded dates

    /**
     * Every way an EXDATE / RDATE list could be written for a Berlin series while the tools run in New York. The first reading
     * of what the provider keeps is the point: the test asserts on the stored text, not on what was sent.
     */
    private enum class DateForm(val exdate: String, val rdate: String) {
        ProviderZonePrefix("Europe/Berlin;20261112T100000,20261116T100000", "Europe/Berlin;20261114T083000,20261121T100000"),
        IcalendarTzid("TZID=Europe/Berlin:20261112T100000,20261116T100000", "TZID=Europe/Berlin:20261114T083000,20261121T100000"),
        UtcInstants("20261112T090000Z,20261116T090000Z", "20261114T073000Z,20261121T090000Z"),
    }

    private data class Stored(val exdate: String?, val rdate: String?, val rrule: String?)

    private fun storedDates(id: Long): Stored = resolver.query(ContentUris.withAppendedId(Calendar.Events.CONTENT_URI, id),
        arrayOf(Calendar.Events.EXDATE, Calendar.Events.RDATE, Calendar.Events.RRULE), null, null, null)!!.use { cursor ->
        assertTrue(cursor.moveToFirst())
        Stored(cursor.getString(0), cursor.getString(1), cursor.getString(2))
    }

    /** The daily 10:00 Berlin series (09:00Z in November) with the dates of [form], or null when the provider refuses them. */
    private fun seedBerlin(form: DateForm): Pair<Long, String?>? {
        val values = eventValues("Placeholder berlin", utc("2026-11-10T09:00:00Z"), duration = "PT1H",
            rrule = "FREQ=DAILY;COUNT=10", timezone = "Europe/Berlin").apply {
            put(Calendar.Events.EXDATE, form.exdate); put(Calendar.Events.RDATE, form.rdate)
        }
        val id = try {
            ContentUris.parseId(requireNotNull(resolver.insert(syncAdapter(Calendar.Events.CONTENT_URI), values)))
        } catch (refused: Exception) {
            android.util.Log.w(TAG, "the provider refused ${form.name}: $refused")
            return null
        }
        val kept = storedDates(id)
        return id to "stored exdate=${kept.exdate} rdate=${kept.rdate}"
    }

    private fun dayTokens(list: String?): List<String> = Regex("""\d{8}""").findAll(list.orEmpty()).map { it.value }.toList()

    /** Whatever precedes the dates of a stored list: a zone with `;`, a `TZID=` with `:` or nothing. */
    private fun prefixOf(list: String?): String {
        val text = list.orEmpty()
        if (text.startsWith("TZID=", ignoreCase = true)) return text.substring(0, text.indexOf(':') + 1)
        val semicolon = text.indexOf(';')
        // A date item starts with a digit; a zone name does not.
        return if (semicolon > 0 && text.first().isLetter()) text.substring(0, semicolon + 1) else ""
    }

    private fun expandsBerlinDates(before: List<Seen>): Boolean {
        val begins = before.map { it.begin }
        return utc("2026-11-12T09:00:00Z") !in begins && utc("2026-11-16T09:00:00Z") !in begins &&
            utc("2026-11-14T07:30:00Z") in begins && utc("2026-11-21T09:00:00Z") in begins
    }

    private fun splitBerlinSeries(form: DateForm) {
        val (series, description) = seedBerlin(form) ?: run {
            org.junit.Assume.assumeTrue("the provider refuses the ${form.name} form (see logcat tag $TAG)", false)
            return
        }
        val before = instances()
        val stored = storedDates(series)
        android.util.Log.i(TAG, "${form.name}: $description, ${before.size} instances")
        // The extra date on the selected day is earlier than the selected 10:00 Berlin occurrence: it belongs to the past,
        // but only if the dates are read in the series zone. Read in the phone's zone (New York) it would look like the future.
        // What the provider makes of the lists is read, not assumed: the excluded days are gone and the extra ones are there.
        if (!expandsBerlinDates(before)) {
            android.util.Log.w(TAG, "${form.name}: stored but not expanded as seeded: $description, ${before.map { it.begin }}")
            org.junit.Assume.assumeTrue("the provider stored but did not expand ${form.name}; this form is unverified", false)
            return
        }
        val selected = "2026-11-14T10:00:00+01:00"
        // One hour later in Berlin, so shifted dates show which zone they were read in.
        val result = body(calendarTools.update(calendarTools.prepareUpdate(args(
            "id" to series, "occurrence_start" to selected, "span" to "future", "start" to "2026-11-14T11:00:00+01:00",
            "title" to "Placeholder berlin new"))))

        val newSeries = result.getValue("id").jsonPrimitive.content.toLong()
        assertNotEquals(series, newSeries)
        val old = storedDates(series)
        val new = storedDates(newSeries)
        // The past stays with the old series: the excluded 12th and the extra date earlier on the 14th. The rest moves on.
        assertEquals(description, listOf("20261112"), dayTokens(old.exdate))
        assertEquals(description, listOf("20261114"), dayTokens(old.rdate))
        assertEquals(description, listOf("20261116"), dayTokens(new.exdate))
        assertEquals(description, listOf("20261121"), dayTokens(new.rdate))
        // Whatever shape the provider stored (prefix or none), the tool wrote the same shape back.
        assertEquals(prefixOf(stored.exdate), prefixOf(old.exdate))
        assertEquals(prefixOf(stored.exdate), prefixOf(new.exdate))
        assertEquals(prefixOf(stored.rdate), prefixOf(old.rdate))
        assertEquals(prefixOf(stored.rdate), prefixOf(new.rdate))
        // And the provider agrees on what the series now is: the same days, the new series one hour later from the 14th.
        val after = instances()
        assertEquals(description, before.take(4).map { it.begin }, after.take(4).map { it.begin })
        assertEquals(description, "Placeholder berlin", after.first().title)
        assertTrue(description, after.any { it.eventId == newSeries && it.title == "Placeholder berlin new" })
        assertTrue(description, after.filter { it.eventId == newSeries }.all { Instant.ofEpochMilli(it.begin).atZone(ZoneId.of("Europe/Berlin")).hour == 11 })
    }

    @Test fun aBerlinSeriesWithProviderZonePrefixedDatesIsSplitInItsOwnZone() = splitBerlinSeries(DateForm.ProviderZonePrefix)
    @Test fun aBerlinSeriesWithTzidPrefixedDatesIsSplitInItsOwnZone() = splitBerlinSeries(DateForm.IcalendarTzid)
    @Test fun aBerlinSeriesWithUtcDatesIsSplitInItsOwnZone() = splitBerlinSeries(DateForm.UtcInstants)

    @Test fun aBerlinSeriesIsCutBeforeTheSelectedOccurrenceWhateverFormTheProviderKept() {
        var tried = 0
        for (form in DateForm.entries) {
            val (series, description) = seedBerlin(form) ?: continue
            val before = instances()
            if (!expandsBerlinDates(before)) {
                android.util.Log.w(TAG, "${form.name}: delete check skips a stored but unexpanded form: $description")
                deleteLocalCalendar(); createPlaceholderCalendar()
                continue
            }
            tried++
            calendarTools.delete(calendarTools.prepareDelete(args(
                "id" to series, "occurrence_start" to "2026-11-14T10:00:00+01:00", "span" to "future")))
            val after = instances()
            // Only what lies before the selected occurrence stays, including the extra date earlier on the same day.
            val cut = utc("2026-11-14T09:00:00Z")
            assertEquals(description, before.filter { it.begin < cut }.map { it.begin }, after.map { it.begin })
            assertTrue(description, after.any { it.begin == utc("2026-11-14T07:30:00Z") })
            deleteLocalCalendar(); createPlaceholderCalendar()
        }
        org.junit.Assume.assumeTrue("no stored date-list form expanded; read logcat tag $TAG", tried > 0)
    }

    // MARK: expected counts and refusals

    @Test fun aWriteToAnEventThatIsGoneFailsInsteadOfSucceedingSilently() {
        val gone = ContentUris.parseId(requireNotNull(resolver.insert(syncAdapter(Calendar.Events.CONTENT_URI), eventValues(
            "Placeholder gone", utc("2026-11-10T15:00:00Z"), end = utc("2026-11-10T16:00:00Z")))))
        resolver.delete(syncAdapter(ContentUris.withAppendedId(Calendar.Events.CONTENT_URI, gone)), null, null)
        try {
            resolver.applyProviderWrites(Calendar.AUTHORITY, listOf(ProviderWrite(ProviderWrite.Kind.Update, Calendar.Events.CONTENT_URI,
                mapOf(Calendar.Events.TITLE to "Placeholder never written"), id = gone)))
            fail("a batch update that matches no row must not report success")
        } catch (expected: OperationApplicationException) {
            // The expected count of one rows is what turns "nothing matched" into an error.
        }
        assertTrue(instances().none { it.title == "Placeholder never written" })
    }

    @Test fun aChangeAfterTheCardIsRefusedWithoutAWrite() {
        val event = seed("Placeholder plain", utc("2026-11-10T15:00:00Z"), end = utc("2026-11-10T16:00:00Z"))
        val prepared = calendarTools.prepareUpdate(args("id" to event, "title" to "Placeholder approved"))
        resolver.update(syncAdapter(ContentUris.withAppendedId(Calendar.Events.CONTENT_URI, event)),
            ContentValues().apply { put(Calendar.Events.EVENT_LOCATION, "Placeholder elsewhere") }, null, null)
        try {
            calendarTools.update(prepared)
            fail("the event changed after the card, so nothing may be written")
        } catch (expected: PhoneToolWriteRefusal) {
            assertEquals("Placeholder plain", row(event).title)
        }
    }

    // MARK: contacts

    @Test fun createAddAndReplaceAContactAgainstTheRealAddressBook() {
        val created = body(contactsTools.create(args(
            "given_name" to "Placeholder", "family_name" to FAMILY, "company" to "Example Co",
            "phones" to entries("555 0100" to "mobile"), "emails" to entries("tester@example.com" to "work"))))
        val id = created.getValue("id").jsonPrimitive.content
        assertEquals(1, created.getValue("phones").jsonArray.size)

        // An unrelated data row must survive every edit below.
        val raw = rawContactIdsOf(id.toLong()).first()
        resolver.insert(Data.CONTENT_URI, ContentValues().apply {
            put(Data.RAW_CONTACT_ID, raw); put(Data.MIMETYPE, Nickname.CONTENT_ITEM_TYPE); put(Nickname.NAME, "Placeholder nick")
        })

        val added = body(contactsTools.update(contactsTools.prepareUpdate(args("id" to id, "phones" to entries("555 0101" to "mobile")))))
        assertEquals("a new number is added next to the old one", 2, added.getValue("phones").jsonArray.size)

        val refused = runCatching { contactsTools.prepareUpdate(args("id" to id, "company" to "Other Co")) }.exceptionOrNull()
        assertTrue("a different company needs replace=true", refused is PhoneToolFailure)

        val replaced = body(contactsTools.update(contactsTools.prepareUpdate(args(
            "id" to id, "replace" to true, "company" to "Other Co", "phones" to entries("555 0102" to "mobile")))))
        assertEquals(1, replaced.getValue("phones").jsonArray.size)
        assertEquals("Other Co", replaced.getValue("company").jsonPrimitive.content)
        assertEquals("emails were not part of the call, so they stay", 1, replaced.getValue("emails").jsonArray.size)
        assertEquals("Placeholder nick", nicknameOf(raw))
    }

    @Test fun renamingAContactReadsItBack() {
        val id = body(contactsTools.create(args("given_name" to "Placeholder", "family_name" to FAMILY))).getValue("id").jsonPrimitive.content
        val result = body(contactsTools.update(contactsTools.prepareUpdate(args("id" to id, "given_name" to "Renamed", "replace" to true))))
        assertTrue(result.getValue("name").jsonPrimitive.content.contains("Renamed"))
    }

    @Test fun aContactWriteToARowThatIsGoneFailsInsteadOfSucceedingSilently() {
        try {
            resolver.applyProviderWrites(ContactsContract.AUTHORITY, listOf(ProviderWrite(ProviderWrite.Kind.Update, Data.CONTENT_URI,
                mapOf(Data.DATA1 to "never written"), id = Long.MAX_VALUE / 2)))
            fail("a batch update that matches no row must not report success")
        } catch (expected: OperationApplicationException) {
        }
    }

    // MARK: helpers

    private data class Seen(val eventId: Long, val title: String, val begin: Long, val end: Long, val allDay: Boolean)
    private data class Row(val title: String, val rrule: String?, val location: String?, val start: Long, val end: Long?, val status: Int)

    private fun utc(iso: String) = Instant.parse(iso).toEpochMilli()

    private fun args(vararg pairs: Pair<String, Any>): JsonObject = buildJsonObject {
        for ((key, value) in pairs) when (value) {
            is Boolean -> put(key, value)
            is JsonArray -> put(key, value)
            else -> put(key, value.toString())
        }
    }

    private fun entries(vararg items: Pair<String, String>) = buildJsonArray {
        for ((value, label) in items) add(buildJsonObject { put("value", value); put("label", label) })
    }

    private fun body(result: PhoneToolResult): JsonObject =
        Json.parseToJsonElement(result.content.single().jsonObject.getValue("text").jsonPrimitive.content).jsonObject

    private fun syncAdapter(uri: Uri): Uri = uri.buildUpon()
        .appendQueryParameter(Calendar.CALLER_IS_SYNCADAPTER, "true")
        .appendQueryParameter(Calendar.Calendars.ACCOUNT_NAME, ACCOUNT)
        .appendQueryParameter(Calendar.Calendars.ACCOUNT_TYPE, Calendar.ACCOUNT_TYPE_LOCAL).build()

    private var syncCounter = 0

    /**
     * A repeating event gets a `_sync_id`, like every event of a synced calendar. The provider links an exception to its series
     * through that id: without it the exception is not applied to the series' instances at all (see
     * [anExceptionOfASeriesWithoutASyncIdIsRefused]).
     */
    private fun eventValues(
        title: String, start: Long, end: Long? = null, duration: String? = null, rrule: String? = null, rdate: String? = null,
        allDay: Boolean = false, timezone: String = newYork.id, synced: Boolean = true,
    ) = ContentValues().apply {
        if (synced && (rrule != null || rdate != null)) put(Calendar.Events._SYNC_ID, "persona-write-check-${++syncCounter}")
        put(Calendar.Events.CALENDAR_ID, calendarId); put(Calendar.Events.TITLE, title); put(Calendar.Events.DTSTART, start)
        put(Calendar.Events.EVENT_TIMEZONE, timezone); put(Calendar.Events.ALL_DAY, if (allDay) 1 else 0)
        // A repeating event has a DURATION and no end; a plain one the opposite.
        end?.let { put(Calendar.Events.DTEND, it) }
        duration?.let { put(Calendar.Events.DURATION, it) }
        rrule?.let { put(Calendar.Events.RRULE, it) }
        rdate?.let { put(Calendar.Events.RDATE, it) }
    }

    private fun seed(
        title: String, start: Long, end: Long? = null, duration: String? = null, rrule: String? = null, rdate: String? = null,
        allDay: Boolean = false, timezone: String = newYork.id, synced: Boolean = true,
    ): Long = ContentUris.parseId(requireNotNull(resolver.insert(syncAdapter(Calendar.Events.CONTENT_URI),
        eventValues(title, start, end, duration, rrule, rdate, allDay, timezone, synced))))

    /** A one-hour repeating event with one attendee and one reminder, so a copy to an exception or new series shows. */
    private fun seedWithRows(title: String, start: Long, rrule: String): Long {
        val id = seed(title, start, duration = "PT1H", rrule = rrule)
        resolver.insert(syncAdapter(Calendar.Attendees.CONTENT_URI), ContentValues().apply {
            put(Calendar.Attendees.EVENT_ID, id); put(Calendar.Attendees.ATTENDEE_NAME, "Placeholder Guest")
            put(Calendar.Attendees.ATTENDEE_EMAIL, "guest@example.com")
            put(Calendar.Attendees.ATTENDEE_RELATIONSHIP, Calendar.Attendees.RELATIONSHIP_ATTENDEE)
            put(Calendar.Attendees.ATTENDEE_TYPE, Calendar.Attendees.TYPE_REQUIRED)
            put(Calendar.Attendees.ATTENDEE_STATUS, Calendar.Attendees.ATTENDEE_STATUS_INVITED)
        })
        resolver.insert(syncAdapter(Calendar.Reminders.CONTENT_URI), ContentValues().apply {
            put(Calendar.Reminders.EVENT_ID, id); put(Calendar.Reminders.MINUTES, 10); put(Calendar.Reminders.METHOD, Calendar.Reminders.METHOD_ALERT)
        })
        return id
    }

    /** What the Instances table expands the placeholder calendar to, oldest first. */
    private fun instances(from: Long = utc("2026-01-01T00:00:00Z"), to: Long = utc("2027-12-31T00:00:00Z")): List<Seen> {
        val uri = Calendar.Instances.CONTENT_URI.buildUpon().also { ContentUris.appendId(it, from); ContentUris.appendId(it, to) }.build()
        return buildList {
            resolver.query(uri, arrayOf(Calendar.Instances.EVENT_ID, Calendar.Instances.TITLE, Calendar.Instances.BEGIN,
                Calendar.Instances.END, Calendar.Instances.ALL_DAY), "${Calendar.Instances.CALENDAR_ID} = ?",
                arrayOf(calendarId.toString()), "${Calendar.Instances.BEGIN} ASC, ${Calendar.Instances.EVENT_ID} ASC")!!.use { cursor ->
                while (cursor.moveToNext()) add(Seen(cursor.getLong(0), cursor.getString(1).orEmpty(), cursor.getLong(2),
                    cursor.getLong(3), cursor.getInt(4) != 0))
            }
        }
    }

    private fun row(id: Long): Row = resolver.query(ContentUris.withAppendedId(Calendar.Events.CONTENT_URI, id),
        arrayOf(Calendar.Events.TITLE, Calendar.Events.RRULE, Calendar.Events.EVENT_LOCATION, Calendar.Events.DTSTART,
            Calendar.Events.DTEND, Calendar.Events.STATUS), null, null, null)!!.use { cursor ->
        assertTrue("event $id exists", cursor.moveToFirst())
        Row(cursor.getString(0).orEmpty(), cursor.getString(1), cursor.getString(2), cursor.getLong(3),
            if (cursor.isNull(4)) null else cursor.getLong(4), if (cursor.isNull(5)) Calendar.Events.STATUS_CONFIRMED else cursor.getInt(5))
    }

    private fun exceptionsOf(seriesId: Long): List<Row> = buildList {
        resolver.query(Calendar.Events.CONTENT_URI, arrayOf(Calendar.Events.TITLE, Calendar.Events.RRULE, Calendar.Events.EVENT_LOCATION,
            Calendar.Events.DTSTART, Calendar.Events.DTEND, Calendar.Events.STATUS), "${Calendar.Events.ORIGINAL_ID} = ?",
            arrayOf(seriesId.toString()), null)!!.use { cursor ->
            while (cursor.moveToNext()) add(Row(cursor.getString(0).orEmpty(), cursor.getString(1), cursor.getString(2), cursor.getLong(3),
                if (cursor.isNull(4)) null else cursor.getLong(4), if (cursor.isNull(5)) Calendar.Events.STATUS_CONFIRMED else cursor.getInt(5)))
        }
    }

    private fun count(uri: Uri, column: String, eventId: Long): Int =
        resolver.query(uri, arrayOf(column), "$column = ?", arrayOf(eventId.toString()), null)!!.use { it.count }

    private fun deleteLocalCalendar() {
        resolver.delete(syncAdapter(Calendar.Calendars.CONTENT_URI),
            "${Calendar.Calendars.ACCOUNT_NAME} = ? AND ${Calendar.Calendars.ACCOUNT_TYPE} = ?", arrayOf(ACCOUNT, Calendar.ACCOUNT_TYPE_LOCAL))
    }

    private fun rawContactIdsOf(contactId: Long): List<Long> = buildList {
        resolver.query(RawContacts.CONTENT_URI, arrayOf(RawContacts._ID), "${RawContacts.CONTACT_ID} = ?", arrayOf(contactId.toString()), null)!!
            .use { while (it.moveToNext()) add(it.getLong(0)) }
    }

    private fun nicknameOf(rawContactId: Long): String? = resolver.query(Data.CONTENT_URI, arrayOf(Nickname.NAME),
        "${Data.RAW_CONTACT_ID} = ? AND ${Data.MIMETYPE} = ?", arrayOf(rawContactId.toString(), Nickname.CONTENT_ITEM_TYPE), null)!!
        .use { if (it.moveToFirst()) it.getString(0) else null }

    /** Only contacts that carry the placeholder family name are ever removed, and removed for real (no trash). */
    private fun deleteContacts() {
        val raws = buildList {
            resolver.query(Data.CONTENT_URI, arrayOf(Data.RAW_CONTACT_ID),
                "${Data.MIMETYPE} = ? AND ${ContactsContract.CommonDataKinds.StructuredName.FAMILY_NAME} = ?",
                arrayOf(ContactsContract.CommonDataKinds.StructuredName.CONTENT_ITEM_TYPE, FAMILY), null)!!
                .use { while (it.moveToNext()) add(it.getLong(0)) }
        }
        if (raws.isEmpty()) return
        val purge = RawContacts.CONTENT_URI.buildUpon().appendQueryParameter(ContactsContract.CALLER_IS_SYNCADAPTER, "true").build()
        resolver.delete(purge, "${RawContacts._ID} IN (${raws.joinToString(",")})", null)
    }

    private companion object {
        const val ACCOUNT = "persona-write-check"
        const val FAMILY = "Persona-Write-Check"
        const val DAY = 86_400_000L
        const val TAG = "PersonaProviderCheck"
    }
}
