package persona.feature.machines

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.MetricPoint
import persona.core.contracts.MetricRange
import persona.core.designsystem.MetricColor
import persona.core.designsystem.Tokens

class MetricSelectionTest {
    @Test fun seriesColorsUseTheRegisteredMetricTokens() {
        val light = MetricColor.of(false)
        assertEquals(Tokens.Metric.Light.cpu, MetricKind.Cpu.color(light))
        assertEquals(Tokens.Metric.Light.mem, MetricKind.Memory.color(light))
        assertEquals(Tokens.Metric.Light.rx, MetricKind.Rx.color(light))
        assertEquals(Tokens.Metric.Light.tx, MetricKind.Tx.color(light))
        assertEquals(Tokens.Metric.Light.load, MetricKind.Load.color(light))
        assertEquals(Tokens.Metric.Light.disk, MetricKind.Disk.color(light))
        assertEquals(6, listOf(light.cpu, light.mem, light.rx, light.tx, light.load, light.disk).toSet().size)

        val dark = MetricColor.of(true)
        assertEquals(Tokens.Metric.Dark.cpu, MetricKind.Cpu.color(dark))
        assertEquals(Tokens.Metric.Dark.mem, MetricKind.Memory.color(dark))
        assertEquals(Tokens.Metric.Dark.rx, MetricKind.Rx.color(dark))
        assertEquals(Tokens.Metric.Dark.tx, MetricKind.Tx.color(dark))
        assertEquals(Tokens.Metric.Dark.load, MetricKind.Load.color(dark))
        assertEquals(Tokens.Metric.Dark.disk, MetricKind.Disk.color(dark))
        assertNotEquals(light, dark)
    }

    @Test fun aTimestampSurvivesRefreshAndRangesThatStillContainIt() {
        val now = 10_000L
        val selected = now - 1_800
        assertEquals(selected, retainedMetricSelection(selected, now, MetricRange.Hour.seconds))
        assertEquals(selected, retainedMetricSelection(selected, now + 10, MetricRange.Hour.seconds))
        assertEquals(selected, retainedMetricSelection(selected, now, MetricRange.SixHours.seconds))
        assertEquals(selected, retainedMetricSelection(selected, now, MetricRange.Day.seconds))
        assertNull(retainedMetricSelection(selected, now, 900))
        assertNull(retainedMetricSelection(selected, now, 0))
        assertNull(retainedMetricSelection(null, now, MetricRange.Hour.seconds))
    }

    @Test fun anUnavailableSeriesDoesNotBorrowAnEmptyPointOrCrossAGap() {
        assertNull(selectedPoint(listOf(MetricPoint(110)), 110, 10) { it.cpu != null })
        assertNull(selectedPoint(emptyList(), 110, 10) { it.cpu != null })

        val missing = listOf(MetricPoint(100, cpu = 10.0), MetricPoint(110), MetricPoint(120, cpu = 20.0))
        assertNull(selectedPoint(missing, 105, 10) { it.cpu != null })
        assertNull(selectedPoint(missing, 110, 10) { it.cpu != null })
        assertNull(selectedPoint(missing, 115, 10) { it.cpu != null })

        val contiguous = listOf(MetricPoint(100, cpu = 10.0), MetricPoint(110, cpu = 15.0), MetricPoint(120, cpu = 20.0))
        assertEquals(100L, selectedPoint(contiguous, 105, 10) { it.cpu != null }?.t)
        assertEquals(110L, selectedPoint(contiguous, 114, 10) { it.cpu != null }?.t)
        assertEquals(120L, selectedPoint(contiguous, 116, 10) { it.cpu != null }?.t)
        val gap = listOf(MetricPoint(100, cpu = 10.0), MetricPoint(110), MetricPoint(130, gap = true))
        assertNull(selectedPoint(gap, 110, 10) { it.cpu != null })
    }
}
