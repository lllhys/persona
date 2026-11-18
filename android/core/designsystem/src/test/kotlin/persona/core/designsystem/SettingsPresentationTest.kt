package persona.core.designsystem

import androidx.compose.ui.unit.dp
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SettingsPresentationTest {
    @Test fun compactAndWidePhoneKeepTheFullWindowSheet() {
        assertEquals(SettingsPresentation.FullWindow, settingsPresentation(largeWindow = false))
    }

    @Test fun regularAndExpandedUseThePagePresentation() {
        assertEquals(SettingsPresentation.PageSized, settingsPresentation(largeWindow = true))
    }

    @Test fun minimumRegularWindowIsNearlyFullAndLeavesTwoPaneDecisionsAlone() {
        val bounds = settingsPageBounds(600.dp, 480.dp)!!
        assertEquals(552.dp, bounds.width)
        assertEquals(432.dp, bounds.height)
    }

    @Test fun expandedWindowHasVisibleMarginsOnEverySide() {
        val bounds = settingsPageBounds(840.dp, 900.dp)!!
        assertEquals(SettingsPageSizing.maximumWidth, bounds.width)
        assertEquals(SettingsPageSizing.maximumHeight, bounds.height)
        assertTrue(840.dp - bounds.width > SettingsPageSizing.minimumWindowMargin * 2)
        assertTrue(900.dp - bounds.height > SettingsPageSizing.minimumWindowMargin * 2)
    }

    @Test fun breakpointInputsDoNotCreateAnotherPresentationPolicy() {
        assertEquals(SettingsPresentation.FullWindow, settingsPresentation(largeWindow = 599 >= 600))
        assertEquals(SettingsPresentation.PageSized, settingsPresentation(largeWindow = 600 >= 600))
        assertEquals(SettingsPresentation.PageSized, settingsPresentation(largeWindow = 839 >= 600))
        assertEquals(SettingsPresentation.PageSized, settingsPresentation(largeWindow = 840 >= 600))
    }

    @Test fun impossibleMarginsFallBackWithoutANegativeSize() {
        assertNull(settingsPageBounds(48.dp, 480.dp))
        assertNull(settingsPageBounds(600.dp, 48.dp))
        assertEquals(1.dp, settingsPageBounds(49.dp, 49.dp)?.width)
    }

    @Test fun sizingValuesAreNamedAliasesOfExistingTokens() {
        assertEquals(ReadingWidth, SettingsPageSizing.maximumWidth)
        assertEquals(ReadingWidth, SettingsPageSizing.maximumHeight)
        assertEquals(Tokens.Space.xl, SettingsPageSizing.minimumWindowMargin)
    }

    @Test fun rotationAndRepeatedResizeComputeTheSameValueWithoutNavigationState() {
        val portrait = settingsPageBounds(600.dp, 900.dp)
        val landscape = settingsPageBounds(900.dp, 600.dp)
        assertEquals(portrait, settingsPageBounds(600.dp, 900.dp))
        assertEquals(landscape, settingsPageBounds(900.dp, 600.dp))
    }
}
