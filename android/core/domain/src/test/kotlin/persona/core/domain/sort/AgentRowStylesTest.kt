package persona.core.domain.sort

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Session
import persona.core.contracts.Workflow
import persona.core.domain.zh

/**
 * The Agents list's row styles (#4338 A9): one display made of the two stored values, with iOS's cases
 * (`AgentRowStyleTests.aDisplayIsAStyleOrCompactList`), the parts each display draws (`AgentRowStyles.swift`:
 * `avatarRow`, `twoLineRow`, `cardRow`; `CompactSlotRow`), and State Cards' state word.
 */
class AgentRowStylesTest {
    private fun row(state: String = "working", f: Session.() -> Session = { this }) = AgentSession(
        Session(machineId = "m", sessionId = "s", title = "Ship the widget", cwd = "/w/app", state = state, agent = "claude").f())

    @Test fun aDisplayIsAStyleOrCompactList() {
        assertEquals(AgentListDisplay.Avatar, AgentListDisplay.of(AgentRowStyle.Avatar, list = false))
        assertEquals(AgentListDisplay.TwoLines, AgentListDisplay.of(AgentRowStyle.TwoLines, list = false))
        assertEquals(AgentListDisplay.Cards, AgentListDisplay.of(AgentRowStyle.Cards, list = false))
        // Compact List whatever style was chosen before it, which it keeps.
        for (style in AgentRowStyle.entries) assertEquals(AgentListDisplay.CompactList, AgentListDisplay.of(style, list = true))
        assertNull(AgentListDisplay.CompactList.style)
        assertEquals(AgentRowStyle.entries, AgentListDisplay.entries.mapNotNull { it.style })
        assertEquals(AgentRowStyle.Avatar, AgentRowStyle.Default)
        assertEquals(AgentListDisplay.Avatar, AgentListDisplay.Default)
        // In iOS's order, as ⋯ › Display and Settings list them.
        assertEquals(listOf("Avatar", "TwoLines", "Cards", "CompactList"), AgentListDisplay.entries.map { it.name })
    }

    @Test fun aStoredStyleReadsByItsNameAndAnythingElseIsAvatar() {
        assertEquals(AgentRowStyle.Cards, AgentRowStyle.fromPreference("Cards"))
        assertEquals(AgentRowStyle.TwoLines, AgentRowStyle.fromPreference("TwoLines"))
        for (value in listOf(null, "", "compact", "removed-value")) assertEquals(AgentRowStyle.Avatar, AgentRowStyle.fromPreference(value))
    }

    @Test fun eachDisplayDrawsTheParts() {
        // A: the mark in its well, the state on the corner; the words; the place line (source, place, model, to-do, bell).
        with(AgentListDisplay.Avatar.rowParts) {
            assertEquals(AgentRowLeading.Well, leading)
            assertFalse(markBesideTitle)
            assertTrue(placeLine)
            assertFalse(sourceAfterWords)
            assertFalse(stateAtFoot)
            assertFalse(card)
            // iOS gives the words two lines (`lineLimit(2)`).
            assertEquals(2, wordLines)
        }
        // B: the dot alone, the mark beside the title; one line of words with the source at its end; no model or to-do.
        with(AgentListDisplay.TwoLines.rowParts) {
            assertEquals(AgentRowLeading.Dot, leading)
            assertTrue(markBesideTitle)
            assertEquals(1, wordLines)
            assertFalse(placeLine)
            assertTrue(sourceAfterWords)
            assertFalse(stateAtFoot)
            assertFalse(card)
        }
        // C: a card, its state's bar down the edge; two lines of words; the state in words at the foot with the place line.
        with(AgentListDisplay.Cards.rowParts) {
            assertEquals(AgentRowLeading.Bar, leading)
            assertTrue(markBesideTitle)
            assertEquals(2, wordLines)
            assertTrue(placeLine)
            assertFalse(sourceAfterWords)
            assertTrue(stateAtFoot)
            assertTrue(card)
        }
        // Compact List: one line, the mark then the state; no words, no place line.
        with(AgentListDisplay.CompactList.rowParts) {
            assertEquals(AgentRowLeading.MarkAndState, leading)
            assertFalse(markBesideTitle)
            assertEquals(0, wordLines)
            assertFalse(placeLine)
            assertFalse(card)
        }
    }

    private val workflow = Workflow(taskId = "t1", runId = "wf_1", name = "parity-audit", status = "running", running = true)

    @Test fun stateCardsSpellEveryStateButSeen() {
        fun card(r: AgentSession, muted: Boolean = false) = r.cardStateWord(muted)?.english()
        assertEquals("Needs you", card(row("needs_you")))
        assertEquals("Working", card(row()))
        assertEquals("Unread", card(row("unread")))
        assertEquals("Awaiting reply", card(row("idle") { copy(awaitsReply = true) }))
        assertEquals("Waiting in the background", card(row { copy(backgroundWaiting = true) }))
        // A computer that is out of reach and a state the app doesn't know have their words too (iOS `stateLabel`).
        assertEquals("Not running", card(row("offline")))
        assertEquals("Unknown", card(row("unknown")))
        // Seen says nothing (iOS: a quiet one's word is left out), nor a muted one.
        assertNull(card(row("idle")))
        assertNull(card(row("needs_you"), muted = true))
        assertNull(card(row("idle") { copy(awaitsReply = true) }, muted = true))
        assertNull(card(row("offline"), muted = true))
    }

    @Test fun aWorkflowKeepsAWorkingAgentWorkingInTheBackground() {
        fun word(r: AgentSession, muted: Boolean = false) = r.stateWord(muted).english()
        // iOS `Session.isBackgroundWorking`: working with a workflow running, whatever else it says; not an ended run.
        assertEquals("Working (Background)", word(row { copy(workflows = listOf(workflow)) }))
        assertEquals("Working", word(row { copy(workflows = listOf(workflow.copy(status = "completed", running = false))) }))
        assertEquals("Working", word(row()))
        // Only a working one: the same run on an agent that has stopped or needs you does not change its state.
        assertEquals("Unread", word(row("unread") { copy(workflows = listOf(workflow)) }))
        assertEquals("Needs you", word(row("needs_you") { copy(workflows = listOf(workflow)) }))
        // Waiting in the background wins, as in `stateLabel`; the assistant's own conversation is never background work.
        assertEquals("Waiting in the background", word(row { copy(backgroundWaiting = true, workflows = listOf(workflow)) }))
        assertEquals("Working", word(row { copy(assistant = true, workflows = listOf(workflow)) }))
        // A card spells it out; so does every other place that writes a state (the space page's cards).
        assertEquals("Working (Background)", row { copy(workflows = listOf(workflow)) }.cardStateWord(false)?.english())
        assertEquals("Seen", word(row("idle")))
        assertEquals("Awaiting reply", word(row("idle") { copy(awaitsReply = true) }))
        // Muted, plain Seen: a muted one is never Awaiting reply.
        assertEquals("Seen", word(row("idle") { copy(awaitsReply = true) }, muted = true))
    }

    @Test fun theStateLabelIsThePlainWordAChatHeaderSays() {
        // iOS `Session.stateLabel`: the same words as `stateWord`, except Awaiting reply, which needs to know about the mute.
        assertEquals("Seen", row("idle") { copy(awaitsReply = true) }.stateLabel.english())
        assertEquals("Awaiting reply", row("idle") { copy(awaitsReply = true) }.stateWord(false).english())
        assertEquals("Working (Background)", row { copy(workflows = listOf(workflow)) }.stateLabel.english())
        assertEquals("Waiting in the background", row { copy(backgroundWaiting = true) }.stateLabel.english())
        assertEquals("Waiting in the background", row { copy(backgroundWaiting = true, workflows = listOf(workflow)) }.stateLabel.english())
        // Only a working one waits in the background; the assistant's own conversation never runs background work.
        assertEquals("Seen", row("idle") { copy(backgroundWaiting = true) }.stateLabel.english())
        assertEquals("Working", row { copy(assistant = true, workflows = listOf(workflow)) }.stateLabel.english())
        for ((state, word) in listOf("needs_you" to "Needs you", "working" to "Working", "unread" to "Unread", "idle" to "Seen",
            "offline" to "Not running", "unknown" to "Unknown")) assertEquals(word, row(state).stateLabel.english())
        // The words in the app's other languages, from iOS's catalog.
        assertEquals("工作中（后台）", row { copy(workflows = listOf(workflow)) }.stateLabel.zh())
        assertEquals("后台等待中", row { copy(backgroundWaiting = true) }.stateLabel.zh())
    }

    @Test fun rowContentTakesTheDisplaysStateWord() {
        fun word(r: AgentSession, display: AgentListDisplay, section: Boolean = false) =
            r.rowContent(AgentRowContext(stateSection = section), display = display).stateWord?.english()
        // Avatar and Two Lines: only what the dot can't say.
        for (display in listOf(AgentListDisplay.Avatar, AgentListDisplay.TwoLines)) {
            assertEquals("Needs you", word(row("needs_you"), display))
            assertNull(word(row(), display))
            assertNull(word(row("unread"), display))
        }
        // State Cards: the state spelled out, but not under a section that names it.
        assertEquals("Working", word(row(), AgentListDisplay.Cards))
        assertEquals("Unread", word(row("unread"), AgentListDisplay.Cards))
        assertNull(word(row(), AgentListDisplay.Cards, section = true))
        assertNull(word(row("idle"), AgentListDisplay.Cards))
        // The rest of the row is the same in every display.
        val s = row { copy(spaceName = "weather", slotName = "widget") }
        val avatar = s.rowContent(AgentRowContext(located = true))
        for (display in AgentListDisplay.entries) {
            val content = s.rowContent(AgentRowContext(located = true), display = display)
            assertEquals(avatar.place, content.place)
            assertEquals(avatar.model, content.model)
        }
    }
}
