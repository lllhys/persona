package persona.android.ui

import android.graphics.Bitmap
import android.graphics.Picture
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.layer.GraphicsLayer

/**
 * Same snapshot as `ColumnDragHardwareSnapshot` in the design-system tests. The app test module cannot see that
 * source set. Production calls public `GraphicsLayer.toImageBitmap()`; this reflection stays out of the release.
 */
internal suspend fun columnDragHardwareSnapshot(layer: GraphicsLayer): ImageBitmap {
    val width = layer.size.width
    val height = layer.size.height
    val picture = object : Picture() {
        override fun getWidth(): Int = width
        override fun getHeight(): Int = height
        override fun requiresHardwareAcceleration(): Boolean = true
        override fun beginRecording(width: Int, height: Int): android.graphics.Canvas = android.graphics.Canvas()
        override fun endRecording() = Unit
        override fun draw(canvas: android.graphics.Canvas) {
            layer.drawOnto(canvas)
        }
    }
    val rendered = Bitmap.createBitmap(picture)
    val soft = if (rendered.config == Bitmap.Config.ARGB_8888) rendered
        else rendered.copy(Bitmap.Config.ARGB_8888, false)
    return soft.asImageBitmap()
}

private val graphicsLayerDraw = GraphicsLayer::class.java.getMethod(
    "draw\$ui_graphics",
    androidx.compose.ui.graphics.Canvas::class.java,
    GraphicsLayer::class.java,
)

private fun GraphicsLayer.drawOnto(canvas: android.graphics.Canvas) {
    graphicsLayerDraw.invoke(this, androidx.compose.ui.graphics.Canvas(canvas), null)
}

/** The seam is internal to the design-system module. */
internal fun installColumnDragHardwareSnapshot(on: Boolean) {
    val holder = Class.forName("persona.core.designsystem.SidebarColumnDragKt")
    val setter = holder.methods.first {
        it.name.startsWith("setColumnDragSnapshotForTest") && it.parameterCount == 1
    }
    val shot: (suspend (GraphicsLayer) -> ImageBitmap?)? = if (on) ::columnDragHardwareSnapshot else null
    setter.invoke(null, shot)
    val hooks = holder.methods.first {
        it.name.startsWith("setColumnDragTestHooks") && it.parameterCount == 1
    }
    hooks.invoke(null, on)
}
