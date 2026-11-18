package persona.feature.machines

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.MetricPoint
import persona.core.designsystem.PersonaTheme

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class MetricChartsLocalizationTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    @Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
    @Test fun englishEmptyAndUnavailableSelection() = verifyLocalizedChart(
        noSample = "No samples at this time",
        empty = "No monitoring data for this period yet",
        backToNow = "Back to now",
        hint = "Long-press a chart to see the values at one moment",
    )

    @Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-xhdpi")
    @Test fun simplifiedChineseEmptyAndUnavailableSelection() = verifyLocalizedChart(
        noSample = "这个时段没有采样",
        empty = "这段时间还没有监控数据",
        backToNow = "回到现在",
        hint = "长按曲线查看同一时刻的数值",
    )

    @Config(sdk = [35], qualifiers = "ja-w400dp-h900dp-xhdpi")
    @Test fun japaneseEmptyAndUnavailableSelection() = verifyLocalizedChart(
        noSample = "この時刻のサンプルはありません",
        empty = "この期間の監視データはまだありません",
        backToNow = "現在に戻る",
        hint = "グラフを長押しすると同じ時刻の値を表示します",
    )

    private fun verifyLocalizedChart(noSample: String, empty: String, backToNow: String, hint: String) {
        assertEquals(backToNow, rule.activity.getString(R.string.machines_back_to_now))
        assertEquals(hint, rule.activity.getString(R.string.machines_long_press_hint))
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                MetricChart(
                    title = "CPU",
                    points = listOf(MetricPoint(900)),
                    kinds = listOf(MetricKind.Cpu),
                    nowValues = listOf(null),
                    end = 1_000,
                    span = 3_600,
                    step = 10,
                    selection = 900,
                    onSelect = {},
                )
            }
        }
        rule.onNodeWithText(noSample).assertExists()
        rule.onNodeWithText(empty).assertExists()
    }
}
