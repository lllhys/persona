package persona.core.designsystem

import androidx.compose.runtime.Immutable
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

/** Tabular figures keep times and counts from jittering as they change. */
internal const val TabularFigures = "tnum"

/**
 * Bundled Latin faces (OFL, licences in assets/licenses). Chinese always falls back to the system font, so each
 * style keeps the phone's own CJK glyphs.
 */
object PersonaFonts {
    private fun variable(res: Int, vararg weights: Int) = FontFamily(
        weights.map { Font(res, FontWeight(it), variationSettings = FontVariation.Settings(FontVariation.weight(it))) },
    )

    val manrope = variable(R.font.manrope, 400, 500, 600, 700, 800)
    val sora = variable(R.font.sora, 300, 400, 500, 600, 700)
    val mono = variable(R.font.jetbrains_mono, 400, 500, 600)
    val barlow = FontFamily(
        Font(R.font.barlow_sc_regular, FontWeight.Normal),
        Font(R.font.barlow_sc_medium, FontWeight.Medium),
        Font(R.font.barlow_sc_semibold, FontWeight.SemiBold),
    )
}

/**
 * The type scale of one style. Sizes never go below 12sp; numbers that change in place use [number] or [micro]
 * (tabular figures).
 */
@Immutable
data class PersonaType(
    /** Root page titles that scroll with the content. */
    val largeTitle: TextStyle,
    /** Bar titles, card and sheet titles. */
    val title: TextStyle,
    /** Row titles and section headings. */
    val headline: TextStyle,
    val body: TextStyle,
    val bodyStrong: TextStyle,
    /** Secondary text inside rows and cards. */
    val callout: TextStyle,
    /** Metadata, hints, section captions. */
    val caption: TextStyle,
    /** Times, counts and other small numbers. */
    val micro: TextStyle,
    /** Button labels. */
    val label: TextStyle,
    val labelSmall: TextStyle,
    /** Large counters (the board's bay counts, hub facts). */
    val number: TextStyle,
    /** Persona speaking: the briefing sentence on the home page. */
    val voice: TextStyle,
    val code: TextStyle,
    val codeSmall: TextStyle,
)

internal object Types {
    private fun scale(
        display: FontFamily, text: FontFamily, numbers: FontFamily,
        largeTitle: TextStyle, number: TextStyle, voice: TextStyle,
        title: TextStyle = TextStyle(fontFamily = display, fontSize = 18.sp, lineHeight = 25.sp, fontWeight = FontWeight.SemiBold),
    ) = PersonaType(
        largeTitle = largeTitle,
        title = title,
        headline = TextStyle(fontFamily = text, fontSize = 16.sp, lineHeight = 22.sp, fontWeight = FontWeight.SemiBold),
        body = TextStyle(fontFamily = text, fontSize = 15.sp, lineHeight = 24.sp, fontWeight = FontWeight.Normal),
        bodyStrong = TextStyle(fontFamily = text, fontSize = 15.sp, lineHeight = 24.sp, fontWeight = FontWeight.SemiBold),
        callout = TextStyle(fontFamily = text, fontSize = 14.sp, lineHeight = 21.sp, fontWeight = FontWeight.Normal),
        caption = TextStyle(fontFamily = text, fontSize = 13.sp, lineHeight = 18.sp, fontWeight = FontWeight.Medium),
        micro = TextStyle(fontFamily = numbers, fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.Medium, fontFeatureSettings = TabularFigures),
        label = TextStyle(fontFamily = text, fontSize = 15.sp, lineHeight = 20.sp, fontWeight = FontWeight.SemiBold),
        labelSmall = TextStyle(fontFamily = text, fontSize = 13.sp, lineHeight = 18.sp, fontWeight = FontWeight.SemiBold),
        number = number,
        voice = voice,
        code = TextStyle(fontFamily = PersonaFonts.mono, fontSize = 13.sp, lineHeight = 20.sp, fontWeight = FontWeight.Normal),
        codeSmall = TextStyle(fontFamily = PersonaFonts.mono, fontSize = 12.sp, lineHeight = 18.sp, fontWeight = FontWeight.Normal),
    )

    // Native system metrics, not the content-only line-height multipliers in iOS Theme.Level.
    private fun system(size: androidx.compose.ui.unit.TextUnit, weight: FontWeight = FontWeight.Normal) =
        TextStyle(fontFamily = FontFamily.Default, fontSize = size, fontWeight = weight)

    // Shared iPhone levels for chrome, with Android's documented long-form reading sizes (#5780).
    val Glass = PersonaType(
        largeTitle = system(Tokens.Type.Display.size, Tokens.Type.Display.weight),
        title = system(Tokens.Type.Title.size, Tokens.Type.Title.weight),
        headline = system(Tokens.Type.Heading.size, Tokens.Type.Heading.weight),
        body = system(Tokens.TypeGlass.Body.size, Tokens.Type.Body.weight),
        bodyStrong = system(Tokens.TypeGlass.Body.size, FontWeight.SemiBold),
        callout = system(Tokens.TypeGlass.Secondary.size, Tokens.Type.Secondary.weight),
        caption = system(Tokens.Type.Caption.size, Tokens.Type.Caption.weight),
        micro = system(Tokens.Type.Label.size, Tokens.Type.Label.weight).copy(fontFeatureSettings = TabularFigures),
        label = system(Tokens.Type.Heading.size, Tokens.Type.Heading.weight),
        labelSmall = system(Tokens.TypeGlass.Secondary.size, FontWeight.Medium),
        number = system(Tokens.Type.Display.size, Tokens.Type.Display.weight).copy(fontFeatureSettings = TabularFigures),
        voice = system(Tokens.TypeGlass.Body.size, Tokens.Type.Body.weight),
        code = system(Tokens.Type.Code.size, Tokens.Type.Code.weight).copy(fontFamily = FontFamily.Monospace),
        // iOS has one code level; the smaller code takes the label level's size.
        codeSmall = system(Tokens.Type.Label.size, Tokens.Type.Code.weight).copy(fontFamily = FontFamily.Monospace),
    )

    val Brief = scale(
        display = PersonaFonts.manrope, text = PersonaFonts.manrope, numbers = PersonaFonts.manrope,
        largeTitle = TextStyle(fontFamily = PersonaFonts.manrope, fontSize = 26.sp, lineHeight = 34.sp, fontWeight = FontWeight.Bold),
        number = TextStyle(fontFamily = PersonaFonts.manrope, fontSize = 22.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold, fontFeatureSettings = TabularFigures),
        voice = TextStyle(fontFamily = PersonaFonts.manrope, fontSize = 28.sp, lineHeight = 38.sp, fontWeight = FontWeight.SemiBold),
        title = TextStyle(fontFamily = PersonaFonts.manrope, fontSize = 17.sp, lineHeight = 24.sp, fontWeight = FontWeight.SemiBold),
    )

    val Board = scale(
        display = FontFamily.Default, text = FontFamily.Default, numbers = PersonaFonts.barlow,
        largeTitle = TextStyle(fontSize = 22.sp, lineHeight = 28.sp, fontWeight = FontWeight.Bold),
        number = TextStyle(fontFamily = PersonaFonts.barlow, fontSize = 30.sp, lineHeight = 32.sp, fontWeight = FontWeight.SemiBold, fontFeatureSettings = TabularFigures),
        voice = TextStyle(fontSize = 15.sp, lineHeight = 22.sp, fontWeight = FontWeight.Medium),
        title = TextStyle(fontSize = 16.sp, lineHeight = 22.sp, fontWeight = FontWeight.SemiBold),
    ).let {
        // Field values (machine, folder, agent) and small numbers read like a printed strip.
        it.copy(micro = it.micro.copy(fontSize = 13.sp))
    }

    fun of(style: AppStyle) = when (style) {
        AppStyle.Glass -> Glass
        AppStyle.Brief -> Brief
        AppStyle.Board -> Board
    }
}
