package persona.android.platform

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.designsystem.GlassLevel
import persona.core.designsystem.GlassThrottle

class GlassFrameMonitorTest {
    @Test fun severeJankLowersEarlyWithoutWeakeningTheNormalWindow() {
        assertFalse(GlassFrameMonitor.shouldLower(frames = 119, janky = 72))
        assertFalse(GlassFrameMonitor.shouldLower(frames = 120, janky = 72))
        assertTrue(GlassFrameMonitor.shouldLower(frames = 120, janky = 73))
        assertFalse(GlassFrameMonitor.shouldLower(frames = 239, janky = 72))
        assertFalse(GlassFrameMonitor.shouldLower(frames = 240, janky = 72))
        assertTrue(GlassFrameMonitor.shouldLower(frames = 240, janky = 73))
    }

    @Test fun refreshRateDropClearsTheWindowAndAcceptsNormal60HzFrames() {
        val window = GlassFrameMonitor.FrameWindow(refreshRate = 120f)
        repeat(119) { assertFalse(window.add(durationNs = 17_000_000L, refreshRate = 120f)) }

        window.displayChanged(refreshRate = 60f)

        repeat(240) { assertFalse(window.add(durationNs = 17_000_000L, refreshRate = 60f)) }
    }

    @Test fun nextWindowReadsTheRefreshRateWhenTheDisplayCallbackWasMissed() {
        val window = GlassFrameMonitor.FrameWindow(refreshRate = 120f)
        repeat(240) { assertFalse(window.add(durationNs = 8_000_000L, refreshRate = 120f)) }

        repeat(240) { assertFalse(window.add(durationNs = 17_000_000L, refreshRate = 60f)) }
    }

    @Test fun realSlowFramesStillLowerAtTheExistingThreshold() {
        val window = GlassFrameMonitor.FrameWindow(refreshRate = 60f)
        repeat(72) { assertFalse(window.add(durationNs = 34_000_000L, refreshRate = 60f)) }
        repeat(47) { assertFalse(window.add(durationNs = 17_000_000L, refreshRate = 60f)) }

        assertTrue(window.add(durationNs = 34_000_000L, refreshRate = 60f))
    }

    @Test fun solidEffectiveLevelDoesNotCountFrames() {
        val window = GlassFrameMonitor.FrameWindow(refreshRate = 60f)

        repeat(240) {
            assertFalse(window.add(durationNs = 34_000_000L, refreshRate = 60f, level = GlassLevel.Solid))
        }
        repeat(119) { assertFalse(window.add(durationNs = 17_000_000L, refreshRate = 60f)) }
    }

    @Test fun loweringStartsAtTheEffectiveDeviceLevel() {
        val previous = GlassThrottle.ceiling.value
        try {
            GlassThrottle.ceiling.value = GlassLevel.Liquid
            GlassThrottle.lower(deviceLevel = GlassLevel.Frosted)
            assertEquals(GlassLevel.Solid, GlassThrottle.ceiling.value)

            GlassThrottle.ceiling.value = GlassLevel.Liquid
            GlassThrottle.lower(deviceLevel = GlassLevel.Liquid)
            assertEquals(GlassLevel.Frosted, GlassThrottle.ceiling.value)
        } finally {
            GlassThrottle.ceiling.value = previous
        }
    }
}
