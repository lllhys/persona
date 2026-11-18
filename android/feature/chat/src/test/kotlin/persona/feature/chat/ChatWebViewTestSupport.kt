package persona.feature.chat

import android.content.Context
import android.webkit.RenderProcessGoneDetail
import org.robolectric.Shadows.shadowOf

/**
 * Robolectric does not implement the WebView implementation's feature probes (algorithmic darkening, prerendering, audio
 * muting), so tests fix them: nothing is supported, which makes every page load with a plain `loadUrl`.
 */
internal val NoWebFeatures = ChatWebFeatures { false }

/**
 * A WebView that records what the pool does to it. Its history and navigation state are whatever a test sets, so a fresh
 * instance always has none: tests built on it prove which instance the pool hands out and the calls it makes, not what a
 * real renderer keeps (history, cookies, cache); that is checked on a device (`chat-link-reopen.json`).
 */
internal class RecordingWebView(context: Context) : ChatSelectionWebView(context) {
    val loads = mutableListOf<String>()
    var destroyCalls = 0
    var reloadCalls = 0
    var cacheClearCalls = 0
    var back = false
    var forward = false

    override fun loadUrl(url: String) {
        loads += url
        currentUrl = url
    }

    override fun reload() {
        reloadCalls++
    }

    override fun destroy() {
        destroyCalls++
    }

    override fun clearCache(includeDiskFiles: Boolean) {
        cacheClearCalls++
    }

    override fun canGoBack() = back

    override fun canGoForward() = forward
}

/** The pool ignores the detail, but the client's parameter is non-null, so a real one is passed. */
internal fun simulateChatRendererGone(view: ChatSelectionWebView) {
    val detail = object : RenderProcessGoneDetail() {
        override fun didCrash() = true
        override fun rendererPriorityAtExit() = 0
    }
    // The client the pool installed, as Robolectric's WebView shadow keeps it.
    shadowOf(view).webViewClient.onRenderProcessGone(view, detail)
}
