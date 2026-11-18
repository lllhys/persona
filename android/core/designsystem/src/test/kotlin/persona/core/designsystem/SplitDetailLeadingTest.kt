package persona.core.designsystem

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w840dp-h600dp-mdpi")
class SplitDetailLeadingTest {
    @get:Rule val rule = createComposeRule()

    @Test fun resolvesAllFourCases() {
        val back: () -> Unit = {}
        val show: () -> Unit = {}

        assertNull(resolveDetailLeading(SplitDetailContext(isFirstPage = true, listHidden = true, onShowList = show), onBack = null))
        assertEquals(DetailLeading.Hidden, resolveDetailLeading(SplitDetailContext(isFirstPage = true, listHidden = false, onShowList = show), onBack = back))
        assertEquals(DetailLeading.ShowList(show), resolveDetailLeading(SplitDetailContext(isFirstPage = true, listHidden = true, onShowList = show), onBack = back))
        assertEquals(DetailLeading.Back(back), resolveDetailLeading(SplitDetailContext(isFirstPage = true, listHidden = true, onShowList = null), onBack = back))
        assertEquals(DetailLeading.Back(back), resolveDetailLeading(SplitDetailContext(isFirstPage = false, listHidden = false, onShowList = show), onBack = back))
        assertEquals(DetailLeading.Back(back), resolveDetailLeading(SplitDetailContext(isFirstPage = false, listHidden = true, onShowList = show), onBack = back))
        assertEquals(DetailLeading.Back(back), resolveDetailLeading(null, onBack = back))
    }

    @Test fun firstPageBesideListHidesBackAcrossHeaders() {
        var backs = 0
        var shows = 0
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalSplitDetail provides SplitDetailContext(isFirstPage = true, listHidden = false, onShowList = { shows++ })) {
                    Column(Modifier.fillMaxWidth().background(PersonaTheme.palette.background)) {
                        ChatTopBar(title = "Weather widget", onBack = { backs++ })
                        ChatSlimBar(title = "Weather widget", openLabel = "Open header", onOpen = {}, onBack = { backs++ })
                        JTopBar(title = "Space", onBack = { backs++ })
                    }
                }
            }
        }
        rule.onAllNodesWithContentDescription("Back").assertCountEquals(0)
        rule.onAllNodesWithContentDescription("Sidebar").assertCountEquals(0)
        assertEquals(0, backs)
        assertEquals(0, shows)
    }

    @Test fun firstPageCollapsedShowsSidebarButtonAndInvokesShowList() {
        var backs = 0
        var shows = 0
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalSplitDetail provides SplitDetailContext(isFirstPage = true, listHidden = true, onShowList = { shows++ })) {
                    Column(Modifier.fillMaxWidth().background(PersonaTheme.palette.background)) {
                        ChatTopBar(title = "Weather widget", onBack = { backs++ })
                        ChatSlimBar(title = "Weather widget", openLabel = "Open header", onOpen = {}, onBack = { backs++ })
                        JTopBar(title = "Space", onBack = { backs++ })
                    }
                }
            }
        }
        rule.onAllNodesWithContentDescription("Back").assertCountEquals(0)
        val sidebarButtons = rule.onAllNodesWithContentDescription("Sidebar")
        sidebarButtons.assertCountEquals(3)
        repeat(3) { index -> sidebarButtons[index].performClick() }
        rule.runOnIdle {
            assertEquals(3, shows)
            assertEquals(0, backs)
        }
    }

    @Test fun pushedPageAndCompactLayoutKeepBack() {
        var pushedBacks = 0
        var compactBacks = 0
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Column(Modifier.fillMaxWidth().background(PersonaTheme.palette.background)) {
                    CompositionLocalProvider(LocalSplitDetail provides SplitDetailContext(isFirstPage = false, listHidden = true, onShowList = { error("Must not show list on pushed page") })) {
                        ChatTopBar(title = "Pushed", onBack = { pushedBacks++ })
                    }
                    CompositionLocalProvider(LocalSplitDetail provides null) {
                        ChatTopBar(title = "Compact", onBack = { compactBacks++ })
                    }
                }
            }
        }
        val backButtons = rule.onAllNodesWithContentDescription("Back")
        backButtons.assertCountEquals(2)
        rule.onAllNodesWithContentDescription("Sidebar").assertCountEquals(0)
        backButtons[0].performClick()
        backButtons[1].performClick()
        rule.runOnIdle {
            assertTrue(pushedBacks == 1 && compactBacks == 1)
        }
    }
}
