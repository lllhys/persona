package persona.feature.chat

import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.flowOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.FoldedAs
import persona.core.contracts.Interaction
import persona.core.contracts.Item
import persona.core.contracts.Question
import persona.core.contracts.QuestionOption
import persona.core.contracts.Resolution
import persona.core.contracts.Session
import persona.core.contracts.SessionCapabilities
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.designsystem.PersonaTheme

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi", application = QuestionTestApplication::class)
class ChildCardCloseUiTest {
    @get:Rule val rule = createComposeRule()

    private val childRequest = Interaction("child-q", machineId = "m", sessionId = "child", kind = "question",
        foldedUnder = "parent", canCancel = true,
        questions = listOf(Question("Continue?", options = listOf(QuestionOption("Yes"), QuestionOption("No")))))
    private val sideRequest = childRequest.copy(id = "side-q", sessionId = "side", foldedAs = FoldedAs.SIDE)
    private val workRequest = childRequest.copy(id = "work-q", toolName = "persona_triage",
        questions = listOf(Question("What next?", options = listOf(
            QuestionOption("Handled, continue"), QuestionOption("Leave it", action = "defer"),
        ))))
    private val repository = object : ChatRepository {
        override fun observe(session: SessionKey) = flowOf(ChatSnapshot())
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int): List<Item> = emptyList()
    }

    private fun show(request: Interaction, parentBlocked: String?, ownerCanApprove: Boolean,
        answers: MutableList<Resolution>, discarded: MutableList<String>) {
        val owner = Session(request.machineId, request.sessionId, sourceKind = "runtime", status = "working", state = "working",
            foldedUnder = request.foldedUnder, foldedAs = request.foldedAs,
            capabilities = SessionCapabilities(approve = ownerCanApprove))
        val fromSide = request.foldedAs == FoldedAs.SIDE
        val children = ChatChildren(rows = if (fromSide) emptyMap() else mapOf(owner.sessionId to owner),
            repository = repository, sides = if (fromSide) listOf(owner) else emptyList())
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                CompositionLocalProvider(LocalChatChildren provides children) {
                    InteractionPanel(listOf(request), null, 600.dp, null, emptySet(), { _, answer -> answers += answer },
                        null, {}, blockedReason = parentBlocked, onDiscardLocal = { discarded += it.id })
                }
            }
        }
    }

    private fun close() = rule.onNode(hasContentDescription("Dismiss card on this device") or
        hasContentDescription("Close question without answering")).assertIsEnabled()

    private fun assertLocalDiscard(request: Interaction, answers: List<Resolution>, discarded: List<String>) {
        assertEquals(listOf(request.id), discarded)
        assertTrue(answers.isEmpty())
    }

    private fun assertRemoteResolution(resolution: Resolution, answers: List<Resolution>, discarded: List<String>) {
        assertEquals(listOf(resolution), answers)
        assertTrue(discarded.isEmpty())
    }

    @Test fun answerableParentDoesNotSendCancelForBlockedChild() {
        val answers = mutableListOf<Resolution>()
        val discarded = mutableListOf<String>()
        show(childRequest, parentBlocked = null, ownerCanApprove = false, answers, discarded)

        close().performClick()

        assertLocalDiscard(childRequest, answers, discarded)
    }

    @Test fun blockedParentDoesNotDiscardAnswerableChildsCancel() {
        val answers = mutableListOf<Resolution>()
        val discarded = mutableListOf<String>()
        show(childRequest, parentBlocked = "Parent cannot approve", ownerCanApprove = true, answers, discarded)

        close().performClick()

        assertRemoteResolution(Resolution(cancel = true), answers, discarded)
    }

    @Test fun answerableParentDoesNotSendCancelForBlockedSide() {
        val answers = mutableListOf<Resolution>()
        val discarded = mutableListOf<String>()
        show(sideRequest, parentBlocked = null, ownerCanApprove = false, answers, discarded)

        close().performClick()

        assertLocalDiscard(sideRequest, answers, discarded)
    }

    @Test fun blockedParentDoesNotDiscardAnswerableSidesCancel() {
        val answers = mutableListOf<Resolution>()
        val discarded = mutableListOf<String>()
        show(sideRequest, parentBlocked = "Parent cannot approve", ownerCanApprove = true, answers, discarded)

        close().performClick()

        assertRemoteResolution(Resolution(cancel = true), answers, discarded)
    }

    @Test fun answerableParentDismissesBlockedChildsStoppedWork() {
        val answers = mutableListOf<Resolution>()
        val discarded = mutableListOf<String>()
        show(workRequest, parentBlocked = null, ownerCanApprove = false, answers, discarded)

        close().performClick()

        assertRemoteResolution(Resolution(dismiss = true), answers, discarded)
    }

    @Test fun blockedParentCancelsAnswerableChildsWorkInsteadOfDismissing() {
        val answers = mutableListOf<Resolution>()
        val discarded = mutableListOf<String>()
        show(workRequest, parentBlocked = "Parent cannot approve", ownerCanApprove = true, answers, discarded)

        close().performClick()

        assertRemoteResolution(Resolution(cancel = true), answers, discarded)
    }
}
