package persona.platform.phonetools

import android.app.Application
import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.Density
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.After
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.HubScope
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.HubConnection
import persona.core.contracts.api.PhoneToolCall
import persona.core.contracts.api.PhoneToolResult
import persona.core.contracts.api.PhoneToolSpec
import persona.core.contracts.api.PhoneToolsTransport
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File
import java.lang.reflect.Proxy
import java.time.Instant
import java.time.ZoneId

/**
 * The confirmation before each calendar and contact write (change this / future, delete, contact update with replace,
 * new contact, long values), in each language, Glass light and dark, plus the largest font. PNGs go to
 * `build/i18n-shots/` (or `-Dpersona.shots=<dir>`). They show the card's layout and wording; a person looks at them.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class CalendarContactSummaryShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val zone = ZoneId.of("Asia/Shanghai")

    @After fun stop() = scope.cancel()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", "允许")

    @Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
    @Test fun english() = shots("en", "Allow")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h900dp-xhdpi")
    @Test fun japanese() = shots("ja", "許可")

    private fun ms(iso: String) = Instant.parse(iso).toEpochMilli()

    private fun record(id: Long, title: String, rrule: String?) = CalendarEventRecord(
        id = id, calendarId = 7, title = title, start = ms("2026-11-04T07:00:00Z"),
        end = if (rrule == null) ms("2026-11-04T08:00:00Z") else null, duration = if (rrule == null) null else "PT1H",
        timezone = "Asia/Shanghai", endTimezone = null, allDay = false, rrule = rrule, rdate = null, exrule = null,
        exdate = null, location = "3F", notes = "Agenda", status = 1, originalId = null, originalInstanceTime = null,
        originalAllDay = null, organizer = null, availability = null, accessLevel = null, guestsCanInviteOthers = null,
        guestsCanModify = null, guestsCanSeeGuests = null, calendarName = "Work",
        ownerAccount = null, calendarAccess = 700,
    )

    /** Each card as the registry would show it: prepared from fake provider data, then only formatted. */
    private fun cards(): List<Pair<String, PhoneConfirmationText>> {
        val longNotes = "Bring the printed budget, the signed vendor forms and the updated hiring plan. ".repeat(5)
        val calendar = FakeCalendarBackend().also { fake ->
            fake.events[1] = record(1, "Weekly sync", null)
            val series = record(2, "Standup", "FREQ=DAILY;COUNT=10")
            fake.events[2] = series.copy(start = ms("2026-11-01T07:00:00Z"))
            fake.instances[2] = (0 until 10).map {
                val begin = ms("2026-11-01T07:00:00Z") + it * 86_400_000L
                CalendarInstance(begin, begin + 3_600_000L)
            }
        }
        val context = ApplicationProvider.getApplicationContext<Application>()
        val tools = CalendarTools(context, calendar, zone)
        fun json(vararg pairs: Pair<String, String>) = buildJsonObject { for ((k, v) in pairs) put(k, v) }
        val contacts = FakeContactsBackend().also {
            it.current = ContactSnapshot(5, 50, ContactFields("Ann", "Lee", "Acme", "", listOf("+1 555 0100"),
                listOf("ann@example.com")), 100, 101,
                listOf(ContactEntryRow(110, LabeledEntry("+1 555 0100", "mobile"))),
                listOf(ContactEntryRow(120, LabeledEntry("ann@example.com", "work"))))
        }
        val contactTools = ContactsTool(context, contacts)
        val newContact: PhoneConfirmationText = { res ->
            ContactEdit.plan(null, buildJsonObject {
                put("given_name", "Ann"); put("family_name", "Lee"); put("company", "Acme")
                put("phones", buildJsonArray { add(JsonPrimitive("555 0100")) })
            }).createSummary(res)
        }
        return listOf(
            "event-change" to tools.prepareUpdate(json("id" to "1", "start" to "2026-11-04T17:00:00+08:00", "location" to "")).text,
            "event-change-this" to tools.prepareUpdate(json("id" to "2", "occurrence_start" to "2026-11-04T15:00:00+08:00",
                "title" to "Standup (moved)", "notes" to longNotes)).text,
            "event-change-future" to tools.prepareUpdate(json("id" to "2", "occurrence_start" to "2026-11-04T15:00:00+08:00",
                "span" to "future", "start" to "2026-11-04T16:30:00+08:00")).text,
            "event-delete-future" to tools.prepareDelete(json("id" to "2", "occurrence_start" to "2026-11-04T15:00:00+08:00",
                "span" to "future")).text,
            "contact-replace" to contactTools.prepareUpdate(buildJsonObject {
                put("id", "5"); put("company", "Beta Industries International Holdings"); put("replace", true)
                put("phones", buildJsonArray { add(JsonPrimitive("555 0200")); add(JsonPrimitive("555 0201")) })
            }).text,
            "contact-new" to newContact,
        )
    }

    private fun shots(language: String, allow: String) {
        val app = ApplicationProvider.getApplicationContext<Application>()
        val connection = Proxy.newProxyInstance(HubConnection::class.java.classLoader, arrayOf(HubConnection::class.java)) { _, method, _ ->
            when (method.name) {
                "getState" -> MutableStateFlow(ConnectionState.WaitingForNetwork)
                "getEvents" -> emptyFlow<Any>()
                else -> error("not used: ${method.name}")
            }
        } as HubConnection
        val transport = object : PhoneToolsTransport {
            override val calls = emptyFlow<PhoneToolCall>()
            override fun discardPending() = Unit
            override fun register(deviceId: String, deviceName: String, simulator: Boolean, tools: List<PhoneToolSpec>) = true
            override fun result(callId: String, result: PhoneToolResult) = true
        }
        val runtime = PhoneToolsRuntime(app, HubScope("hub-cards-$language", "one"), "device", connection, transport, scope)
        runtime.setForeground(true)
        var dark by mutableStateOf(false)
        var fontScale by mutableStateOf(1f)
        rule.setContent {
            val density = LocalDensity.current
            CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale)) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = AppStyle.Glass) {
                    PhoneConfirmationHost(runtime.confirmations)
                }
            }
        }
        for ((name, text) in cards()) {
            val asking = scope.launch { runtime.confirmations.confirm(text) }
            for ((suffix, isDark, scale) in listOf(Triple("light", false, 1f), Triple("dark", true, 1f), Triple("light-xl", false, 2f))) {
                dark = isDark; fontScale = scale
                rule.waitForIdle()
                rule.onNode(hasText(allow) and hasAnyAncestor(isDialog())).assertExists()
                capture(rule.onNode(isDialog()).captureToImage().asAndroidBitmap(), "calendar-contacts-$name-$language-$suffix")
            }
            asking.cancel()
            rule.waitForIdle()
        }
        runtime.close()
    }

    private fun capture(bitmap: Bitmap, name: String) =
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
}
