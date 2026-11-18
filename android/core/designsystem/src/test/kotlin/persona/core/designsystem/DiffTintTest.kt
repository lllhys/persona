package persona.core.designsystem

import androidx.compose.ui.graphics.Color
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * A diff line's tint and its changed words' tint come from the palette, the way iOS `Theme.Diff` has them: Glass reads the
 * generated tokens (their colour, not just their strength), Brief and Board keep their own diff hue at the tokens' strength.
 */
class DiffTintTest {
    private data class DiffValues(val addFill: Color, val delFill: Color, val addEmphasis: Color, val delEmphasis: Color)
    private val light = DiffValues(
        Tokens.Common.Light.Diff.addFill, Tokens.Common.Light.Diff.delFill,
        Tokens.Common.Light.Diff.addEmphasis, Tokens.Common.Light.Diff.delEmphasis,
    )
    private val dark = DiffValues(
        Tokens.Common.Dark.Diff.addFill, Tokens.Common.Dark.Diff.delFill,
        Tokens.Common.Dark.Diff.addEmphasis, Tokens.Common.Dark.Diff.delEmphasis,
    )

    /** The emphasis is a generated token, not a number in a view: iOS `Theme.Diff.addEmphasis` / `delEmphasis` (0.32 / 0.30) on the fill's colour. */
    @Test fun theEmphasisTokensAreTheFillsColourAtTheIosStrength() {
        for (token in listOf(light, dark)) {
            assertEquals(token.addFill.copy(alpha = 0.32f), token.addEmphasis)
            assertEquals(token.delFill.copy(alpha = 0.30f), token.delEmphasis)
        }
    }

    @Test fun glassTakesTheTokensColourForTheLineAndForTheWords() {
        for ((palette, token) in listOf(Palettes.of(AppStyle.Glass, false) to light, Palettes.of(AppStyle.Glass, true) to dark)) {
            assertEquals(token.addFill, palette.diffAddFill)
            assertEquals(token.delFill, palette.diffDelFill)
            assertEquals(token.addEmphasis, palette.diffAddEmphasis)
            assertEquals(token.delEmphasis, palette.diffDelEmphasis)
        }
    }

    /** The light fill is the lighter base colour of iOS, not the deeper text colour with its strength. */
    @Test fun glassFillIsNotTheTextColourAtTheTokenStrength() {
        val palette = Palettes.of(AppStyle.Glass, false)
        assertNotEquals(palette.diffAdd.copy(alpha = light.addFill.alpha), palette.diffAddFill)
        assertNotEquals(palette.diffDel.copy(alpha = light.delFill.alpha), palette.diffDelFill)
    }

    @Test fun lineTintsAreTranslucentAndEmphasisIsDeeperThanTheLine() {
        // Compose's Color constructor quantizes alpha to eight bits (0.14 becomes 36 / 255).
        for (style in AppStyle.entries) for (isDark in listOf(false, true)) {
            val palette = Palettes.of(style, isDark)
            assertEquals("$style $isDark", 0.14f, palette.diffAddFill.alpha, 0.002f)
            assertEquals("$style $isDark", 0.12f, palette.diffDelFill.alpha, 0.002f)
            assertEquals("$style $isDark", 0.32f, palette.diffAddEmphasis.alpha, 0.002f)
            assertEquals("$style $isDark", 0.30f, palette.diffDelEmphasis.alpha, 0.002f)
        }
    }

    @Test fun briefAndBoardKeepTheirOwnDiffHue() {
        for (style in listOf(AppStyle.Brief, AppStyle.Board)) for (isDark in listOf(false, true)) {
            val palette = Palettes.of(style, isDark)
            assertEquals("$style $isDark", palette.diffAdd.copy(alpha = 0.14f), palette.diffAddFill)
            assertEquals("$style $isDark", palette.diffDel.copy(alpha = 0.12f), palette.diffDelFill)
            assertEquals("$style $isDark", palette.diffAdd.copy(alpha = 0.32f), palette.diffAddEmphasis)
            assertEquals("$style $isDark", palette.diffDel.copy(alpha = 0.30f), palette.diffDelEmphasis)
        }
        // and they are not Glass's colours: each style's mapping is its own
        assertNotEquals(Palettes.of(AppStyle.Glass, false).diffAddFill, Palettes.of(AppStyle.Brief, false).diffAddFill)
        assertNotEquals(Palettes.of(AppStyle.Glass, true).diffDelFill, Palettes.of(AppStyle.Board, true).diffDelFill)
    }

    /** The older word tint the extension code review uses is not moved by this. */
    @Test fun theExtensionReviewsWordTintIsUnchanged() {
        val palette = Palettes.of(AppStyle.Glass, false)
        assertEquals(palette.diffAdd.copy(alpha = 0.30f), palette.diffAddWord)
        assertEquals(palette.diffDel.copy(alpha = 0.30f), palette.diffDelWord)
    }
}
