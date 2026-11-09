package persona.core.designsystem

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Typeface
import android.util.LruCache
import android.view.View
import android.view.ViewGroup
import android.webkit.JavascriptInterface
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import androidx.annotation.VisibleForTesting
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import kotlin.math.sqrt
import kotlin.math.ceil
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/**
 * Formulas (KaTeX) and diagrams (Mermaid) drawn into pictures (docs/android/android.md "File viewer" › "Formulas and diagrams", iOS `RenderedMarkdown.swift`):
 * one hidden WebView loads the bundled `assets/render/render.html` (its CSP lets in only the bundle's scripts, styles and
 * fonts; every other request is answered empty), draws one thing at a time and is captured into a bitmap. What to draw
 * goes in as an argument of `renderMath` / `renderMermaid`, never as markup. Pictures are kept by (kind, source,
 * dark, size) in memory and in the cache directory. Formulas are drawn black on transparent and tinted by the caller
 * (one picture for light and dark); diagrams are drawn once per appearance with Mermaid's own themes. The page loads
 * KaTeX with itself and Mermaid (5.5 MB) only on the first diagram (`render.js` `loadMermaid`), so drawing formulas
 * never waits for it; when it cannot load, that diagram fails with Mermaid's missing-name error as before.
 */
object MarkdownRenderer {
    enum class Kind { InlineMath, DisplayMath, Mermaid }

    /**
     * Why something was not drawn: KaTeX's or Mermaid's own words when the source is wrong ([source] true, kept so it is
     * not drawn again), or what went wrong around it (the page, the window, the time; tried again next time).
     */
    class Failure(message: String, val source: Boolean = false) : Exception(message)

    /** Part of every key: bumped when the drawing changes (the bundled scripts, the page), so old pictures go. */
    const val VERSION = "1"

    /** Diagrams wider than this (CSS px, i.e. dp) are cut at it. */
    private const val MAX_WIDTH = 2048

    /** At most this many pixels a picture (32 MB): a bigger one is drawn at a lower resolution. */
    private const val MAX_PIXELS = 8_000_000.0

    private const val PAGE = "file:///android_asset/render/render.html"
    private const val BUNDLE = "file:///android_asset/render/"

    private val images = object : LruCache<String, Bitmap>(48 shl 20) {
        override fun sizeOf(key: String, value: Bitmap) = value.byteCount
    }
    private val failures = LruCache<String, String>(256)
    private val lock = Mutex()
    private var web: WebView? = null
    private var loaded: CompletableDeferred<Unit>? = null
    private val answers = ConcurrentHashMap<Int, CompletableDeferred<Result<String>>>()
    private var next = 0

    /** [points]: the text size in CSS px (dp, with the font scale); [density]: pixels a dp. */
    fun key(kind: Kind, source: String, dark: Boolean, points: Float, density: Float): String {
        val id = listOf(VERSION, kind.name, source, if (kind == Kind.Mermaid && dark) "dark" else "",
            (points * 10).roundToInt().toString(), (density * 100).roundToInt().toString()).joinToString("\n")
        return MessageDigest.getInstance("SHA-256").digest(id.toByteArray()).take(16).joinToString("") { "%02x".format(it) }
    }

    fun cached(kind: Kind, source: String, dark: Boolean, points: Float, density: Float): Bitmap? =
        images.get(key(kind, source, dark, points, density))

    /**
     * The picture, drawn when no copy is kept; throws [Failure] with KaTeX's or Mermaid's message when the source is
     * wrong. [host] is a view in the window the hidden WebView joins (a WebView outside a window draws nothing).
     */
    suspend fun image(host: View, kind: Kind, source: String, dark: Boolean, points: Float): Bitmap {
        val density = host.resources.displayMetrics.density
        val key = key(kind, source, dark, points, density)
        images.get(key)?.let { return it }
        failures.get(key)?.let { throw Failure(it, source = true) }
        val file = File(File(host.context.cacheDir, "markdown-render"), "$key.png")
        withContext(Dispatchers.IO) { if (file.isFile) BitmapFactory.decodeFile(file.path) else null }?.let {
            images.put(key, it)
            return it
        }
        val bitmap = try {
            lock.withLock {
                // Drawn while this one waited (the same formula twice in a message).
                images.get(key)?.let { return it }
                withContext(Dispatchers.Main) { draw(host, kind, source, dark, points, density) }
            }
        } catch (e: Failure) {
            if (e.source) failures.put(key, e.message.orEmpty())
            throw e
        }
        images.put(key, bitmap)
        withContext(Dispatchers.IO) {
            runCatching {
                file.parentFile?.mkdirs()
                val part = File.createTempFile(key, ".part", file.parentFile)
                part.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
                if (!part.renameTo(file)) part.delete()
            }
        }
        return bitmap
    }

    /** For tests: a picture as if drawn, or ([bitmap] null) KaTeX's or Mermaid's [error] for the source. */
    @VisibleForTesting
    fun put(kind: Kind, source: String, dark: Boolean, points: Float, density: Float, bitmap: Bitmap?, error: String = "") {
        val key = key(kind, source, dark, points, density)
        if (bitmap != null) images.put(key, bitmap) else failures.put(key, error)
    }

    /** An inline formula KaTeX could not read: its source in monospace, black on transparent like a drawn formula. */
    fun source(tex: String, points: Float, density: Float): Bitmap {
        val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            typeface = Typeface.MONOSPACE
            textSize = points * density
            color = android.graphics.Color.BLACK
        }
        val text = tex.replace('\n', ' ')
        val metrics = paint.fontMetrics
        val width = max(1, ceil(paint.measureText(text)).toInt())
        val height = max(1, ceil(metrics.descent - metrics.ascent).toInt())
        return Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888).also {
            Canvas(it).drawText(text, 0f, -metrics.ascent, paint)
        }
    }

    private suspend fun draw(host: View, kind: Kind, source: String, dark: Boolean, points: Float, density: Float): Bitmap {
        val web = page(host)
        val call = when (kind) {
            Kind.Mermaid -> "renderMermaid(${JSONObject.quote(source)}, $dark, $points)"
            else -> "renderMath(${JSONObject.quote(source)}, ${kind == Kind.DisplayMath}, $points)"
        }
        val answer = withTimeoutOrNull(20_000) { evaluate(web, call) } ?: run {
            // Still drawing: a fresh page, so what it draws late can't end up in the next picture.
            discard()
            throw Failure(host.context.getString(R.string.designsystem_render_too_slow))
        }
        val json = answer.getOrElse {
            web.evaluateJavascript("clearOut()", null)
            throw Failure(it.message ?: host.context.getString(R.string.designsystem_render_nothing), source = true)
        }
        val box = runCatching { JSONArray(json) }.getOrNull()?.takeIf { it.length() == 4 } ?: throw Failure(host.context.getString(R.string.designsystem_render_nothing))
        val x = max(0.0, box.getDouble(0)); val y = max(0.0, box.getDouble(1))
        val w = min(box.getDouble(2), MAX_WIDTH.toDouble()); val h = min(box.getDouble(3), MAX_WIDTH * 4.0)
        if (w <= 0 || h <= 0) throw Failure(host.context.getString(R.string.designsystem_render_nothing))
        // The page must be as large as what was drawn: the capture covers only the WebView's own area.
        val needW = ceil((x + w) * density).toInt(); val needH = ceil((y + h) * density).toInt()
        if (web.width < needW || web.height < needH) {
            web.layoutParams = FrameLayout.LayoutParams(max(web.width, needW), max(web.height, needH))
            web.requestLayout()
        }
        painted(web)
        val scale = min(1.0, sqrt(MAX_PIXELS / (w * density * h * density)))
        val bitmap = Bitmap.createBitmap(max(1, ceil(w * density * scale).toInt()), max(1, ceil(h * density * scale).toInt()), Bitmap.Config.ARGB_8888)
        Canvas(bitmap).apply {
            scale(scale.toFloat(), scale.toFloat())
            translate((-x * density).toFloat(), (-y * density).toFloat())
        }.let { web.draw(it) }
        web.evaluateJavascript("clearOut()", null)
        return bitmap
    }

    /** Takes the WebView out of its window (it is destroyed there); the next drawing loads a new one. */
    private fun discard() {
        val current = web ?: return
        web = null; loaded = null
        (current.parent as? ViewGroup)?.removeView(current) ?: current.destroy()
    }

    /** Waits until what the page drew last is on the WebView (a capture before that comes out blank). */
    private suspend fun painted(web: WebView) {
        val done = CompletableDeferred<Unit>()
        web.postVisualStateCallback(0, object : WebView.VisualStateCallback() {
            override fun onComplete(requestId: Long) { done.complete(Unit) }
        })
        withTimeoutOrNull(2_000) { done.await() }
    }

    /** Runs an async function of the page and waits for what it resolves to (JSON) or its error's message. */
    private suspend fun evaluate(web: WebView, call: String): Result<String> {
        val id = next++
        val answer = CompletableDeferred<Result<String>>()
        answers[id] = answer
        web.evaluateJavascript("""$call.then(function (r) { PersonaRender.done($id, JSON.stringify(r)); },
            function (e) { PersonaRender.fail($id, String((e && e.message) || e)); });""", null)
        return try { answer.await() } finally { answers.remove(id) }
    }

    private object Bridge {
        @JavascriptInterface fun done(id: Int, json: String) { answers[id]?.complete(Result.success(json)) }
        @JavascriptInterface fun fail(id: Int, message: String) {
            answers[id]?.complete(Result.failure(Exception(message.removePrefix("Error: ").removePrefix("ParseError: "))))
        }
    }

    /**
     * The WebView, made and loaded once per window: behind everything in the window's root, fully transparent (the
     * capture draws it regardless), out of touch and accessibility.
     */
    @SuppressLint("SetJavaScriptEnabled", "JavascriptInterface")
    private suspend fun page(host: View): WebView {
        // The activity's own window, also for Markdown in a dialog, a sheet or a popup (their roots come and go, and a
        // popup's root takes no views).
        val activity = host.context.activity() ?: throw Failure(host.context.getString(R.string.designsystem_render_no_window))
        val root = activity.window?.decorView as? FrameLayout ?: throw Failure(activity.getString(R.string.designsystem_render_no_window))
        web?.let { current ->
            if (current.isAttachedToWindow && current.parent === root) { loaded?.await(); return current }
            discard()
        }
        val ready = CompletableDeferred<Unit>()
        val view = WebView(activity).apply {
            alpha = 0f
            isClickable = false
            isFocusable = false
            importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS
            setBackgroundColor(android.graphics.Color.TRANSPARENT)
            settings.javaScriptEnabled = true
            settings.allowFileAccess = false
            settings.allowContentAccess = false
            settings.blockNetworkLoads = true
            settings.domStorageEnabled = false
            settings.setSupportMultipleWindows(false)
            settings.textZoom = 100 // sizes come in already scaled
            addJavascriptInterface(Bridge, "PersonaRender")
            webViewClient = object : WebViewClient() {
                override fun shouldInterceptRequest(view: WebView?, request: WebResourceRequest?): WebResourceResponse? {
                    val url = request?.url?.toString().orEmpty()
                    if (url.startsWith(BUNDLE) && !url.contains("..")) return null
                    return WebResourceResponse("text/plain", "utf-8", ByteArrayInputStream(ByteArray(0)))
                }
                override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?) = true
                override fun onPageFinished(view: WebView?, url: String?) { ready.complete(Unit) }
                override fun onReceivedError(view: WebView?, request: WebResourceRequest?, error: android.webkit.WebResourceError?) {
                    if (request?.isForMainFrame == true) ready.completeExceptionally(Failure(activity.getString(R.string.designsystem_render_page_missing)))
                }
            }
        }
        // Gone with its window (the activity's): the next drawing makes one in the new window.
        view.addOnAttachStateChangeListener(object : View.OnAttachStateChangeListener {
            override fun onViewAttachedToWindow(v: View) = Unit
            override fun onViewDetachedFromWindow(v: View) {
                if (web === view) { web = null; loaded = null }
                view.destroy()
            }
        })
        // Laid out as large as a formula's line or a diagram can be, so drawing one rarely has to resize the page.
        val px = host.resources.displayMetrics.density
        root.addView(view, 0, FrameLayout.LayoutParams(((MAX_WIDTH + 128) * px).toInt(), (1024 * px).toInt()))
        web = view
        loaded = ready
        view.loadUrl(PAGE)
        val ok = runCatching { withTimeoutOrNull(20_000) { ready.await() } }.getOrNull() != null
        if (!ok) {
            discard()
            throw Failure(activity.getString(R.string.designsystem_render_page_not_loaded))
        }
        return view
    }

    private tailrec fun Context.activity(): Activity? = when (this) {
        is Activity -> this
        is ContextWrapper -> baseContext.activity()
        else -> null
    }
}
