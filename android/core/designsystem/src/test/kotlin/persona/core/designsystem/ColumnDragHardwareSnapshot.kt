package persona.core.designsystem

import android.graphics.Bitmap
import android.graphics.Picture
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.layer.GraphicsLayer

/**
 * Robolectric's public `toImageBitmap` draws into a software bitmap and drops a child `RenderEffect`.
 * This is `LayerSnapshotV28`: `Bitmap.createBitmap(Picture)` draws the layer on a hardware canvas, which keeps the
 * effect. `GraphicsLayer.draw` is internal, so the call stays in test source. Release R8 does not keep that method.
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

/** `GraphicsLayer.draw` is internal. The API 28 snapshot calls the same method. */
private val graphicsLayerDraw = GraphicsLayer::class.java.getMethod(
    "draw\$ui_graphics",
    androidx.compose.ui.graphics.Canvas::class.java,
    GraphicsLayer::class.java,
)

private fun GraphicsLayer.drawOnto(canvas: android.graphics.Canvas) {
    graphicsLayerDraw.invoke(this, androidx.compose.ui.graphics.Canvas(canvas), null)
}
