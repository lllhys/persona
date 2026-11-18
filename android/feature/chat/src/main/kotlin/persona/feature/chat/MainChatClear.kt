package persona.feature.chat

import androidx.compose.runtime.compositionLocalOf

/**
 * The main conversation's Clear context (#5100, the web's #5095): `/clear` in its composer's "/" list and "+" sheet, and
 * `/clear` sent alone, clear it through the hub instead of going to Persona, with no confirmation (nothing is deleted:
 * the old conversation is archived). The assistant page provides it around its main conversation while the hub offers
 * it, and nothing anywhere else. [clear] runs [onFailed] when the hub did not clear it, so the composer puts `/clear` back.
 */
fun interface MainChatClear {
    fun clear(onFailed: () -> Unit)
}

val LocalMainChatClear = compositionLocalOf<MainChatClear?> { null }

/** A draft that is the `/clear` command alone (web `isClearCommand`): the word, nothing attached; anything more is a message. */
internal fun isMainClearCommand(text: String, attachments: Int, extraContent: Boolean): Boolean =
    attachments == 0 && !extraContent && text.trim().equals(SlashCommand.CLEAR, ignoreCase = true)
