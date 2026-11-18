package persona.feature.settings

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.click
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
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
 * Extensions' UI slots drawn on the JVM with Robolectric's native graphics (no emulator): the "Extensions" section of
 * Settings › Extensions (a card, nav pages with badges) and a nav page (segmented, number grid, stacked bars, lines, a
 * chart with a picked point), in every style, light and dark. Each is checked to draw something and written as a PNG to
 * `build/extension-ui-shots/` (or `-Dpersona.shots=<dir>`) to look at (docs/android/android.md "Extension UI and marketplace").
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h2000dp-xhdpi")
class ExtensionSlotsShotsTest {
    @get:Rule val rule = createComposeRule()

    private val days = listOf("10-29", "10-30", "10-31", "11-01", "11-02", "11-03", "11-04")
    private fun series(name: String?, tone: String?, values: List<Double>, unit: String = "M") =
        ExtensionChartSeries(name, tone, days.zip(values) { d, v -> ExtensionChartPoint(d, v, "%.1f$unit".format(v)) })

    private val card = ExtensionView(sections = listOf(ExtensionViewSection(items = listOf(
        ExtensionViewItem("stat_grid", stats = listOf(ExtensionStat("今天", "1.5M", "-56% 比昨天", "positive"),
            ExtensionStat("近 7 天", "17.0M", "+12% 比上周", "caution"), ExtensionStat("费用", "¥36.20"))),
        ExtensionViewItem("chart", kind = "bar", unit = "M tokens", series = listOf(series("tokens", null, listOf(2.2, 3.1, 1.1, 0.7, 4.0, 3.5, 1.5)))),
    ))))

    private val page = ExtensionView(title = "用量", state = "range=7d", refreshAfter = 600, sections = listOf(
        ExtensionViewSection(items = listOf(ExtensionViewItem("segmented", name = "range", value = "7d", action = ExtensionViewAction("range"),
            choices = listOf(ExtensionViewChoice("1d", "今天"), ExtensionViewChoice("7d", "7 天"), ExtensionViewChoice("30d", "30 天"), ExtensionViewChoice("90d", "90 天"))))),
        ExtensionViewSection(title = "概况", items = listOf(
            ExtensionViewItem("stat_grid", stats = listOf(ExtensionStat("tokens", "17.0M", "+12% 比上周", "caution"), ExtensionStat("费用", "¥36.20", "+8% 比上周", "caution"),
                ExtensionStat("请求", "1,204"), ExtensionStat("缓存命中", "62%", tone = "positive"))),
            ExtensionViewItem("a_newer_kind", title = "这一项这个 App 不认识，跳过"))),
        ExtensionViewSection(title = "按天", footer = "按你的时区分天。", items = listOf(
            ExtensionViewItem("chart", kind = "bar", unit = "M tokens", stacked = true,
                series = listOf(series("输入", null, listOf(1.8, 2.4, 0.9, 0.6, 3.1, 2.7, 1.2)), series("输出", null, listOf(0.4, 0.7, 0.2, 0.1, 0.9, 0.8, 0.3)))),
            ExtensionViewItem("chart", kind = "bar", title = "输入和输出（并排）", unit = "M tokens",
                series = listOf(series("输入", null, listOf(1.8, 2.4, 0.9, 0.6, 3.1, 2.7, 1.2)), series("输出", "positive", listOf(0.4, 0.7, 0.2, 0.1, 0.9, 0.8, 0.3)))),
            ExtensionViewItem("chart", kind = "line", title = "费用", unit = "元", detail = "含缓存折扣",
                series = listOf(series("费用", "caution", listOf(3.1, 5.2, 2.0, 1.1, 8.3, 7.6, 3.5), unit = " 元"))))),
        ExtensionViewSection(title = "近 30 天", items = listOf(ExtensionViewItem("chart", kind = "line", unit = "次",
            series = listOf(ExtensionChartSeries("请求", null, (1..30).map { ExtensionChartPoint("10-%02d".format(it), (40 + (it * 37) % 90).toDouble()) }),
                ExtensionChartSeries("失败", "danger", (1..30).map { ExtensionChartPoint("10-%02d".format(it), ((it * 7) % 12).toDouble()) }))))),
        ExtensionViewSection(title = "按模型", items = listOf(
            ExtensionViewItem("row", title = "model-large", subtitle = "1,021 次", value = "12.4M"),
            ExtensionViewItem("row", title = "model-small", subtitle = "183 次", value = "4.6M", badge = ExtensionViewBadge("新", "positive")))),
    ))

    private val slots = ExtensionSlotsState(available = true, places = listOf(
        ExtensionUiPlace("example-gateway", "today", "今天的用量", "overview_card", extensionName = "Example 网关面板", size = "compact", opens = "usage"),
        ExtensionUiPlace("example-gateway", "usage", "用量", "nav_page", extensionName = "Example 网关面板", icon = "chart", badge = ExtensionViewBadge("3", "danger")),
        ExtensionUiPlace("example-alerts", "alerts", "告警", "nav_page", icon = "bell", badge = ExtensionViewBadge("新", "caution")),
        ExtensionUiPlace("example-notes", "notes", "一个标题很长很长很长很长很长很长的笔记入口", "nav_page", icon = "rocket"),
    ))

    private val handle = ExtensionViewHandle({ _, _ -> }, { _, _ -> }, {}, {})

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/plugin-ui-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
        // Something besides the background was drawn.
        val background = bitmap.getPixel(1, 1)
        var other = 0
        for (y in 0 until bitmap.height step 7) for (x in 0 until bitmap.width step 7) if (bitmap.getPixel(x, y) != background) other++
        assertTrue("$name drew nothing", other > 100)
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Test fun drawsTheSettingsSectionAndANavPageInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var home by mutableStateOf(true)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background)) {
                    LazyColumn(Modifier.fillMaxWidth(), contentPadding = PaddingValues(horizontal = Tokens.Space.l, vertical = Tokens.Space.l)) {
                        if (home) {
                            extensionSlotItems(slots, listOf(ExtensionCardSlot(slots.cards.single(), ExtensionViewState(card), handle) {}),
                                selected = { false }, onOpen = {})
                            item { JSectionHeader("连接") }
                            item { JGroupItem(GroupPosition.Single) { JRow("配对", subtitle = "扫码、粘贴链接或手动输入") } }
                        } else {
                            extensionViewItems("page", ExtensionViewState(page, values = ExtensionViewText.fields(page)), handle)
                            item {
                                JChart(ExtensionViewText.chart(page.sections[2].items[0])!!, Modifier.testTag("picked").padding(top = Tokens.Space.l),
                                    title = "点一下：显示这一天的数")
                            }
                        }
                    }
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            home = true
            capture("plugin-settings-${s.name.lowercase()}-${if (d) "dark" else "light"}")
            home = false
            rule.waitForIdle()
            rule.onNodeWithTag("picked").performTouchInput { click(androidx.compose.ui.geometry.Offset(width * 0.65f, height * 0.6f)) }
            capture("plugin-nav-page-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
    }

}
