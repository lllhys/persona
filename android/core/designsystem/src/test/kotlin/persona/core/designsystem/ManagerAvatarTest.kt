package persona.core.designsystem

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** An assistant's monogram, by the rule every client shares (docs/assistant/managers.md "Clients"). */
class ManagerAvatarTest {
    private val order = listOf(JHue.Blue, JHue.Green, JHue.Orange, JHue.Pink, JHue.Indigo)

    @Test fun theDefaultIsTheAiPurpleWhateverItsId() {
        assertNull(managerHue("assistant", isDefault = true))
        assertNull(managerHue("m-3fa21c", isDefault = true))
    }

    /** The sum of the id's UTF-8 bytes, mod 5: blue, green, orange, pink, indigo. */
    @Test fun theOthersGoByTheSumOfTheirIdsBytes() {
        // "d" is 100, "e" 101 … "h" 104
        assertEquals(order, listOf("d", "e", "f", "g", "h").map { managerHue(it, isDefault = false) })
        assertEquals(JHue.Blue, managerHue("", isDefault = false))
        // 'a' + 'b' = 97 + 98 = 195
        assertEquals(order[195 % 5], managerHue("ab", isDefault = false))
        // every byte of a character counts: "é" is C3 A9
        assertEquals(order[(0xC3 + 0xA9) % 5], managerHue("é", isDefault = false))
        // the mock hub's ids
        assertEquals(order["m-3fa21c".sumOf { it.code } % 5], managerHue("m-3fa21c", isDefault = false))
    }

    @Test fun theInitialIsTheNamesFirstCharacter() {
        assertEquals("W", managerInitial(" work "))
        assertEquals("工", managerInitial("工作"))
        // a character outside the BMP is one initial, not half of one
        assertEquals("🏠", managerInitial("🏠 home"))
        assertEquals("", managerInitial("  "))
    }
}
