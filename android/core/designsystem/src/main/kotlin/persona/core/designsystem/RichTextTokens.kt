package persona.core.designsystem

import androidx.compose.ui.unit.dp

/**
 * Geometry of rich message text (code blocks, list markers, quotes, tables) that the shared scale has no step for. A code
 * block's string and number colours are palette roles ([PersonaPalette.codeString], [PersonaPalette.codeNumber]).
 */
object RichTextTokens {
    /** The outline around a code block. */
    val codeBorder = Tokens.Size.line
    /** The copy icon in a code block's header. */
    val codeCopyIcon = Tokens.Size.markXS
    /** The narrowest a list's number or bullet column gets, so items line up. */
    val listMarkerMinWidth = Tokens.Size.iconXS
    /** The bar down the leading edge of a quote. */
    val quoteBar = Tokens.Size.bar
    /** A content-driven table column holds a few words before it wraps. Scaled with the message's font size. */
    val tableColumnMinimum = 160.dp
    /** A sentence wraps at a readable measure when its table scrolls. Scaled with the message's font size. */
    val tableColumnMaximum = 220.dp
}
