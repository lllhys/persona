package persona.feature.chat

/** The small set of hardware keys the large-window composer handles; everything else belongs to the field or shell. */
internal enum class ComposerKey { Enter, Escape, Other }

internal enum class ComposerKeyPress { Down, Up }

internal data class ComposerKeyInput(
    val key: ComposerKey,
    val press: ComposerKeyPress,
    val repeatCount: Int,
    val ctrlPressed: Boolean,
    val metaPressed: Boolean,
    val shiftPressed: Boolean,
    val composing: Boolean,
    val canSend: Boolean,
    val hasContent: Boolean,
    val blocked: Boolean,
    val expandedEditor: Boolean = false,
)

internal enum class ComposerKeyAction { PassThrough, Consume, Send, Collapse }

/**
 * Hardware-key policy for the composer. Ordinary Return (with or without Shift) and every unrelated key pass through.
 * A send combination is consumed even when Send is disabled, so it cannot turn into a newline; only its first key-down
 * sends. IME composition always gets first refusal, including Escape cancelling the current composition.
 */
internal fun composerKeyAction(input: ComposerKeyInput): ComposerKeyAction {
    if (input.composing) return ComposerKeyAction.PassThrough
    if (input.key == ComposerKey.Escape && input.expandedEditor) {
        return if (input.press == ComposerKeyPress.Down && input.repeatCount == 0) ComposerKeyAction.Collapse
        else ComposerKeyAction.Consume
    }
    if (input.key != ComposerKey.Enter || (!input.ctrlPressed && !input.metaPressed)) return ComposerKeyAction.PassThrough
    if (input.press == ComposerKeyPress.Up || input.repeatCount > 0) return ComposerKeyAction.Consume
    if (!input.canSend || !input.hasContent || input.blocked) return ComposerKeyAction.Consume
    return ComposerKeyAction.Send
}
