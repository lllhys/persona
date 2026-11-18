package persona.core.designsystem

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class HardwareShortcutControllerTest {
    @Test fun emptyEscapeIsConsumedWithoutNavigationOrAgentSideEffects() {
        val controller = HardwareShortcutController()
        assertTrue(controller.dispatch(HardwareShortcut.Escape, HardwareKeyPhase.Down))
        assertEquals(0, controller.registrationCount)
    }

    @Test fun escapeDismissesOnlyTheTopRegistration() {
        val controller = HardwareShortcutController()
        val closed = mutableListOf<String>()
        controller.register("sheet") { closed += "sheet" }
        controller.register("dialog") { closed += "dialog" }
        controller.dispatch(HardwareShortcut.Escape, HardwareKeyPhase.Down)
        assertEquals(listOf("dialog"), closed)
        assertEquals(2, controller.registrationCount)
    }

    @Test fun keyUpAndRepeatNeverDismissAnotherLayer() {
        val controller = HardwareShortcutController()
        var closed = 0
        controller.register("sheet") { closed++ }
        controller.dispatch(HardwareShortcut.Escape, HardwareKeyPhase.Down, repeated = true)
        controller.dispatch(HardwareShortcut.Escape, HardwareKeyPhase.Up)
        assertEquals(0, closed)
    }

    @Test fun disposingOneTokenCannotRemoveAnotherRegistration() {
        val controller = HardwareShortcutController()
        val closed = mutableListOf<String>()
        val first = controller.register("first") { closed += "first" }
        val second = controller.register("second") { closed += "second" }
        controller.unregister(first)
        controller.dispatch(HardwareShortcut.Escape, HardwareKeyPhase.Down)
        assertEquals(listOf("second"), closed)
        controller.unregister(first)
        controller.dispatch(HardwareShortcut.Escape, HardwareKeyPhase.Down)
        assertEquals(listOf("second", "second"), closed)
        controller.unregister(second)
        assertEquals(0, controller.registrationCount)
    }

    @Test fun replacingTheSameTokenDoesNotLetItsOldDisposalRemoveTheReplacement() {
        val controller = HardwareShortcutController()
        val closed = mutableListOf<String>()
        val old = controller.register("shared") { closed += "old" }
        controller.register("shared") { closed += "new" }
        controller.unregister(old)
        controller.dispatch(HardwareShortcut.Escape, HardwareKeyPhase.Down)
        assertEquals(listOf("new"), closed)
    }

    @Test fun helpOpensWithoutChangingTransientRegistrations() {
        val controller = HardwareShortcutController()
        controller.register("settings") {}
        controller.dispatch(HardwareShortcut.Help, HardwareKeyPhase.Down)
        assertTrue(controller.helpVisible)
        assertEquals(1, controller.registrationCount)
        controller.closeHelp()
        assertFalse(controller.helpVisible)
    }
}
