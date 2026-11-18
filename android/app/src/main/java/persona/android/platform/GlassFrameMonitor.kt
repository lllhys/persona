package persona.android.platform

import android.app.Activity
import android.hardware.display.DisplayManager
import android.os.Handler
import android.os.HandlerThread
import android.util.Log
import android.view.FrameMetrics
import android.view.Window
import persona.core.designsystem.GlassLevel
import persona.core.designsystem.GlassThrottle

/**
 * Lowers the glass style one step (refraction → blur → opaque) when drawing it keeps missing frames.
 * Frames only arrive while something moves, so the window is mostly scrolling and animation. Severe sustained jank
 * lowers after 120 frames; the looser 240-frame threshold keeps a few slow frames while a page opens from costing the effect.
 */
object GlassFrameMonitor {
    private const val WINDOW = 240
    private const val EARLY_WINDOW = 120
    private const val EARLY_JANK_SHARE = 0.60
    private const val JANK_SHARE = 0.30

    internal fun shouldLower(frames: Int, janky: Int): Boolean {
        if (frames < EARLY_WINDOW) return false
        val share = janky.toDouble() / frames
        return share > EARLY_JANK_SHARE || (frames >= WINDOW && share > JANK_SHARE)
    }

    private fun budgetNs(refreshRate: Float) = 1_000_000_000L / refreshRate.toLong().coerceAtLeast(30)

    /** One frame window. A display-rate change starts fresh; a missed callback is caught at the next window. */
    internal class FrameWindow(refreshRate: Float) {
        private var budget = budgetNs(refreshRate)
        private var frames = 0
        private var janky = 0

        fun displayChanged(refreshRate: Float) = updateBudget(refreshRate)

        fun add(durationNs: Long, refreshRate: Float, level: GlassLevel = GlassLevel.Liquid): Boolean {
            if (level == GlassLevel.Solid) return false
            if (frames == 0) updateBudget(refreshRate)
            frames++
            if (durationNs > budget * 3 / 2) janky++
            val lower = shouldLower(frames, janky)
            if (lower || frames >= WINDOW) clear()
            return lower
        }

        private fun updateBudget(refreshRate: Float) {
            val updated = budgetNs(refreshRate)
            if (updated == budget) return
            budget = updated
            clear()
        }

        private fun clear() {
            frames = 0
            janky = 0
        }
    }

    fun attach(activity: Activity, deviceLevel: () -> GlassLevel, enabled: () -> Boolean) {
        val thread = HandlerThread("glass-frames").apply { start() }
        val handler = Handler(thread.looper)
        val window = FrameWindow(activity.display?.refreshRate ?: 60f)
        val displays = activity.getSystemService(DisplayManager::class.java)
        displays?.registerDisplayListener(object : DisplayManager.DisplayListener {
            override fun onDisplayAdded(displayId: Int) = Unit
            override fun onDisplayRemoved(displayId: Int) = Unit
            override fun onDisplayChanged(displayId: Int) {
                if (displayId == activity.display?.displayId) {
                    displays.getDisplay(displayId)?.refreshRate?.let(window::displayChanged)
                }
            }
        }, handler)
        val listener = Window.OnFrameMetricsAvailableListener { _, metrics, _ ->
            if (!enabled()) return@OnFrameMetricsAvailableListener
            if (metrics.getMetric(FrameMetrics.FIRST_DRAW_FRAME) == 1L) return@OnFrameMetricsAvailableListener
            val level = maxOf(deviceLevel(), GlassThrottle.ceiling.value)
            val lower = window.add(
                metrics.getMetric(FrameMetrics.TOTAL_DURATION),
                activity.display?.refreshRate ?: 60f,
                level,
            )
            if (lower) {
                activity.runOnUiThread {
                    val device = deviceLevel()
                    if (device != GlassLevel.Solid) GlassThrottle.lower(device)
                }
                Log.i("PersonaGlass", "glass lowered by the current frame window")
            }
        }
        activity.window.addOnFrameMetricsAvailableListener(listener, handler)
    }
}
