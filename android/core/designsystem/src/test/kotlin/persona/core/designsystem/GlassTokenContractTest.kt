package persona.core.designsystem

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.security.MessageDigest

/** Shared iPhone chrome tokens, Android reading sizes, native font metrics and the glass renderer. */
class GlassTokenContractTest {
    @Test fun standardTypeUsesTheSystemFontWithoutContentLineSpacing() {
        val t = Types.Glass
        // Chrome roles use spec/tokens' shared levels; long-form roles use Android's typeGlass tokens (#5780).
        val roles = listOf(
            Triple(t.largeTitle, Tokens.Type.Display.size, Tokens.Type.Display.weight),
            Triple(t.title, Tokens.Type.Title.size, Tokens.Type.Title.weight),
            Triple(t.headline, Tokens.Type.Heading.size, Tokens.Type.Heading.weight),
            Triple(t.body, Tokens.TypeGlass.Body.size, Tokens.Type.Body.weight),
            Triple(t.bodyStrong, Tokens.TypeGlass.Body.size, FontWeight.SemiBold),
            Triple(t.callout, Tokens.TypeGlass.Secondary.size, Tokens.Type.Secondary.weight),
            Triple(t.caption, Tokens.Type.Caption.size, Tokens.Type.Caption.weight),
            Triple(t.micro, Tokens.Type.Label.size, Tokens.Type.Label.weight),
            Triple(t.label, Tokens.Type.Heading.size, Tokens.Type.Heading.weight),
            Triple(t.labelSmall, Tokens.TypeGlass.Secondary.size, FontWeight.Medium),
            Triple(t.number, Tokens.Type.Display.size, Tokens.Type.Display.weight),
            Triple(t.voice, Tokens.TypeGlass.Body.size, Tokens.Type.Body.weight),
        )
        // Chrome keeps the shared iPhone levels; long-form reading uses Android's 16sp / 14sp tokens (#5780).
        assertEquals(listOf(28.sp, 22.sp, 17.sp, 16.sp, 16.sp, 14.sp, 13.sp, 12.sp, 17.sp, 14.sp, 28.sp, 16.sp), roles.map { it.first.fontSize })
        assertEquals(listOf(FontWeight.Bold, FontWeight.SemiBold, FontWeight.SemiBold, FontWeight.Normal, FontWeight.SemiBold, FontWeight.Normal,
            FontWeight.Normal, FontWeight.Normal, FontWeight.SemiBold, FontWeight.Medium, FontWeight.Bold, FontWeight.Normal), roles.map { it.first.fontWeight })
        for ((style, size, weight) in roles) {
            assertEquals(FontFamily.Default, style.fontFamily)
            assertEquals(size, style.fontSize)
            assertEquals(weight, style.fontWeight)
            assertEquals(TextUnit.Unspecified, style.lineHeight)
        }
        assertEquals(TabularFigures, t.number.fontFeatureSettings)
        assertEquals(TabularFigures, t.micro.fontFeatureSettings)
        assertEquals(FontFamily.Monospace, t.code.fontFamily)
        assertEquals(listOf(13.sp, 12.sp), listOf(t.code.fontSize, t.codeSmall.fontSize))
    }

    @Test fun glassUsesSharedSystemSurfacesAndCurrentStateSemantics() {
        for ((dark, p) in listOf(false to Palettes.GlassLight, true to Palettes.GlassDark)) {
            assertEquals(if (dark) Color.Black else Color.White, p.background)
            assertEquals(if (dark) Tokens.Semantic.Glass.Dark.card else Tokens.Semantic.Glass.Light.card, p.surfaceOpaque)
            assertEquals(if (dark) Color.White else Color.Black, p.ink)
            assertEquals(if (dark) Tokens.Semantic.Glass.Dark.secondaryText else Tokens.Semantic.Glass.Light.secondaryText, p.ink2)
            assertEquals(p.ink2, p.ink3)
            val systemBlue = if (dark) Tokens.Semantic.Glass.Dark.accent else Tokens.Semantic.Glass.Light.accent
            assertEquals(systemBlue, p.accent)
            // The iPhone's colours as they are (#4209), and white words on the accent, as on iOS.
            assertEquals(Color.White, p.onAccent)
            assertEquals(Color.White, p.needsYou.onDot)
            // Needs You is the iPhone blue (#4209), now read from the generated tokens.
            val needsDot = if (dark) Tokens.Status.Dark.NeedsYou.dot else Tokens.Status.Light.NeedsYou.dot
            val needsText = if (dark) Tokens.Status.Dark.NeedsYou.text else Tokens.Status.Light.NeedsYou.text
            assertEquals(systemBlue, needsDot)
            assertEquals(if (dark) Color(0xFF409CFF) else Color(0xFF0064C8), needsText)
            assertEquals(needsDot, p.needsYou.dot)
            assertEquals(needsText, p.needsYou.text)
            assertEquals(if (dark) Tokens.Status.Dark.Working.dot else Tokens.Status.Light.Working.dot, p.working.dot)
            assertEquals(if (dark) Tokens.Status.Dark.Working.text else Tokens.Status.Light.Working.text, p.working.text)
            assertEquals(if (dark) Tokens.Status.Dark.Unread.dot else Tokens.Status.Light.Unread.dot, p.done.dot)
            assertEquals(if (dark) Tokens.Status.Dark.Unread.text else Tokens.Status.Light.Unread.text, p.done.text)
            val neutralTint = if (dark) Tokens.Status.Dark.Offline.tint else Tokens.Status.Light.Offline.tint
            assertEquals(neutralTint, p.needsYou.tint)
            assertEquals(neutralTint, p.working.tint)
            assertEquals(neutralTint, p.done.tint)
            assertTrue(p.surface.alpha < 1f)
            assertEquals(p.surfaceOpaque, p.statusSurface)
            assertEquals(if (dark) Tokens.Semantic.Glass.Dark.userBubble else Tokens.Semantic.Glass.Light.userBubble, p.userBubble)
            assertEquals(p.caution, p.tone(Tone.Caution).dot)
            assertEquals(p.positive, p.tone(Tone.Positive).dot)
        }
        assertEquals(14.dp, Shapes.Glass.card)
        assertEquals(22.dp, Shapes.Glass.panel)
        assertEquals(10.dp, Shapes.Glass.control)
        assertEquals(20.dp, Tokens.Radius.bubble)
    }

    @Test fun glassMovesWithTheIPhoneSprings() {
        val glass = Motions.of(AppStyle.Glass)
        // Theme.Motion.state is .snappy(duration: 0.25), bounce 0.15; .layout is .smooth(duration: 0.3), no bounce.
        assertEquals(250, glass.quick)
        assertEquals(300, glass.standard)
        assertEquals(0.85f, glass.state!!.dampingRatio, 1e-6f)
        assertEquals(631.65f, glass.state!!.stiffness, 0.01f)
        assertEquals(1f, glass.layout!!.dampingRatio, 1e-6f)
        assertEquals(438.65f, glass.layout!!.stiffness, 0.01f)
        // Brief and Board keep their tweens.
        for ((style, quick, standard) in listOf(Triple(AppStyle.Brief, 140, 240), Triple(AppStyle.Board, 100, 160))) {
            val m = Motions.of(style)
            assertEquals(listOf(quick, standard), listOf(m.quick, m.standard))
            assertEquals(null, m.state)
            assertEquals(null, m.layout)
        }
    }

    @Test fun spacingIsTheIPhoneScale() {
        val t = Tokens.Space
        assertEquals(listOf(0, 2, 4, 6, 8, 12, 16, 24, 32).map { it.dp }, listOf(t.flush, t.xxs, t.xs, t.sm, t.s, t.m, t.l, t.xl, t.xxl))
        assertEquals(listOf(t.xs, t.s, t.l, t.xl, t.l), listOf(t.lines, t.titleToContent, t.block, t.section, t.page))
    }

    @Test fun compactChromeAndSidebarBadgesKeepReadableText() {
        assertEquals(Color(0xB83A3A3C), HomeTokens.chromeDark)
        assertEquals(22.dp, HomeTokens.sidebarBadgeSize)
        for (palette in listOf(Palettes.GlassLight, Palettes.GlassDark)) {
            // White words on the accent, held to iOS's floor for them (3.5:1, #4209); secondary words are iOS's own gray.
            val fill = sidebarActionFill(palette)
            assertTrue(palette.whiteOnAccent)
            assertTrue(ExtensionThemeColors.contrast(Color.White, fill) >= 3.5)
            val chrome = (if (palette.dark) HomeTokens.chromeDark else HomeTokens.chromeLight)
                .compositeOn(palette.background)
            assertTrue(ExtensionThemeColors.contrast(palette.ink, chrome) >= 4.5)
        }
    }

    @Test fun compactSidebarRetainsCustomAccentColors() {
        val custom = Palettes.GlassDark.copy(accent = Color.Magenta)
        assertEquals(custom.accent, sidebarActionFill(custom))
        assertTrue(ExtensionThemeColors.contrast(ExtensionThemeColors.on(custom.accent), custom.accent) >= 4.5)
    }

    @Test fun onlyFinalDefaultHomeAnswersUseTheNeutralBox() {
        val items = listOf(NavItem("assistant", "Assistant", persona.core.designsystem.icons.PhIcons.Sparkle),
            NavItem("agents", "Agents", persona.core.designsystem.icons.PhIcons.Terminal))
        val home = HomeChrome(items, "assistant", {}, {})
        assertTrue(boxesHomeAnswer(true, AppStyle.Glass, home))
        assertTrue(!boxesHomeAnswer(false, AppStyle.Glass, home))
        assertTrue(!boxesHomeAnswer(true, AppStyle.Glass, null))
        assertTrue(!boxesHomeAnswer(true, AppStyle.Glass, HomeChrome(items, "agents", {}, {})))
        for (style in listOf(AppStyle.Brief, AppStyle.Board)) assertTrue(!boxesHomeAnswer(true, style, home))
    }

    @Test fun neutralAnswerBoxesMatchTheReferenceAndReadWithNativeRoles() {
        assertEquals(Color(0xFFF0F0F3), HomeTokens.personaAnswerLight)
        assertEquals(Color(0xFF2A2A2E), HomeTokens.personaAnswerDark)
        for (palette in listOf(Palettes.GlassLight, Palettes.GlassDark)) {
            val fill = homeAnswerFill(palette)
            // iOS's floors (#4209): body words 4.5:1, the accent as a link 3:1; secondary words are iOS's own gray.
            assertTrue(ExtensionThemeColors.contrast(palette.ink, fill) >= 4.5)
            assertTrue(ExtensionThemeColors.contrast(palette.accent, fill) >= 3.0)
            val custom = palette.copy(customRoles = true, surfaceOpaque = palette.background)
            assertEquals(custom.surfaceOpaque, homeAnswerFill(custom))
        }
    }

    @Test fun standardHomeUserColorsMatchTheNewLayoutWithoutReplacingCustomBubbles() {
        val home = HomeChrome(listOf(NavItem("assistant", "Assistant", persona.core.designsystem.icons.PhIcons.Sparkle)), "assistant", {}, {})
        for (palette in listOf(Palettes.GlassLight, Palettes.GlassDark)) {
            val expected = if (palette.dark) Color(0xFF1C2F47) else Color(0xFFE3EDFD)
            assertEquals(expected, homeUserFill(palette, home))
            assertTrue(ExtensionThemeColors.contrast(palette.ink, expected) >= 4.5)
            assertEquals(palette.userBubble, homeUserFill(palette, null))
            val custom = palette.copy(userBubble = Color.Magenta)
            assertEquals(custom.userBubble, homeUserFill(custom, home))
        }
    }

    /** Pin entire non-Glass definitions, including fonts, heights, statuses, source badges and shared scale helper. */
    @Test fun briefAndBoardDefinitionsRemainByteIdentical() {
        val expected = mapOf(
            "Typography.kt" to "c27fdf9ee919e9bd6a9f898d9fb2b959bfae36d5f3a82bf23bbe950df6d2ce93",
            "PersonaTheme.kt" to "33b727267bc7b07e8e4e4666c3afeeb5a7e6eab4b6f17bfff7f81f6111e679e8",
            "StatusPalette.kt" to "6269abcf94815d8ca5cbd61f121fc594e0fe41e8ca24bb7e7d997e25f6c4ccdf",
        )
        for ((name, hash) in expected) {
            val source = repositoryFile("android/core/designsystem/src/main/kotlin/persona/core/designsystem/$name").readText()
            val stable = when (name) {
                "Typography.kt" -> source.substringAfter("    private fun scale(").substringBefore("    // Native system metrics") +
                    source.substringAfter("    val Brief = scale(").substringBefore("    fun of(style:")
                "PersonaTheme.kt" -> source.substringAfter("    val Brief = PersonaShapes(").substringBefore("    fun of(style:")
                else -> source.substringAfter("    // Brief: paper and ink;").substringBefore("    fun of(style:")
            }
            val actual = MessageDigest.getInstance("SHA-256").digest(stable.toByteArray()).joinToString("") { "%02x".format(it) }
            assertEquals(name, hash, actual)
        }
    }

    private fun repositoryFile(path: String): File =
        generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .map { File(it, path) }.first { it.isFile }
}
