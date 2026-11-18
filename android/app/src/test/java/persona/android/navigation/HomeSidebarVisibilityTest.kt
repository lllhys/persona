package persona.android.navigation

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class HomeSidebarVisibilityTest {
    @Test fun columnOutLeavesOutPageButtonWithRoomAndShowsColumnFoldButton() {
        assertEquals(
            HomeLeadingAction.HiddenSpacer,
            resolveHomeLeadingAction(
                assistantRoot = true,
                regular = true,
                columnsOver = false,
                sidebarColumnShown = true,
            ),
        )
        assertTrue(showsSidebarColumnFoldButton(regular = true, columnsOver = false))
    }

    @Test fun columnFoldedBringsBackPageButtonToReopenColumn() {
        assertEquals(
            HomeLeadingAction.ShowColumn,
            resolveHomeLeadingAction(
                assistantRoot = true,
                regular = true,
                columnsOver = false,
                sidebarColumnShown = false,
            ),
        )
    }

    @Test fun floatingOverlayAndCompactKeepPageButtonAndNeverShowColumnFoldButton() {
        for (columnShown in listOf(false, true)) {
            assertEquals(
                HomeLeadingAction.ToggleOverlay,
                resolveHomeLeadingAction(
                    assistantRoot = true,
                    regular = true,
                    columnsOver = true,
                    sidebarColumnShown = columnShown,
                ),
            )
            assertEquals(
                HomeLeadingAction.OpenDrawer,
                resolveHomeLeadingAction(
                    assistantRoot = true,
                    regular = false,
                    columnsOver = false,
                    sidebarColumnShown = columnShown,
                ),
            )
        }
        assertFalse(showsSidebarColumnFoldButton(regular = true, columnsOver = true))
        assertFalse(showsSidebarColumnFoldButton(regular = false, columnsOver = false))
    }

    @Test fun agentsTabAndDrawerNeverToggleTheColumn() {
        for (regular in listOf(false, true)) {
            for (columnsOver in listOf(false, true)) {
                for (columnShown in listOf(false, true)) {
                    assertEquals(
                        HomeLeadingAction.OpenDrawer,
                        resolveHomeLeadingAction(
                            assistantRoot = false,
                            regular = regular,
                            columnsOver = columnsOver,
                            sidebarColumnShown = columnShown,
                        ),
                    )
                }
            }
        }
    }
}
