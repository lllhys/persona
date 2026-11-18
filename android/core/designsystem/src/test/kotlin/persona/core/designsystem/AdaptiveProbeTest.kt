package persona.core.designsystem

import android.content.Context
import android.content.pm.ApplicationInfo
import android.os.SystemClock
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-mdpi")
class AdaptiveProbeTest {
    @get:Rule val rule = createComposeRule()
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val file = File(context.noBackupFilesDir, "hub-data/adaptive-probe.json")

    @After fun tearDown() = AdaptiveProbe.disable()

    private fun enable() {
        context.applicationInfo.flags = context.applicationInfo.flags or ApplicationInfo.FLAG_DEBUGGABLE
        org.robolectric.shadows.ShadowProcess.setPid(1234)
        AdaptiveProbe.enable(context)
    }

    private fun snapshot() = decodeAdaptiveProbeSnapshot(
        file.readText(), SystemClock.elapsedRealtimeNanos(), 10_000_000_000L)

    @Test fun disabledProbeHasNoFileOrListener() {
        AdaptiveProbe.disable()
        AdaptiveProbe.publishList("Agents", AdaptiveListEvidence("session", 12))
        AdaptiveProbe.publishComposer("chat:one", androidx.compose.ui.text.input.TextFieldValue("secret"), emptyList())
        assertFalse(file.exists())
        assertFalse(AdaptiveProbe.enabled)
    }

    @Test fun enableWritesAtomicallyWithoutDraftText() {
        enable()
        AdaptiveProbe.publishComposer("chat:one", androidx.compose.ui.text.input.TextFieldValue("private draft"), listOf("a" to "Uploaded"))
        AdaptiveProbe.publishComposerFocus("chat:one", true)
        val snapshot = assertNotNullSnapshot()
        val composer = snapshot.pages.getValue("chat:one").composer!!
        assertEquals(64, composer.draftHmac!!.length)
        assertEquals(true, composer.focused)
        assertEquals(AdaptiveAttachmentEvidence("a", "Uploaded"), composer.attachments!!.single())
        assertFalse(file.readText().contains("private draft"))
        assertFalse(File(file.path + ".bak").exists())
    }

    @Test fun pagesKeepTheirOwnStableKeys() {
        enable()
        AdaptiveProbe.publishList("Agents", AdaptiveListEvidence("session-a", 10))
        AdaptiveProbe.publishList("Settings", AdaptiveListEvidence("notifications", 20))
        AdaptiveProbe.removeList("Agents")
        val pages = assertNotNullSnapshot().pages
        assertFalse(pages.containsKey("Agents"))
        assertEquals("notifications", pages.getValue("Settings").list!!.firstVisibleKey)
    }

    @Test fun staleAndCorruptSnapshotsAreNotEvidence() {
        enable()
        AdaptiveProbe.publishList("Agents", AdaptiveListEvidence("session-a", 10))
        val valid = assertNotNullSnapshot()
        val now = SystemClock.elapsedRealtimeNanos()
        assertNull(decodeAdaptiveProbeSnapshot("not json", now, 1000))
        assertNull(decodeAdaptiveProbeSnapshot(file.readText(), valid.updatedAtNanos + 1001, 1000))
        assertNull(decodeAdaptiveProbeSnapshot(file.readText().replace("\"fieldVersion\":1", "\"fieldVersion\":99"), now, 1000))
    }

    @Test fun disableDeletesAndStopsLaterWrites() {
        enable()
        AdaptiveProbe.publishList("Agents", AdaptiveListEvidence("session-a", 10))
        assertTrue(file.exists())
        AdaptiveProbe.disable()
        assertFalse(file.exists())
        AdaptiveProbe.publishList("Agents", AdaptiveListEvidence("session-b", 20))
        assertFalse(file.exists())
    }

    @Test fun equalDraftsHaveDifferentHmacsAcrossEnableSessions() {
        enable()
        val value = androidx.compose.ui.text.input.TextFieldValue("same draft")
        AdaptiveProbe.publishComposer("chat:one", value, emptyList())
        val first = assertNotNullSnapshot().pages.getValue("chat:one").composer!!.draftHmac!!
        AdaptiveProbe.disable()
        enable()
        AdaptiveProbe.publishComposer("chat:one", value, emptyList())
        val second = assertNotNullSnapshot().pages.getValue("chat:one").composer!!.draftHmac!!
        assertNotEquals(first, second)
    }

    @Test fun boundsStayWithTheirPageWhenListAndComposerEvidenceChanges() {
        enable()
        val bounds = AdaptiveBoundsEvidence(
            content = AdaptiveRectEvidence(0, 0, 600, 400),
            hinge = AdaptiveRectEvidence(0, 400, 600, 440),
        )
        AdaptiveProbe.publishBounds("Settings", bounds)
        AdaptiveProbe.publishList("Settings", AdaptiveListEvidence("notifications", 20))
        AdaptiveProbe.publishComposer("Settings", androidx.compose.ui.text.input.TextFieldValue("draft"), emptyList())
        val page = assertNotNullSnapshot().pages.getValue("Settings")
        assertEquals(bounds, page.bounds)
        assertEquals("notifications", page.list!!.firstVisibleKey)
        assertEquals(400, page.bounds!!.content.bottom)
        assertEquals(400, page.bounds!!.hinge!!.top)
    }

    @Test fun machineGridPublishesActualColumnsForAShortLastRow() {
        enable()
        AdaptiveProbe.publishList("Machines", AdaptiveListEvidence("machine-row|1:only", 0))
        AdaptiveProbe.publishMachineGridColumns("Machines", 3)
        val list = assertNotNullSnapshot().pages.getValue("Machines").list!!
        assertEquals("machine-row|1:only", list.firstVisibleKey)
        assertEquals(3, list.columns)
        assertTrue(isAdaptiveChromeKey("usage-title"))
        assertTrue(isAdaptiveChromeKey("usage-0-account"))
        assertTrue(isAdaptiveChromeKey("loading"))
        assertTrue(isAdaptiveChromeKey("failure"))
    }

    @Test fun listProbeUsesFirstBusinessItemKeyAndPixelOffset() {
        enable()
        lateinit var listState: LazyListState
        rule.setContent {
            listState = rememberLazyListState()
            AdaptiveListProbe("Agents", listState)
            LazyColumn(state = listState) {
                item(key = "page-title") { Box(Modifier.height(40.dp)) }
                item(key = "usage-title") { Box(Modifier.height(40.dp)) }
                item(key = "usage-0-account") { Box(Modifier.height(40.dp)) }
                item(key = "loading") { Box(Modifier.height(40.dp)) }
                item(key = "session-stable") { Box(Modifier.height(80.dp)) }
            }
        }
        rule.waitUntil(timeoutMillis = 3_000) {
            runCatching { assertNotNullSnapshot().pages["Agents"]?.list?.firstVisibleKey == "session-stable" }.getOrDefault(false)
        }
        assertEquals(160, assertNotNullSnapshot().pages.getValue("Agents").list!!.firstVisibleOffsetPx)
    }

    private fun assertNotNullSnapshot(): AdaptiveProbeSnapshot {
        val value = snapshot()
        assertNotNull(file.readText(), value)
        return value!!
    }
}
