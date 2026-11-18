package persona.feature.chat

import org.junit.Assert.assertEquals
import org.junit.Test

class ComposerKeyboardTest {
    private fun input(
        key: ComposerKey = ComposerKey.Enter,
        press: ComposerKeyPress = ComposerKeyPress.Down,
        repeatCount: Int = 0,
        ctrl: Boolean = false,
        meta: Boolean = false,
        shift: Boolean = false,
        composing: Boolean = false,
        canSend: Boolean = true,
        hasContent: Boolean = true,
        blocked: Boolean = false,
        expanded: Boolean = false,
    ) = ComposerKeyInput(key, press, repeatCount, ctrl, meta, shift, composing, canSend, hasContent, blocked, expanded)

    @Test fun ordinaryAndShiftReturnStayWithTheField() {
        assertEquals(ComposerKeyAction.PassThrough, composerKeyAction(input()))
        assertEquals(ComposerKeyAction.PassThrough, composerKeyAction(input(shift = true)))
    }

    @Test fun ctrlAndMetaReturnSendOnlyTheFirstKeyDown() {
        assertEquals(ComposerKeyAction.Send, composerKeyAction(input(ctrl = true)))
        assertEquals(ComposerKeyAction.Send, composerKeyAction(input(meta = true)))
        assertEquals(ComposerKeyAction.Consume, composerKeyAction(input(ctrl = true, repeatCount = 1)))
        assertEquals(ComposerKeyAction.Consume, composerKeyAction(input(ctrl = true, press = ComposerKeyPress.Up)))
    }

    @Test fun unavailableSendConsumesWithoutSending() {
        for (unavailable in listOf(
            input(ctrl = true, canSend = false),
            input(ctrl = true, hasContent = false),
            input(ctrl = true, blocked = true),
        )) assertEquals(ComposerKeyAction.Consume, composerKeyAction(unavailable))
    }

    @Test fun imeCompositionOwnsEveryKeyUntilCommitted() {
        assertEquals(ComposerKeyAction.PassThrough, composerKeyAction(input(ctrl = true, composing = true)))
        assertEquals(ComposerKeyAction.PassThrough,
            composerKeyAction(input(key = ComposerKey.Escape, composing = true, expanded = true)))
    }

    @Test fun escapeCollapsesOnlyTheExpandedEditor() {
        assertEquals(ComposerKeyAction.PassThrough, composerKeyAction(input(key = ComposerKey.Escape)))
        assertEquals(ComposerKeyAction.Collapse, composerKeyAction(input(key = ComposerKey.Escape, expanded = true)))
        assertEquals(ComposerKeyAction.Consume,
            composerKeyAction(input(key = ComposerKey.Escape, press = ComposerKeyPress.Up, expanded = true)))
        assertEquals(ComposerKeyAction.Consume,
            composerKeyAction(input(key = ComposerKey.Escape, repeatCount = 2, expanded = true)))
    }

    @Test fun unrelatedKeysAndShortcutHelpPassThrough() {
        assertEquals(ComposerKeyAction.PassThrough, composerKeyAction(input(key = ComposerKey.Other, ctrl = true)))
        assertEquals(ComposerKeyAction.PassThrough, composerKeyAction(input(key = ComposerKey.Other)))
    }
}
