package persona.core.designsystem

import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color

/** Monitoring series colors, independent of agent states, interface style and theme accent. */
@Immutable
data class MetricPalette(
    val cpu: Color,
    val mem: Color,
    val rx: Color,
    val tx: Color,
    val load: Color,
    val disk: Color,
)

object MetricColor {
    private val light = MetricPalette(
        cpu = Tokens.Metric.Light.cpu,
        mem = Tokens.Metric.Light.mem,
        rx = Tokens.Metric.Light.rx,
        tx = Tokens.Metric.Light.tx,
        load = Tokens.Metric.Light.load,
        disk = Tokens.Metric.Light.disk,
    )
    private val dark = MetricPalette(
        cpu = Tokens.Metric.Dark.cpu,
        mem = Tokens.Metric.Dark.mem,
        rx = Tokens.Metric.Dark.rx,
        tx = Tokens.Metric.Dark.tx,
        load = Tokens.Metric.Dark.load,
        disk = Tokens.Metric.Dark.disk,
    )

    fun of(dark: Boolean): MetricPalette = if (dark) this.dark else light

    val current: MetricPalette
        @Composable get() = of(PersonaTheme.darkTheme)
}
