package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.runtime.*
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.Density
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.*
import java.io.File

/**
 * UI version 8's dashboard page (#9158) drawn on the JVM with Robolectric's native graphics, as the mock hub's made-up
 * "Today" page (scripts/mock/mock_hub.py, MOCK_EXTENSION_COLUMNS=1): a phone (one column, the stack), a foldable or a
 * portrait tablet (two columns) and a landscape tablet (three), light and dark, every style at the tablet's width and
 * the largest text (200 %, Android 14's largest). Written to `build/extension-columns-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35])
class ExtensionColumnsShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun row(key: String, title: String, subtitle: String = "", value: String? = null, icon: String? = null, color: String? = null,
                    avatar: String? = null, badge: ExtensionViewBadge? = null) =
        ExtensionViewItem("row", key = key, title = title, subtitle = subtitle.ifEmpty { null }, value = value, icon = icon, iconColor = color,
            avatar = avatar?.let { ExtensionViewAvatar(text = it) }, badge = badge, action = ExtensionViewAction("open", values = mapOf("row" to key)))

    private fun card(key: String, title: String, icon: String, color: String, link: String, summary: String, rows: List<ExtensionViewItem>,
                     more: String? = null, span: String? = null, columns: Int? = null) = ExtensionViewSection(
        key = key, title = title, icon = icon, iconColor = color, span = span, columns = columns,
        link = ExtensionViewSectionLink(link, ExtensionViewAction("page", push = ExtensionViewPush(key, title))),
        items = listOf(ExtensionViewItem("text", key = "$key.summary", text = summary, style = "secondary")) + rows +
            listOfNotNull(more?.let { ExtensionViewItem("row", key = "$key.more", title = it, action = ExtensionViewAction("page", push = ExtensionViewPush(key, it))) }))

    /** The mock hub's "Today" page; every name is invented. */
    private val today = ExtensionView(title = "Today", layout = "columns", sections = listOf(
        card("needs", "Needs me", "bell", "orange", "All", "4 things wait for you · 1 due today", listOf(
            row("need.travel", "Approve the travel request", "Approval · from Robin", icon = "check", color = "blue", badge = ExtensionViewBadge("Today", "caution")),
            row("need.review", "Reply about the design review", "Chat · Kai asked 2 h ago", icon = "chat", color = "teal"),
            row("need.agent", "Agent waits for a permission", "Running Agents · release notes", icon = "robot", color = "purple"),
            row("need.report", "Sign the quarterly report", "Document · due Friday", icon = "doc", color = "gray"),
        ), span = "full", columns = 2),
        card("calendar", "Calendar", "calendar", "red", "Calendar", "3 events today · next at 14:00", listOf(
            row("cal.sync", "Weekly sync", "Room 4 · 6 people", "14:00"),
            row("cal.robin", "1:1 with Robin", "Video call", "16:30"),
            row("cal.release", "Release review", "Room 2", "17:30"),
        ), "3 more this week"),
        card("lark", "Lark today", "chat", "blue", "Lark", "12 unread in 4 chats", listOf(
            row("lark.design", "Design team", "Kai: the new cards look good", avatar = "DT", badge = ExtensionViewBadge("5")),
            row("lark.release", "Release", "Build 812 is out", avatar = "RE", badge = ExtensionViewBadge("2")),
        ), "2 more chats"),
        card("wechat", "WeChat", "chat", "green", "WeChat", "3 unread · 1 mention", listOf(
            row("wechat.family", "Family", "Mom: dinner at 7?", avatar = "FA", badge = ExtensionViewBadge("2")),
            row("wechat.club", "Climbing club", "Saturday trip is on", avatar = "CC", badge = ExtensionViewBadge("1")),
        ), "1 more chat"),
        card("agents", "Running Agents", "robot", "purple", "Agents", "2 running · 1 waits for you", listOf(
            row("agent.notes", "Release notes", "Waits for a permission", badge = ExtensionViewBadge("Needs you", "caution")),
            row("agent.tests", "Flaky test hunt", "Running · 12 min"),
            row("agent.docs", "Docs sweep", "Running · 3 min"),
        ), "1 finished today"),
    ))

    private val api = object : ExtensionApiStub() {
        override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?): ExtensionView = today
    }

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/extension-columns-shots").also { it.mkdirs() }

    /** Draws the page in each [looks] (style, dark, font scale) and saves it as `today-<width>-<look>.png`. */
    private fun shoot(width: String, looks: List<Triple<AppStyle, Boolean, Float>>) {
        var look by mutableStateOf(looks.first())
        rule.setContent {
            val density = LocalDensity.current
            CompositionLocalProvider(LocalDensity provides Density(density.density, look.third)) {
                PersonaTheme(style = look.first, darkTheme = look.second, reduceMotion = true) {
                    ExtensionUiPage(api, "today", "today", "Today", {})
                }
            }
        }
        val sums = mutableMapOf<Triple<AppStyle, Boolean, Float>, Long>()
        for (l in looks) {
            look = l
            // the first card is on screen at every width and text size (at 200 % the last ones are below it)
            rule.waitUntil(5_000) { rule.onAllNodesWithText("Approve the travel request").fetchSemanticsNodes().isNotEmpty() }
            rule.waitForIdle()
            val bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
            val background = bitmap.getPixel(1, 1)
            var other = 0
            var sum = 0L
            for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) {
                val pixel = bitmap.getPixel(x, y)
                sum += pixel
                if (pixel != background) other++
            }
            sums[l] = sum
            val name = buildString {
                append("today-$width-${l.first.name.lowercase()}-${if (l.second) "dark" else "light"}")
                if (l.third > 1f) append("-large")
            }
            File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            assertTrue("$name drew nothing", other > 50)
        }
        val glass = sums.filterKeys { it.first == AppStyle.Glass && it.third == 1f }
        if (glass.size == 2) assertNotEquals("light and dark differ", glass.values.first(), glass.values.last())
    }

    private val lightAndDark = listOf(Triple(AppStyle.Glass, false, 1f), Triple(AppStyle.Glass, true, 1f))

    @Test @Config(qualifiers = "en-w400dp-h2000dp-xhdpi")
    fun phone() = shoot("400", lightAndDark + Triple(AppStyle.Glass, false, 2f))

    @Test @Config(qualifiers = "en-w800dp-h1400dp-hdpi")
    fun foldable() = shoot("800", lightAndDark)

    @Test @Config(qualifiers = "en-w1280dp-h1100dp-hdpi")
    fun tablet() = shoot("1280", lightAndDark + listOf(Triple(AppStyle.Brief, false, 1f), Triple(AppStyle.Board, false, 1f),
        Triple(AppStyle.Board, true, 1f), Triple(AppStyle.Glass, false, 2f)))
}
