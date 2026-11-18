package persona.feature.chat

import android.content.Context
import android.webkit.CookieManager
import android.webkit.WebSettings
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/**
 * What the pool does with its views: which instance it hands out, when it destroys, creates or refuses one, and which calls
 * it makes. The views are [RecordingWebView]s, so in-page history, cookies and the disk cache are not exercised here; the
 * device scene `chat-link-reopen.json` and its helper check those.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ChatWebViewPoolTest {
    private val link = "https://example.invalid/articles/original"
    private val next = "https://example.invalid/articles/linked"
    private val dev = "http://127.0.0.1:4173/preview"
    private val context: Context get() = ApplicationProvider.getApplicationContext()
    private val created = mutableListOf<RecordingWebView>()
    private lateinit var pool: ChatWebViewPool

    @Before fun setUp() {
        CookieManager.getInstance().setCookie(link, "session=kept")
        pool = ChatWebViewPool(context, NoWebFeatures) { viewContext ->
            RecordingWebView(viewContext).also(created::add)
        }
    }

    @After fun sharedDataSurvivesEveryScenario() {
        // Robolectric's in-memory cookie store: this only shows the pool never clears it, not that a real store persists.
        assertEquals("session=kept", CookieManager.getInstance().getCookie(link))
        assertTrue(created.all { it.cacheClearCalls == 0 })
        pool.destroy()
    }

    @Test fun releaseDestroysTheShownViewAndPreloadsAReplacementInstanceForTheOriginalUrl() {
        pool.preload(link)
        val preloaded = created.single()
        assertEquals(listOf(link), preloaded.loads)

        val shown = pool.acquire(link) as RecordingWebView
        assertSame(preloaded, shown)
        pool.release(shown)

        val fresh = pool.existing(link) as RecordingWebView
        assertNotSame(shown, fresh)
        assertEquals(1, shown.destroyCalls)
        assertEquals(listOf(link), fresh.loads)
        assertSame(fresh, pool.acquire(link))
        assertEquals(listOf(link), fresh.loads)
    }

    @Test fun theReplacementInstanceStartsFromTheOriginalUrlWhateverTheOldViewHadShown() {
        // The recording view reports no history for any new instance: this shows a new instance loading only the link's own url.
        val shown = pool.acquire(link) as RecordingWebView
        shown.loadUrl(next)
        shown.back = true
        shown.forward = true
        shown.currentUrl = next
        shown.pageTitle = "Linked page"
        shown.failure = "Earlier failure"
        shown.loadProgress = 47

        pool.release(shown)

        val fresh = pool.existing(link) as RecordingWebView
        assertNotSame(shown, fresh)
        assertEquals(listOf(link), fresh.loads)
        assertEquals(link, fresh.currentUrl)
        assertFalse(fresh.canGoBack())
        assertFalse(fresh.canGoForward())
        assertEquals("", fresh.pageTitle)
        assertNull(fresh.failure)
        assertEquals(0, fresh.loadProgress)
    }

    @Test fun repeatedReleaseIsIdempotentAndKeepsTheReplacementPreload() {
        val shown = pool.acquire(link) as RecordingWebView
        pool.release(shown)
        val fresh = pool.existing(link) as RecordingWebView

        pool.release(shown)
        pool.release(shown)

        assertEquals(1, shown.destroyCalls)
        assertEquals(0, fresh.destroyCalls)
        assertSame(fresh, pool.existing(link))
        assertEquals(listOf(link), fresh.loads)
    }

    @Test fun releaseAfterMemoryTrimCannotDeleteANewPreload() {
        val shown = pool.acquire(link) as RecordingWebView
        pool.acquire("https://example.invalid/other")
        pool.trimMemory()
        assertEquals(1, shown.destroyCalls)

        pool.preload(link)
        val fresh = pool.existing(link) as RecordingWebView
        pool.release(shown)

        assertEquals(1, shown.destroyCalls)
        assertEquals(0, fresh.destroyCalls)
        assertSame(fresh, pool.existing(link))
    }

    @Test fun releaseAfterRendererLossCannotDeleteTheRetryView() {
        val shown = pool.acquire(link) as RecordingWebView
        simulateChatRendererGone(shown)
        assertEquals(1, shown.destroyCalls)

        val retry = pool.acquire(link) as RecordingWebView
        pool.release(shown)

        assertNotSame(shown, retry)
        assertSame(retry, pool.existing(link))
        assertEquals(0, retry.destroyCalls)
        assertEquals(listOf(link), retry.loads)
    }

    @Test fun releaseAndPreloadKeepTheViewCapAndNeverTrimTheActiveView() {
        val activeUrl = "https://example.invalid/active"
        val active = pool.acquire(activeUrl) as RecordingWebView
        pool.activate(active)
        repeat(7) { index -> pool.preload("https://example.invalid/preload/$index") }

        assertTrue(created.count { it.destroyCalls == 0 } <= 4)
        assertEquals(0, active.destroyCalls)
        assertSame(active, pool.existing(activeUrl))

        pool.release(active)
        assertTrue(created.count { it.destroyCalls == 0 } <= 4)
        assertEquals(1, active.destroyCalls)
        assertNotSame(active, pool.existing(activeUrl))
    }

    @Test fun activePortForgetReloadsWithoutCreatingOrPreloadingAnotherView() {
        pool.forget(dev)
        val shown = pool.acquire(dev) as RecordingWebView
        pool.activate(shown)
        shown.loadProgress = 100
        val count = created.size

        pool.forget(dev)
        assertSame(shown, pool.existing(dev))
        assertEquals(1, shown.reloadCalls)
        assertEquals(0, shown.destroyCalls)
        assertEquals(count, created.size)

        pool.release(shown)
        assertSame(shown, pool.existing(dev))
        assertEquals(count, created.size)
        assertEquals(0, shown.destroyCalls)
    }

    @Test fun inactivePortForgetCreatesFreshOnNextOpenAndKeepsNoCacheAcrossRelease() {
        pool.forget(dev)
        val shown = pool.acquire(dev) as RecordingWebView
        pool.activate(shown)
        val elsewhere = pool.acquire("https://example.invalid/elsewhere") as RecordingWebView
        pool.activate(elsewhere)

        pool.forget(dev)
        assertEquals(1, shown.destroyCalls)
        assertNull(pool.existing(dev))
        pool.release(shown)

        val fresh = pool.acquire(dev) as RecordingWebView
        assertNotSame(shown, fresh)
        assertEquals(WebSettings.LOAD_NO_CACHE, fresh.settings.cacheMode)
        assertEquals(listOf(dev), fresh.loads)
        val count = created.size

        pool.release(fresh)
        assertSame(fresh, pool.existing(dev))
        assertEquals(count, created.size)
        assertEquals(0, fresh.destroyCalls)
    }

    @Test fun loopbackReleasedAsAnOrdinaryLinkIsDestroyedAndNeverPreloaded() {
        // With a port, without one, and the other loopback spellings: the phone's own address is never loaded ahead.
        listOf("http://localhost:5173/app", "http://localhost/", "http://127.0.0.1/", "https://[::1]/page", "http://dev.localhost/").forEach { local ->
            val shown = pool.acquire(local) as RecordingWebView
            val count = created.size

            pool.release(shown)

            assertEquals(local, 1, shown.destroyCalls)
            assertNull(local, pool.existing(local))
            assertEquals(local, count, created.size)
        }
    }

    @Test fun preloadRefusesALoopbackAddressWithOrWithoutAPort() {
        listOf("http://localhost/", "http://localhost:3000/", "http://127.0.0.1/x").forEach { pool.preload(it) }
        assertTrue(created.isEmpty())
        pool.preload(link)
        assertEquals(1, created.size)
    }

    @Test fun aClosedPoolCreatesNothingAndABeforeCloseReleaseOnlyFindsItAlreadyDestroyed() {
        val shown = pool.acquire(link) as RecordingWebView
        pool.destroy()
        val count = created.size
        assertEquals(1, shown.destroyCalls)

        assertNull(pool.acquire(next))
        pool.preload(next)
        pool.release(shown)

        assertEquals(count, created.size)
        assertEquals(1, shown.destroyCalls)
        assertNull(pool.existing(link))
        assertNull(pool.existing(next))
    }

    @Test fun releaseBeforeCloseBuildsAReplacementAndCloseDestroysIt() {
        // What a host teardown does when its page releases before the pool closes; the host declares the close to run first.
        val shown = pool.acquire(link) as RecordingWebView
        pool.release(shown)
        val replacement = pool.existing(link) as RecordingWebView
        pool.destroy()
        assertEquals(1, replacement.destroyCalls)
        assertNull(pool.existing(link))
    }

    @Test fun openingAnotherLinkNeverTrimsAViewTheHostStillHolds() {
        val other = "https://example.invalid/other"
        val first = pool.acquire(link) as RecordingWebView
        pool.activate(first)
        val preloads = (1..3).map { "https://example.invalid/preload/$it" }
        preloads.forEach { pool.preload(it) }

        // The card shows `other` while `link` is still held (it is released after `other` is acquired).
        val second = pool.acquire(other) as RecordingWebView
        assertEquals(0, first.destroyCalls)
        assertSame(first, pool.existing(link))
        // The oldest preload went instead.
        assertEquals(1, created[1].destroyCalls)

        pool.release(first)
        val warmLink = pool.existing(link) as RecordingWebView
        assertNotSame(first, warmLink)
        assertEquals(0, warmLink.destroyCalls)
        pool.activate(second)
        pool.release(second)

        // Reopening the first link finds the warm replacement made when it was released; nothing new is created.
        val count = created.size
        assertSame(warmLink, pool.acquire(link))
        assertEquals(count, created.size)
        assertEquals(0, warmLink.destroyCalls)
    }

    @Test fun uncommittedAcquisitionsStayBoundedAndNeverEvictTheCommittedPage() {
        val committed = pool.acquire(link) as RecordingWebView
        pool.activate(committed)
        repeat(3) { index -> pool.preload("https://example.invalid/preload/$index") }

        val uncommitted = (0 until 12).map { index ->
            val candidate = pool.acquire("https://example.invalid/pending/$index") as RecordingWebView
            assertEquals(0, committed.destroyCalls)
            assertSame(committed, pool.existing(link))
            assertTrue(created.count { it.destroyCalls == 0 } <= 4)
            candidate
        }

        // Only the newest pending view is protected; earlier compositions may have been abandoned and become evictable again.
        assertTrue(uncommitted.dropLast(1).any { it.destroyCalls == 1 })
        assertEquals(0, uncommitted.last().destroyCalls)
        pool.trimMemory()
        assertEquals(0, committed.destroyCalls)
        assertEquals(0, uncommitted.last().destroyCalls)
    }

    @Test fun theReplacementKeepsTheSamePageConfigurationAndTheStoreIsNeverCleared() {
        val shown = pool.acquire(link) as RecordingWebView
        pool.release(shown)
        val fresh = pool.existing(link) as RecordingWebView

        assertTrue(fresh.settings.javaScriptEnabled)
        assertTrue(fresh.settings.domStorageEnabled)
        assertFalse(fresh.settings.allowFileAccess)
        assertFalse(fresh.settings.allowContentAccess)
        assertEquals(WebSettings.LOAD_CACHE_ELSE_NETWORK, fresh.settings.cacheMode)
        // Robolectric always answers false here and does not record the setter; the device scene checks real cookies.
        assertEquals("session=kept", CookieManager.getInstance().getCookie(link))
    }
}
