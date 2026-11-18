package persona.core.designsystem

import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.isFocused
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performKeyInput
import androidx.compose.ui.test.pressKey
import androidx.compose.ui.test.withKeyDown
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w640dp-h800dp-xhdpi")
class ComposerHardwareTest {
    @get:Rule val rule = createComposeRule()

    private fun show(
        value: TextFieldValue,
        hardware: ComposerHardwareActions?,
        onCollapse: () -> Unit,
        onSend: () -> Unit,
    ) {
        rule.setContent {
            CompositionLocalProvider(LocalComposerHardware provides hardware) {
                PersonaTheme(darkTheme = false, reduceMotion = true) {
                    ExpandedComposer(value, {}, "Message", onCollapse) {
                        SendButton(onClick = onSend, enabled = true)
                    }
                }
            }
        }
        rule.onNode(hasSetTextAction()).performClick()
        rule.waitForIdle()
    }

    @Test fun ctrlAndMetaReturnCollapseAndUseTheProvidedSend() {
        var collapsed = 0
        var sent = 0
        show(TextFieldValue("draft", TextRange(5)), ComposerHardwareActions(send = { sent++ }),
            onCollapse = { collapsed++ }, onSend = {})
        rule.onNode(hasSetTextAction() and isFocused()).performKeyInput {
            withKeyDown(Key.CtrlLeft) { pressKey(Key.Enter) }
        }
        assertEquals(1, collapsed)
        assertEquals(1, sent)
        rule.onNode(hasSetTextAction() and isFocused()).performKeyInput {
            withKeyDown(Key.MetaLeft) { pressKey(Key.Enter) }
        }
        assertEquals(2, collapsed)
        assertEquals(2, sent)
    }

    @Test fun escapeCollapsesEvenWhenSendIsUnavailable() {
        var collapsed = 0
        show(TextFieldValue(), ComposerHardwareActions(), onCollapse = { collapsed++ }, onSend = {})
        rule.onNode(hasSetTextAction() and isFocused()).performKeyInput { pressKey(Key.Escape) }
        assertEquals(1, collapsed)
    }

    @Test fun defaultNullKeepsStandaloneEditorKeysUnchanged() {
        var collapsed = 0
        var sent = 0
        show(TextFieldValue("draft"), null, onCollapse = { collapsed++ }, onSend = { sent++ })
        rule.onNode(hasSetTextAction() and isFocused()).performKeyInput {
            withKeyDown(Key.CtrlLeft) { pressKey(Key.Enter) }
            pressKey(Key.Escape)
        }
        assertEquals(0, collapsed)
        assertEquals(0, sent)
    }

    @Test fun compositionKeepsBothSendAndEscapeWithTheIme() {
        var collapsed = 0
        var sent = 0
        show(TextFieldValue("日本語", TextRange(3), TextRange(0, 3)), ComposerHardwareActions(send = { sent++ }),
            onCollapse = { collapsed++ }, onSend = {})
        rule.onNode(hasSetTextAction() and isFocused()).performKeyInput {
            withKeyDown(Key.CtrlLeft) { pressKey(Key.Enter) }
            pressKey(Key.Escape)
        }
        assertEquals(0, collapsed)
        assertEquals(0, sent)
    }
}
