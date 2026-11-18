@file:OptIn(androidx.compose.material3.adaptive.ExperimentalMaterial3AdaptiveApi::class)

package persona.android.ui

import android.app.Application
import android.content.ComponentName
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.material3.adaptive.layout.PaneExpansionState
import androidx.compose.material3.adaptive.layout.rememberPaneExpansionState
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.hapticfeedback.HapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows
import org.robolectric.annotation.Config
import persona.android.navigation.PaneHinge
import persona.core.navigation.AppTab

@RunWith(AndroidJUnit4::class)
@Config(application = Application::class, sdk = [35])
class PaneSplitterMemoryTest {
    @get:Rule val rule = createEmptyComposeRule()

    @Test fun onlyUserCollapseAndExpandChangeNotifies() {
        val app = ApplicationProvider.getApplicationContext<Application>()
        Shadows.shadowOf(app.packageManager).addActivityIfNotPresent(ComponentName(app, ComponentActivity::class.java))
        lateinit var state: PaneExpansionState
        lateinit var scope: CoroutineScope
        val notifications = mutableListOf<Boolean>()
        val splitter = PaneSplitter(
            AppTab.Agents,
            null,
            collapsible = true,
            initialCollapsed = true,
            onCollapsedChange = { notifications += it },
        ).apply { width = 1000f; detailOpen = true }
        val haptics = object : HapticFeedback {
            override fun performHapticFeedback(hapticFeedbackType: HapticFeedbackType) = Unit
        }

        ActivityScenario.launch(ComponentActivity::class.java).use { scenario ->
            scenario.onActivity { activity ->
                activity.setContent {
                    state = rememberPaneExpansionState()
                    scope = rememberCoroutineScope()
                }
            }
            rule.runOnIdle {
                assertTrue(splitter.collapsed)
                assertTrue(notifications.isEmpty())

                // Placement / policy / width / detail changes must not notify.
                splitter.rest(state)
                splitter.width = 800f
                splitter.rest(state)
                splitter.hinges = listOf(PaneHinge(400f, 400f, separating = false))
                splitter.rest(state)
                splitter.detailOpen = false
                splitter.rest(state)
                splitter.collapseEnabled = false
                splitter.rest(state)
                assertTrue(splitter.collapsed)
                assertTrue(notifications.isEmpty())

                // Restore collapsible detail and test drag without crossing expand threshold.
                splitter.collapseEnabled = true
                splitter.detailOpen = true
                splitter.width = 1000f
                splitter.hinges = emptyList()
                splitter.rest(state)
                splitter.start()
                splitter.drag(state, 50f, haptics)
                assertTrue(notifications.isEmpty())
                splitter.stop(state, 0f, scope)
                assertTrue(splitter.collapsed)
                assertTrue(notifications.isEmpty())

                // Expanding via button/TalkBack notifies once.
                splitter.expand(state)
                splitter.expand(state)
                assertFalse(splitter.collapsed)
                assertEquals(listOf(false), notifications)

                // Resizing drag while open does not notify.
                splitter.start()
                splitter.drag(state, 40f, haptics)
                assertEquals(listOf(false), notifications)
                splitter.stop(state, 0f, scope)
                assertFalse(splitter.collapsed)
                assertEquals(listOf(false), notifications)

                // Collapsing via drag far left notifies once.
                splitter.start()
                splitter.drag(state, -350f, haptics)
                assertEquals(listOf(false), notifications)
                splitter.stop(state, 0f, scope)
                assertTrue(splitter.collapsed)
                assertEquals(listOf(false, true), notifications)

                // Collapse again is a no-op; dragging back open notifies once.
                splitter.collapse(state)
                assertEquals(listOf(false, true), notifications)
                splitter.start()
                splitter.drag(state, 320f, haptics)
                splitter.stop(state, 0f, scope)
                assertFalse(splitter.collapsed)
                assertEquals(listOf(false, true, false), notifications)
            }
        }
    }
}
