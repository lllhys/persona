package persona.core.designsystem

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.ExtensionTheme

/** Rich message text moved its literals into tokens (#4228); the values must stay what the inbox drew before, the code colours follow the palette (#934). */
class RichTextTokensTest {
    @Test fun keepsTheValuesTheInboxDrewBefore() {
        assertEquals(1.dp, RichTextTokens.codeBorder)
        assertEquals(12.dp, RichTextTokens.codeCopyIcon)
        assertEquals(20.dp, RichTextTokens.listMarkerMinWidth)
        assertEquals(3.dp, RichTextTokens.quoteBar)
        assertEquals(160.dp, RichTextTokens.tableColumnMinimum)
        assertEquals(220.dp, RichTextTokens.tableColumnMaximum)
    }

    @Test fun codeColoursKeepTheDefaultLookAndFollowEveryOtherStyleAndMode() {
        // Glass light is the default look: the Lark code block keeps the colours it always drew (#4228).
        assertEquals(Color(0xFF2E7D32), Palettes.GlassLight.codeString)
        assertEquals(Color(0xFFE65100), Palettes.GlassLight.codeNumber)
        // Every other style × mode takes its own roles instead of the fixed light colours (#934).
        for (style in AppStyle.entries) for (dark in listOf(false, true)) {
            if (style == AppStyle.Glass && !dark) continue
            val p = Palettes.of(style, dark)
            assertEquals("$style dark=$dark", p.diffAdd, p.codeString)
            assertEquals("$style dark=$dark", p.cautionText, p.codeNumber)
            // and reads on the code block, dark modes included (the fixed light colours did not)
            val block = p.ink.copy(alpha = 0.05f).compositeOver(p.background)
            for (c in listOf(p.codeString, p.codeNumber)) assertTrue("$style dark=$dark", ExtensionThemeColors.contrast(c, block) >= 3.0)
        }
    }

    @Test fun aThemePackagesCautionColoursCodeNumbers() {
        val theme = ExtensionTheme("example", "Example", "ocean", "Ocean", mapOf("caution" to "#7A4A00"), mapOf("caution" to "#FFD27A"))
        assertEquals(Color(0xFF7A4A00), ExtensionThemeColors.palette(theme, AppStyle.Glass, dark = false).codeNumber)
        assertEquals(Color(0xFFFFD27A), ExtensionThemeColors.palette(theme, AppStyle.Board, dark = true).codeNumber)
    }

    @Test fun codeTextIsThirteenSpInEveryStyle() {
        for (style in AppStyle.entries) assertEquals(13.sp, Types.of(style).code.fontSize)
    }
}
